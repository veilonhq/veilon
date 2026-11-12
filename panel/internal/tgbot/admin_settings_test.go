package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

func (r *rig) createAdmin(name string, sudo bool) int64 {
	r.t.Helper()
	st, raw := r.srv.Do(context.Background(), r.root(), "POST", "/admins", map[string]any{"username": name, "password": "password123", "is_sudo": sudo})
	if st != 201 {
		r.t.Fatalf("admin %d %s", st, raw)
	}
	var a struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.ID
}

func (r *rig) admin(id int64) db.Admin {
	var a db.Admin
	r.gdb.Where("id = ?", id).Limit(1).Find(&a)
	return a
}

func TestAdminDeleteNeedsConfirm(t *testing.T) {
	r := newRig(t)
	id := r.createAdmin("sam", false)
	r.start()
	c := r.press("al", "<b>ادمین ها</b>")
	c = r.press(button(t, c, "sam"), "نقش: 👤 محدود")
	c = r.press(button(t, c, "حذف"), "حذف شود")
	if r.admin(id).ID == 0 {
		t.Fatal("deleted before confirm")
	}
	r.press(button(t, c, "نه"), "sam")
	if r.admin(id).ID == 0 {
		t.Fatal("deleted on no")
	}
	c = r.press(fmt.Sprintf("ad:%d", id), "حذف شود")
	r.press(button(t, c, "بله"), "<b>ادمین ها</b>")
	if r.admin(id).ID != 0 {
		t.Fatal("admin still exists")
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "admin.delete").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
}

func TestAdminEditDisableAndActorGuard(t *testing.T) {
	r := newRig(t)
	id := r.createAdmin("sam", false)
	r.start()
	v := r.press(fmt.Sprintf("av:%d", id), "sam")
	// limits and permissions
	c := r.press(button(t, v, "سقف کاربر"), "حداکثر چند کاربر")
	r.typeText("۲۵", "از ۲۵")
	c = r.press(fmt.Sprintf("at:%d", id), "به گیگابایت")
	r.typeText("100", "sam")
	c = r.press(fmt.Sprintf("ap:%d", id), "دسترسی های sam")
	r.press(button(t, c, "ورودی ها"), "دسترسی های sam")
	a := r.admin(id)
	if a.UserQuota != 25 || a.TrafficQuota != 100<<30 || !auth.Perms(&a)[auth.PermInbounds] {
		t.Fatalf("admin %+v perms %v", a, auth.PermList(&a))
	}
	// disable needs a confirmation; a disabled admin cannot sign in
	c = r.press(fmt.Sprintf("ax:%d", id), "غیرفعال شود")
	if r.admin(id).Disabled {
		t.Fatal("disabled before confirm")
	}
	r.press(button(t, c, "بله"), "⛔️ غیرفعال")
	if !r.admin(id).Disabled {
		t.Fatal("not disabled")
	}
	if _, _, _, err := r.srv.Auth.Login("1.1.1.1", "sam", "password123"); err != auth.ErrAdminDisabled {
		t.Fatalf("login of a disabled admin: %v", err)
	}
	r.press(fmt.Sprintf("ax:%d", id), "نقش:")
	if r.admin(id).Disabled {
		t.Fatal("not enabled again")
	}
	// password of another admin can be set
	r.press(fmt.Sprintf("aw:%d", id), "رمز تازه")
	r.typeText("newpassword1", "sam")
	if !auth.CheckPassword(r.admin(id).PasswordHash, "newpassword1") {
		t.Fatal("password not changed")
	}
	// the bot's own admin: no role, password, disable or delete buttons,
	// and a forged press is refused
	root := r.root()
	before := root.PasswordHash
	v = r.press(fmt.Sprintf("av:%d", root.ID), "ربات با این ادمین کار می کند")
	for _, l := range []string{"رمز تازه", "حذف", "غیرفعال", "محدود کردن"} {
		if strings.Contains(fmt.Sprint(v.p["reply_markup"]), l) {
			t.Fatalf("actor screen has %q", l)
		}
	}
	r.press(fmt.Sprintf("aw:%d", root.ID), "ربات با این ادمین کار می کند")
	if r.root().PasswordHash != before {
		t.Fatal("actor password changed")
	}
	c = r.press(fmt.Sprintf("ad:%d", root.ID), "حذف شود")
	r.press(button(t, c, "بله"), "انجام نشد")
	if r.admin(root.ID).ID == 0 {
		t.Fatal("actor deleted")
	}
}

func TestAdminCreate(t *testing.T) {
	r := newRig(t)
	r.start()
	r.press("an", "نام کاربری ادمین")
	r.typeText("helper", "رمز ادمین")
	c := r.typeText("password123", "نقش این ادمین")
	r.press(button(t, c, "محدود"), "helper")
	var a db.Admin
	r.gdb.Where("username = ?", "helper").Limit(1).Find(&a)
	if a.ID == 0 || a.IsSudo || !auth.CheckPassword(a.PasswordHash, "password123") {
		t.Fatalf("admin %+v", a)
	}
}

func TestPanelSettings(t *testing.T) {
	r := newRig(t)
	r.start()
	c := r.press("ps", "تنظیمات پنل")
	if strings.Contains(fmt.Sprint(c.p["text"]), "123456789:") {
		t.Fatal("token shown")
	}
	r.press(button(t, c, "بازه بروزرسانی"), "هر چند ساعت")
	r.typeText("500", "۱ تا ۱۶۸")
	r.typeText("۲۴", "هر ۲۴ ساعت")
	if db.LoadSettings(r.gdb).SubUpdateHours != 24 {
		t.Fatal("sub_update_hours not saved")
	}
	// the panel port asks for a confirmation first
	r.press("pse:0", "پورت تازه پنل")
	c = r.typeText("9443", "عوض شود")
	if db.LoadSettings(r.gdb).PanelPort != 9000 {
		t.Fatal("port changed before confirm")
	}
	r.press(button(t, c, "بله"), "تنظیمات پنل")
	if db.LoadSettings(r.gdb).PanelPort != 9443 {
		t.Fatal("port not changed")
	}
	r.press("psb:ad_block", "تنظیمات پنل")
	r.press("pst", "منطقه زمانی پنل")
	r.press("psts:0", "Asia/Tehran")
	st := db.LoadSettings(r.gdb)
	if !st.AdBlock || st.Timezone != "Asia/Tehran" {
		t.Fatalf("settings %+v", st)
	}
	// notification switches are stored without restarting the bot
	r.press("pn", "اعلان های ربات")
	r.press("pnt:traffic", "اعلان های ربات")
	if !tgbot.LoadConfig(r.gdb).Notify.Traffic {
		t.Fatal("traffic notices not switched on")
	}
	r.press("ps", "تنظیمات پنل")
}

func TestAuditPagedAndFiltered(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 23; i++ {
		audit.Log(r.gdb, &db.Admin{ID: 1, Username: "root"}, "user.create", fmt.Sprintf("u%02d", i), "1.1.1.1")
	}
	audit.Log(r.gdb, &db.Admin{ID: 2, Username: "sam"}, "user.delete", "last-one", "2.2.2.2")
	r.start()
	c := r.press("au:1", "گزارش فعالیت")
	text := fmt.Sprint(c.p["text"])
	if !strings.Contains(text, "last-one") || strings.Contains(text, "u00") {
		t.Fatalf("first page %s", text)
	}
	button(t, c, "▶️")
	c = r.press(button(t, c, "فیلتر ادمین"), "کدام ادمین")
	c = r.press(button(t, c, "sam"), "فقط: <b>sam</b>")
	text = fmt.Sprint(c.p["text"])
	if !strings.Contains(text, "last-one") || strings.Contains(text, "u22") {
		t.Fatalf("filtered %s", text)
	}
	c = r.press(button(t, c, "همه"), "گزارش فعالیت")
	if strings.Contains(fmt.Sprint(c.p["text"]), "فقط:") {
		t.Fatal("filter not cleared")
	}
}
