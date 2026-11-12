package tgbot

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Audit log: newest first, paged, optionally limited to one actor.

const auditPerPage = 10

var auditActions = map[string]string{
	"auth.login":            "ورود",
	"auth.login_failed":     "ورود ناموفق",
	"auth.password_changed": "تغییر رمز",
	"auth.username_changed": "تغییر نام کاربری",
	"user.create":           "ساختن کاربر",
	"user.update":           "ویرایش کاربر",
	"user.delete":           "حذف کاربر",
	"user.reset_traffic":    "صفر کردن مصرف",
	"user.revoke_sub":       "لینک اشتراک تازه",
	"backup.create":         "گرفتن پشتیبان",
	"backup.restore":        "بازگردانی پشتیبان",
	"core.restart":          "راه اندازی دوباره هسته",
	"panel.update":          "بروزرسانی پنل",
	"node.install":          "دستور نصب نود",
	"inbound.create":        "ساختن ورودی",
	"inbound.update":        "ویرایش ورودی",
	"inbound.delete":        "حذف ورودی",
	"node.create":           "ساختن نود",
	"node.update":           "ویرایش نود",
	"node.delete":           "حذف نود",
	"admin.create":          "ساختن ادمین",
	"admin.update":          "ویرایش ادمین",
	"admin.delete":          "حذف ادمین",
	"settings.update":       "تغییر تنظیمات",
	"xray.config":           "تنظیمات Xray",
	"telegram.update":       "تنظیمات ربات",
	"telegram.notify":       "اعلان های ربات",
}

func init() {
	on("au", auditList)
	on("auf", auditFilter)
	on("aus", func(r *req, a []string) {
		k := int(argInt(a, 0))
		if k >= 0 && k < len(r.st.auditActors) {
			r.st.auditActor = r.st.auditActors[k]
		} else {
			r.st.auditActor = ""
		}
		auditList(r, []string{"1"})
	})
}

type auditEntry struct {
	ID        int64     `json:"id"`
	Admin     string    `json:"admin"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

func auditList(r *req, a []string) {
	page := int(argInt(a, 0))
	if page < 1 {
		page = 1
	}
	q := url.Values{"page": {fmt.Sprint(page)}, "per_page": {fmt.Sprint(auditPerPage)}}
	if r.st.auditActor != "" {
		q.Set("admin", r.st.auditActor)
	}
	var res struct {
		Items []auditEntry `json:"items"`
		Total int64        `json:"total"`
	}
	if err := r.call("GET", "/audit?"+q.Encode(), nil, &res); err != nil {
		r.fail(err, "home")
		return
	}
	pages := int((res.Total + auditPerPage - 1) / auditPerPage)
	if pages < 1 {
		pages = 1
	}
	t := header("🗂", "گزارش فعالیت")
	if r.st.auditActor != "" {
		t += "فقط: <b>" + esc(r.st.auditActor) + "</b>\n"
	}
	t += "تعداد: " + faInt(res.Total) + " · تازه ترین ها بالا\n"
	loc := r.b.loc()
	if len(res.Items) == 0 {
		t += "\nموردی نیست."
	}
	for _, e := range res.Items {
		act := auditActions[e.Action]
		if act == "" {
			act = e.Action
		}
		who := e.Admin
		if who == "" {
			who = "ناشناس"
		}
		t += "\n🕒 " + jdate(e.CreatedAt, loc) + " · <b>" + esc(act) + "</b>\n👤 " + esc(cut(who, 30))
		if e.IP != "" {
			t += " · " + code(e.IP)
		}
		if tg := strings.TrimSpace(e.Target); tg != "" {
			t += "\n↳ " + esc(cut(strings.ReplaceAll(tg, "\n", " "), 80))
		}
		t += "\n"
	}
	kb := Keyboard{}
	if p := pager("au", page, pages); p != nil {
		kb = append(kb, p)
	}
	row := []Button{btn("👤 فیلتر ادمین", "auf")}
	if r.st.auditActor != "" {
		row = append(row, btn("🧹 همه", "aus", -1))
	}
	row = append(row, btn("🔄 تازه کردن", "au", page))
	kb = append(kb, row, nav("home"))
	r.show(view{text: t, kb: kb})
}

func auditFilter(r *req, _ []string) {
	var res struct {
		Actors []string `json:"actors"`
	}
	if err := r.call("GET", "/audit?per_page=1&actors=1", nil, &res); err != nil {
		r.fail(err, "au")
		return
	}
	r.st.auditActors = res.Actors
	var bs []Button
	for i, n := range res.Actors {
		mark := "👤 "
		if n == r.st.auditActor {
			mark = "✅ "
		}
		bs = append(bs, btn(mark+cut(n, 24), "aus", i))
	}
	kb := chunk(bs, 2)
	kb = append(kb, []Button{btn("🧹 همه ادمین ها", "aus", -1)}, nav("au"))
	t := header("👤", "فیلتر گزارش") + "کارهای کدام ادمین نشان داده شود؟"
	if len(res.Actors) == 0 {
		t += "\n\nهنوز گزارشی ثبت نشده است."
	}
	r.show(view{text: t, kb: kb})
}
