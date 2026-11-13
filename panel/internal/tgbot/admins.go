package tgbot

import (
	"fmt"
	"strconv"
	"time"

	"github.com/veilonhq/veilon/panel/internal/auth"
)

// Admins section: list, add, edit role and limits, disable, delete. Every
// change goes through the panel's own admin API (sudo only), so its rules
// hold here too: nobody removes, disables or demotes the last full admin,
// an admin who owns users is not deleted, and the bot never removes its own
// access. The password of the admin the bot acts as is never changed here.

type adminRow struct {
	ID               int64     `json:"id"`
	Username         string    `json:"username"`
	IsSudo           bool      `json:"is_sudo"`
	Disabled         bool      `json:"disabled"`
	Permissions      []string  `json:"permissions"`
	UserQuota        int       `json:"user_quota"`
	TrafficQuota     int64     `json:"traffic_quota"`
	Users            int64     `json:"users"`
	TrafficAllocated int64     `json:"traffic_allocated"`
	CreatedAt        time.Time `json:"created_at"`
}

var permLabels = map[string]string{
	auth.PermUsersView:   T("دیدن کاربران"),
	auth.PermUsersCreate: T("ساختن کاربر"),
	auth.PermUsersEdit:   T("ویرایش کاربر"),
	auth.PermUsersDelete: T("حذف کاربر"),
	auth.PermAllUsers:    T("همه کاربران (نه فقط کاربران خودش)"),
	auth.PermInbounds:    T("ورودی ها"),
	auth.PermSettings:    T("تنظیمات و هسته"),
	auth.PermCerts:       T("گواهی ها"),
	auth.PermLogs:        T("گزارش ها"),
	auth.PermGroups:      T("گروه ها"),
}

const adminsPerPage = 8

func init() {
	on("al", adminsList)
	on("av", adminView)
	on("ar", func(r *req, a []string) {
		ad, ok := r.adminLoad(argInt(a, 0))
		if !ok {
			return
		}
		q := "<b>" + esc(ad.Username) + T("</b> ادمین کامل شود؟\nادمین کامل به همه بخش ها، از جمله ادمین ها و تنظیمات، دسترسی دارد.")
		if ad.IsSudo {
			q = "<b>" + esc(ad.Username) + T("</b> ادمین محدود شود؟\nفقط به بخش هایی که برایش روشن کنید دسترسی خواهد داشت.")
		}
		confirm(r, T("تغییر نقش"), q, cbd("ary", ad.ID), cbd("av", ad.ID))
	})
	on("ary", func(r *req, a []string) {
		ad, ok := r.adminLoad(argInt(a, 0))
		if !ok {
			return
		}
		adminPatch(r, ad.ID, map[string]any{"is_sudo": !ad.IsSudo})
	})
	on("ap", adminPerms)
	on("apt", func(r *req, a []string) {
		ad, ok := r.adminLoad(argInt(a, 0))
		if !ok {
			return
		}
		p := argStr(a, 1)
		if permLabels[p] == "" || ad.IsSudo {
			adminView(r, a[:1])
			return
		}
		has := map[string]bool{}
		for _, x := range ad.Permissions {
			has[x] = true
		}
		has[p] = !has[p]
		list := []string{}
		for _, x := range auth.AllPerms {
			if has[x] {
				list = append(list, x)
			}
		}
		if err := r.call("PATCH", fmt.Sprintf("/admins/%d", ad.ID), map[string]any{"permissions": list}, nil); err != nil {
			r.fail(err, cbd("av", ad.ID))
			return
		}
		r.toast("✅")
		adminPerms(r, a[:1])
	})
	on("aq", func(r *req, a []string) {
		id := argInt(a, 0)
		r.ask("aquota", []string{strconv.FormatInt(id, 10)}, T("سقف تعداد کاربر"), T("این ادمین حداکثر چند کاربر بسازد؟ عدد بفرستید (۰ یعنی بی نهایت)."), cbd("av", id),
			[]Button{btn(T("♾ بی نهایت"), "aqs", id, 0)})
	})
	on("aqs", func(r *req, a []string) {
		adminPatch(r, argInt(a, 0), map[string]any{"user_quota": argInt(a, 1)})
	})
	onInput("aquota", func(r *req, a []string, text string, _ *Document) {
		id := argInt(a, 0)
		n, ok := parseInt(text)
		if !ok || n < 0 || n > 1_000_000 {
			r.ask("aquota", a, T("سقف تعداد کاربر"), T("⚠️ یک عدد صحیح از ۰ بفرستید (۰ یعنی بی نهایت)."), cbd("av", id), []Button{btn(T("♾ بی نهایت"), "aqs", id, 0)})
			return
		}
		adminPatch(r, id, map[string]any{"user_quota": n})
	})
	on("at", func(r *req, a []string) {
		id := argInt(a, 0)
		r.ask("atraffic", []string{strconv.FormatInt(id, 10)}, T("سقف حجم"), T("جمع حجمی که این ادمین می تواند به کاربرانش بدهد، به گیگابایت (۰ یعنی بی نهایت):"), cbd("av", id),
			[]Button{btn(T("♾ بی نهایت"), "ats", id, 0)})
	})
	on("ats", func(r *req, a []string) {
		adminPatch(r, argInt(a, 0), map[string]any{"traffic_quota": 0})
	})
	onInput("atraffic", func(r *req, a []string, text string, _ *Document) {
		id := argInt(a, 0)
		f, ok := parseNum(text)
		if !ok || f < 0 || f > 1e7 {
			r.ask("atraffic", a, T("سقف حجم"), T("⚠️ یک عدد از ۰ به گیگابایت بفرستید (۰ یعنی بی نهایت)."), cbd("av", id), []Button{btn(T("♾ بی نهایت"), "ats", id, 0)})
			return
		}
		adminPatch(r, id, map[string]any{"traffic_quota": gb(f)})
	})
	on("aw", func(r *req, a []string) {
		ad, ok := r.adminLoad(argInt(a, 0))
		if !ok {
			return
		}
		if r.isActor(ad.ID) {
			r.alert(T("رمز ادمینی که ربات با آن کار می کند از ربات عوض نمی شود."))
			adminView(r, a)
			return
		}
		r.ask("apass", []string{strconv.FormatInt(ad.ID, 10)}, T("رمز تازه ")+ad.Username,
			T("رمز تازه را بفرستید (حداقل ۸ حرف). پیام شما بلافاصله از گفتگو پاک می شود.\nبا عوض شدن رمز، این ادمین از همه دستگاه ها بیرون می رود."), cbd("av", ad.ID))
	})
	onInput("apass", func(r *req, a []string, text string, _ *Document) {
		id := argInt(a, 0)
		if r.isActor(id) {
			r.show(view{text: header("⚠️", T("انجام نشد")) + T("رمز ادمینی که ربات با آن کار می کند از ربات عوض نمی شود."), kb: Keyboard{nav(cbd("av", id))}})
			return
		}
		if err := r.call("PATCH", fmt.Sprintf("/admins/%d", id), map[string]any{"password": text}, nil); err != nil {
			r.fail(err, cbd("av", id))
			return
		}
		r.toast(T("✅ رمز عوض شد"))
		adminView(r, []string{strconv.FormatInt(id, 10)})
	})
	on("ax", func(r *req, a []string) {
		ad, ok := r.adminLoad(argInt(a, 0))
		if !ok {
			return
		}
		if ad.Disabled {
			adminPatch(r, ad.ID, map[string]any{"disabled": false})
			return
		}
		confirm(r, T("غیرفعال کردن ادمین"), "<b>"+esc(ad.Username)+T("</b> غیرفعال شود؟\nدیگر نمی تواند وارد پنل شود و از همه دستگاه ها بیرون می رود. کاربرانش دست نمی خورند."), cbd("axy", ad.ID), cbd("av", ad.ID))
	})
	on("axy", func(r *req, a []string) { adminPatch(r, argInt(a, 0), map[string]any{"disabled": true}) })
	on("ad", func(r *req, a []string) {
		ad, ok := r.adminLoad(argInt(a, 0))
		if !ok {
			return
		}
		q := T("ادمین <b>") + esc(ad.Username) + T("</b> حذف شود؟ این کار برگشت ندارد.")
		if ad.Users > 0 {
			q += T("\n\n⚠️ این ادمین ") + faInt(ad.Users) + T(" کاربر دارد؛ پنل ادمینی که کاربر دارد را حذف نمی کند. اول کاربرانش را به ادمین دیگری بدهید یا حذف کنید.")
		}
		confirm(r, T("حذف ادمین"), q, cbd("ady", ad.ID), cbd("av", ad.ID))
	})
	on("ady", func(r *req, a []string) {
		id := argInt(a, 0)
		if err := r.call("DELETE", fmt.Sprintf("/admins/%d", id), nil, nil); err != nil {
			r.fail(err, cbd("av", id))
			return
		}
		r.toast(T("🗑 حذف شد"))
		adminsList(r, nil)
	})
	// new admin: username, password, role
	on("an", func(r *req, _ []string) {
		r.st.draft = map[string]string{}
		r.ask("anname", nil, T("ادمین جدید"), T("نام کاربری ادمین را بفرستید (۳ تا ۳۲ حرف انگلیسی، رقم، _ . -):"), "al")
	})
	onInput("anname", func(r *req, _ []string, text string, _ *Document) {
		r.st.draft["name"] = text
		r.ask("anpass", nil, T("ادمین جدید · ")+cut(text, 20), T("رمز ادمین را بفرستید (حداقل ۸ حرف). پیام شما بلافاصله از گفتگو پاک می شود."), "al")
	})
	onInput("anpass", func(r *req, _ []string, text string, _ *Document) {
		r.st.draft["pass"] = text
		r.show(view{text: header("👤", T("ادمین جدید · ")+r.st.draft["name"]) + T("نقش این ادمین؟\n\n👤 <b>محدود:</b> فقط کاربران خودش را می بیند و می سازد (بعدا می شود دسترسی های بیشتری داد).\n👑 <b>کامل:</b> به همه چیز دسترسی دارد."),
			kb: Keyboard{{btn(T("👤 محدود"), "anr", 0), btn(T("👑 کامل"), "anr", 1)}, {btn(T("✖️ انصراف"), "al")}}})
	})
	on("anr", func(r *req, a []string) {
		name, pass := r.st.draft["name"], r.st.draft["pass"]
		if name == "" || pass == "" {
			adminsList(r, nil)
			return
		}
		r.st.draft = map[string]string{}
		var ad adminRow
		if err := r.call("POST", "/admins", map[string]any{"username": name, "password": pass, "is_sudo": argInt(a, 0) == 1}, &ad); err != nil {
			r.fail(err, "al")
			return
		}
		r.toast(T("✅ ساخته شد"))
		adminView(r, []string{strconv.FormatInt(ad.ID, 10)})
	})
}

// isActor reports whether id is the panel admin the bot acts as.
func (r *req) isActor(id int64) bool {
	a, err := r.b.actor(r.from)
	return err == nil && a.ID == id
}

func (r *req) admins() ([]adminRow, error) {
	var list []adminRow
	err := r.call("GET", "/admins", nil, &list)
	return list, err
}

func (r *req) adminLoad(id int64) (*adminRow, bool) {
	list, err := r.admins()
	if err != nil {
		r.fail(err, "home")
		return nil, false
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], true
		}
	}
	r.fail(&apiErr{Status: 404, Message: "not found"}, "al")
	return nil, false
}

func adminBadge(a *adminRow) string {
	switch {
	case a.Disabled:
		return "⛔️"
	case a.IsSudo:
		return "👑"
	default:
		return "👤"
	}
}

func adminsList(r *req, a []string) {
	list, err := r.admins()
	if err != nil {
		r.fail(err, "home")
		return
	}
	page := int(argInt(a, 0))
	pages := (len(list) + adminsPerPage - 1) / adminsPerPage
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	t := header("👤", T("ادمین ها")) + T("تعداد: <b>") + faInt(len(list)) + T("</b>\n👑 کامل · 👤 محدود · ⛔️ غیرفعال · 🤖 ربات با این ادمین کار می کند")
	kb := Keyboard{}
	from := (page - 1) * adminsPerPage
	for i := from; i < len(list) && i < from+adminsPerPage; i++ {
		ad := &list[i]
		label := adminBadge(ad) + " " + cut(ad.Username, 22) + " · " + faInt(ad.Users) + T(" کاربر")
		if r.isActor(ad.ID) {
			label += " · 🤖"
		}
		kb = append(kb, []Button{btn(label, "av", ad.ID)})
	}
	if p := pager("al", page, pages); p != nil {
		kb = append(kb, p)
	}
	kb = append(kb, []Button{btn(T("➕ ادمین جدید"), "an")}, nav("home"))
	r.show(view{text: t, kb: kb})
}

func adminView(r *req, a []string) {
	ad, ok := r.adminLoad(argInt(a, 0))
	if !ok {
		return
	}
	actor := r.isActor(ad.ID)
	t := header(adminBadge(ad), ad.Username)
	role := T("👤 محدود")
	if ad.IsSudo {
		role = T("👑 کامل")
	}
	t += T("نقش: ") + role
	if ad.Disabled {
		t += T(" · ⛔️ غیرفعال")
	}
	t += T("\nکاربران: <b>") + faInt(ad.Users) + "</b>"
	if !ad.IsSudo {
		uq := T("بی نهایت")
		if ad.UserQuota > 0 {
			uq = faInt(ad.UserQuota)
		}
		tq := T("بی نهایت")
		if ad.TrafficQuota > 0 {
			tq = size(ad.TrafficQuota)
		}
		t += T(" از ") + uq + T("\nحجم داده شده: ") + size(ad.TrafficAllocated) + T(" از ") + tq + T("\n\n<b>دسترسی ها:</b>\n")
		has := map[string]bool{}
		for _, p := range ad.Permissions {
			has[p] = true
		}
		for _, p := range auth.AllPerms {
			if has[p] {
				t += "✅ " + permLabels[p] + "\n"
			}
		}
	} else {
		t += "\n"
	}
	t += T("\nساخته شده: ") + jdate(ad.CreatedAt, r.b.loc()) + "\n"
	if actor {
		t += T("\n🤖 ربات با این ادمین کار می کند؛ نقش، رمز، غیرفعال کردن و حذفش از ربات ممکن نیست.")
	}
	kb := Keyboard{}
	if !actor {
		role := btn(T("👑 کامل کردن"), "ar", ad.ID)
		if ad.IsSudo {
			role = btn(T("👤 محدود کردن"), "ar", ad.ID)
		}
		kb = append(kb, []Button{role, btn(T("🔑 رمز تازه"), "aw", ad.ID)})
	}
	if !ad.IsSudo {
		kb = append(kb, []Button{btn(T("🛂 دسترسی ها"), "ap", ad.ID)}, []Button{btn(T("👥 سقف کاربر"), "aq", ad.ID), btn(T("📦 سقف حجم"), "at", ad.ID)})
	}
	if !actor {
		dis := btn(T("⛔️ غیرفعال کردن"), "ax", ad.ID)
		if ad.Disabled {
			dis = btn(T("✅ فعال کردن"), "ax", ad.ID)
		}
		kb = append(kb, []Button{dis, btn(T("🗑 حذف"), "ad", ad.ID)})
	}
	kb = append(kb, nav("al"))
	r.show(view{text: t, kb: kb})
}

func adminPerms(r *req, a []string) {
	ad, ok := r.adminLoad(argInt(a, 0))
	if !ok {
		return
	}
	if ad.IsSudo {
		adminView(r, a)
		return
	}
	has := map[string]bool{}
	for _, p := range ad.Permissions {
		has[p] = true
	}
	kb := Keyboard{}
	for _, p := range auth.AllPerms {
		kb = append(kb, []Button{btn(onOff(has[p])+" "+permLabels[p], "apt", ad.ID, p)})
	}
	kb = append(kb, nav(cbd("av", ad.ID)))
	r.show(view{text: header("🛂", T("دسترسی های ")+ad.Username) + T("هر مورد را بزنید تا روشن یا خاموش شود. تغییر بلافاصله ذخیره می شود."), kb: kb})
}

func adminPatch(r *req, id int64, body map[string]any) {
	if _, ok := body["password"]; ok && r.isActor(id) {
		r.show(view{text: header("⚠️", T("انجام نشد")) + T("رمز ادمینی که ربات با آن کار می کند از ربات عوض نمی شود."), kb: Keyboard{nav(cbd("av", id))}})
		return
	}
	if err := r.call("PATCH", fmt.Sprintf("/admins/%d", id), body, nil); err != nil {
		r.fail(err, cbd("av", id))
		return
	}
	r.toast(T("✅ ذخیره شد"))
	adminView(r, []string{strconv.FormatInt(id, 10)})
}
