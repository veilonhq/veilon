package tgbot_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// The user's firewall from the bot: open it from the edit menu, add
// entries (several at once, a bad one is explained and asked again),
// delete one, clear all with a confirmation; the same list the web form
// and the API use. In English too.
func TestBotUserFirewall(t *testing.T) {
	r := newRig(t)
	u := r.createUser("bob", 10, 5)
	r.start()
	e := r.press(fmt.Sprintf("ue:%d", u.ID), "ویرایش bob")
	fw := r.press(button(t, e, "فایروال"), "فایروال bob")
	if !strings.Contains(fmt.Sprint(fw.p["text"]), "فهرست خالی است") || !strings.Contains(fmt.Sprint(fw.p["text"]), "AmneziaWG") {
		t.Fatalf("empty screen %v", fw.p["text"])
	}
	r.press(button(t, fw, "افزودن"), "یک یا چند مورد بفرستید")
	c := r.typeText("geosite:youtube", "پیشوند پشتیبانی نمی شود")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "geosite:youtube") {
		t.Fatalf("refused entry not named: %v", c.p["text"])
	}
	c = r.typeText("Example.com، 93.184.215.14\nfull:api.example.org example.com", "۳ مورد اضافه شد")
	text := fmt.Sprint(c.p["text"])
	for _, w := range []string{"example.com", "93.184.215.14", "full:api.example.org", "دامنه و زیر دامنه ها", "موارد تکراری"} {
		if !strings.Contains(text, w) {
			t.Fatalf("list lacks %q: %s", w, text)
		}
	}
	if got := r.user("bob").Firewall; got != "example.com\n93.184.215.14\nfull:api.example.org" {
		t.Fatalf("stored %q", got)
	}
	c = r.press(button(t, c, "🗑 ۲"), "فایروال bob")
	if got := r.user("bob").Firewall; got != "example.com\nfull:api.example.org" {
		t.Fatalf("after delete %q", got)
	}
	v := r.press(fmt.Sprintf("uv:%d", u.ID), "فایروال: ۲ مورد بسته")
	_ = v
	c = r.press(fmt.Sprintf("ufw:%d", u.ID), "فایروال bob")
	c = r.press(button(t, c, "پاک کردن همه"), "همه موارد فایروال")
	r.press(button(t, c, "بله"), "فهرست خالی است")
	if got := r.user("bob").Firewall; got != "" {
		t.Fatalf("after clear %q", got)
	}
	var n int64
	r.gdb.Model(&db.AuditLog{}).Where("action = ? AND admin_name LIKE ?", "user.firewall", "telegram:%").Count(&n)
	if n != 3 {
		t.Fatalf("audit entries %d", n)
	}

	if err := db.SetSetting(r.gdb, fmt.Sprintf("_telegram_lang_%d", adminID), "en"); err != nil {
		t.Fatal(err)
	}
	c = r.press(fmt.Sprintf("ufw:%d", u.ID), "Firewall · bob")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "The list is empty") || strings.ContainsAny(fmt.Sprint(c.p["text"]), "ابپت") {
		t.Fatalf("english screen %v", c.p["text"])
	}
	r.press(button(t, c, "Add"), "Send one or more entries")
	r.typeText("10.0.0.0/4", "the range is too wide")
	r.typeText("10.0.0.0/8", "1 added to the list")
}
