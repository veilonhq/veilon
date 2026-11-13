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
	"auth.login":            T("ورود"),
	"auth.login_failed":     T("ورود ناموفق"),
	"auth.password_changed": T("تغییر رمز"),
	"auth.username_changed": T("تغییر نام کاربری"),
	"user.create":           T("ساختن کاربر"),
	"user.update":           T("ویرایش کاربر"),
	"user.delete":           T("حذف کاربر"),
	"user.reset_traffic":    T("صفر کردن مصرف"),
	"user.revoke_sub":       T("لینک اشتراک تازه"),
	"backup.create":         T("گرفتن پشتیبان"),
	"backup.restore":        T("بازگردانی پشتیبان"),
	"core.restart":          T("راه اندازی دوباره هسته"),
	"panel.update":          T("بروزرسانی پنل"),
	"node.install":          T("دستور نصب نود"),
	"inbound.create":        T("ساختن ورودی"),
	"inbound.update":        T("ویرایش ورودی"),
	"inbound.delete":        T("حذف ورودی"),
	"node.create":           T("ساختن نود"),
	"node.update":           T("ویرایش نود"),
	"node.delete":           T("حذف نود"),
	"admin.create":          T("ساختن ادمین"),
	"admin.update":          T("ویرایش ادمین"),
	"admin.delete":          T("حذف ادمین"),
	"settings.update":       T("تغییر تنظیمات"),
	"xray.config":           T("تنظیمات Xray"),
	"telegram.update":       T("تنظیمات ربات"),
	"telegram.notify":       T("اعلان های ربات"),
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
	t := header("🗂", T("گزارش فعالیت"))
	if r.st.auditActor != "" {
		t += T("فقط: <b>") + esc(r.st.auditActor) + "</b>\n"
	}
	t += T("تعداد: ") + faInt(res.Total) + T(" · تازه ترین ها بالا\n")
	loc := r.b.loc()
	if len(res.Items) == 0 {
		t += T("\nموردی نیست.")
	}
	for _, e := range res.Items {
		act := auditActions[e.Action]
		if act == "" {
			act = e.Action
		}
		who := e.Admin
		if who == "" {
			who = T("ناشناس")
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
	row := []Button{btn(T("👤 فیلتر ادمین"), "auf")}
	if r.st.auditActor != "" {
		row = append(row, btn(T("🧹 همه"), "aus", -1))
	}
	row = append(row, btn(T("🔄 تازه کردن"), "au", page))
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
	kb = append(kb, []Button{btn(T("🧹 همه ادمین ها"), "aus", -1)}, nav("au"))
	t := header("👤", T("فیلتر گزارش")) + T("کارهای کدام ادمین نشان داده شود؟")
	if len(res.Actors) == 0 {
		t += T("\n\nهنوز گزارشی ثبت نشده است.")
	}
	r.show(view{text: t, kb: kb})
}
