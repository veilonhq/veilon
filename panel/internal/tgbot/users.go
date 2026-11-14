package tgbot

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/veilonhq/veilon/panel/internal/users"
)

const usersPerPage = 8

var statusLabel = map[string]string{
	"active":   T("🟢 فعال"),
	"disabled": T("⚪️ غیرفعال"),
	"limited":  T("🔴 حجم تمام شده"),
	"expired":  T("🟠 منقضی"),
	"on_hold":  T("🔵 در انتظار اولین اتصال"),
}

var statusDot = map[string]string{"active": "🟢", "disabled": "⚪️", "limited": "🔴", "expired": "🟠", "on_hold": "🔵"}

var userFilters = []struct{ key, label string }{
	{"", T("همه")}, {"online", T("آنلاین")}, {"active", T("فعال")}, {"expired", T("منقضی")}, {"limited", T("حجم تمام")}, {"disabled", T("غیرفعال")}, {"on_hold", T("در انتظار")},
}

func init() {
	on("ul", usersList)
	on("uf", func(r *req, a []string) {
		r.st.userFilter = argStr(a, 0)
		usersList(r, []string{"1"})
	})
	on("us", func(r *req, a []string) {
		r.ask("usearch", nil, T("جستجوی کاربر"), T("بخشی از نام کاربری یا یادداشت را بفرستید:"), "ul:1")
	})
	onInput("usearch", func(r *req, a []string, text string, _ *Document) {
		r.st.userSearch = text
		usersList(r, []string{"1"})
	})
	on("usc", func(r *req, a []string) {
		r.st.userSearch, r.st.userFilter = "", ""
		usersList(r, []string{"1"})
	})
	on("uv", userView)
	on("uq", userQR)
	on("ut", userToggle)
	on("ud", userDeleteAsk)
	on("udy", userDelete)
	on("ur", userRenew)
	on("ura", func(r *req, a []string) { userExtend(r, argInt(a, 0), argInt(a, 1)) })
	on("urd", func(r *req, a []string) {
		id := argInt(a, 0)
		r.ask("uextend", a, T("تمدید زمان"), T("چند روز اضافه شود؟ (یک عدد، مثلا ۳۰)"), cbd("ur", id))
	})
	onInput("uextend", func(r *req, a []string, text string, _ *Document) {
		n, ok := parseInt(text)
		if !ok || n < 1 {
			r.ask("uextend", a, T("تمدید زمان"), T("⚠️ یک عدد درست از ۱ به بالا بفرستید:"), cbd("ur", argInt(a, 0)))
			return
		}
		userExtend(r, argInt(a, 0), n)
	})
	on("urg", func(r *req, a []string) { userAddTraffic(r, argInt(a, 0), float64(argInt(a, 1))) })
	on("urG", func(r *req, a []string) {
		r.ask("uaddgb", a, T("افزودن حجم"), T("چند گیگابایت اضافه شود؟ (مثلا ۲۰ یا ۰.۵)"), cbd("ur", argInt(a, 0)))
	})
	onInput("uaddgb", func(r *req, a []string, text string, _ *Document) {
		n, ok := parseNum(text)
		if !ok || n <= 0 {
			r.ask("uaddgb", a, T("افزودن حجم"), T("⚠️ یک عدد بزرگتر از صفر بفرستید:"), cbd("ur", argInt(a, 0)))
			return
		}
		userAddTraffic(r, argInt(a, 0), n)
	})
	on("urr", func(r *req, a []string) {
		id := argInt(a, 0)
		confirm(r, T("صفر کردن مصرف"), T("مصرف این کاربر صفر شود؟"), cbd("urry", id), cbd("ur", id))
	})
	on("urry", func(r *req, a []string) {
		id := argInt(a, 0)
		if err := r.call("POST", fmt.Sprintf("/users/%d/reset-traffic", id), nil, nil); err != nil {
			r.fail(err, cbd("uv", id))
			return
		}
		r.toast(T("✅ مصرف صفر شد"))
		userView(r, []string{strconv.FormatInt(id, 10)})
	})
	on("ue", userEditMenu)
	on("uef", userEditField)
	on("urs", func(r *req, a []string) {
		userPatch(r, argInt(a, 0), map[string]any{"reset_strategy": argStr(a, 1)}, "ue")
	})
	on("uht", func(r *req, a []string) { userFlip(r, argInt(a, 0), "require_hwid") })
	on("utt", func(r *req, a []string) { userFlip(r, argInt(a, 0), "block_torrent") })
	on("uwd", userWeekday)
	on("uib", userInboundToggle)
	on("ugp", func(r *req, a []string) {
		gid := argInt(a, 1)
		var v any = gid
		if gid == 0 {
			v = nil
		}
		userPatch(r, argInt(a, 0), map[string]any{"group_id": v}, "ue")
	})
	on("urv", func(r *req, a []string) {
		id := argInt(a, 0)
		confirm(r, T("تعویض لینک اشتراک"), T("لینک اشتراک و کلید های این کاربر عوض می شود و لینک قبلی دیگر کار نمی کند. ادامه می دهید؟"), cbd("urvy", id), cbd("ue", id))
	})
	on("urvy", func(r *req, a []string) {
		id := argInt(a, 0)
		if err := r.call("POST", fmt.Sprintf("/users/%d/revoke-sub", id), nil, nil); err != nil {
			r.fail(err, cbd("uv", id))
			return
		}
		r.toast(T("✅ لینک تازه ساخته شد"))
		userView(r, []string{strconv.FormatInt(id, 10)})
	})
	onInput("ufield", userFieldInput)
	on("uc", userConns)
	on("uci", userConnIP)
	on("ucd", userConnDevice)
	// create
	on("un", userNew)
	onInput("unname", userNewName)
	on("unl", func(r *req, a []string) { r.st.draft["gb"] = "0"; userNewDaysAsk(r) })
	onInput("ungb", userNewGB)
	on("und", func(r *req, a []string) {
		r.st.draft["days"], r.st.draft["hold"] = argStr(a, 0), argStr(a, 1)
		userNewInbounds(r, nil)
	})
	onInput("undays", userNewDays)
	on("unh", func(r *req, a []string) {
		r.st.draft["hold"] = "1"
		r.ask("undays", nil, T("کاربر جدید"), T("شروع از اولین اتصال: چند روز بعد از اولین اتصال منقضی شود؟"), "ul:1")
	})
	on("unib", userNewInbounds)
	on("unok", userNewCreate)
}

func (r *req) loadInbounds() map[int64]string {
	var list []struct {
		ID       int64  `json:"id"`
		Remark   string `json:"remark"`
		Protocol string `json:"protocol"`
	}
	out := map[int64]string{}
	if r.call("GET", "/inbounds", nil, &list) == nil {
		for _, i := range list {
			out[i.ID] = i.Remark
		}
	}
	return out
}

func usersList(r *req, a []string) {
	page := int(argInt(a, 0))
	if page < 1 {
		page = 1
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(usersPerPage)}}
	if r.st.userSearch != "" {
		q.Set("search", r.st.userSearch)
	}
	if r.st.userFilter != "" {
		q.Set("status", r.st.userFilter)
	}
	var res struct {
		Items []users.View `json:"items"`
		Total int64        `json:"total"`
	}
	if err := r.call("GET", "/users?"+q.Encode(), nil, &res); err != nil {
		r.fail(err, "home")
		return
	}
	pages := int((res.Total + usersPerPage - 1) / usersPerPage)
	if pages < 1 {
		pages = 1
	}
	text := header("👥", T("کاربران"))
	filter := T("همه")
	for _, f := range userFilters {
		if f.key == r.st.userFilter {
			filter = f.label
		}
	}
	text += Tf("نمایش: <b>%s</b> · تعداد: <b>%s</b>\n", filter, faInt(res.Total))
	if r.st.userSearch != "" {
		text += T("🔍 جستجو: ") + code(r.st.userSearch) + "\n"
	}
	if len(res.Items) == 0 {
		text += T("\nکاربری پیدا نشد.")
	} else {
		text += T("\nبرای دیدن جزئیات روی کاربر بزنید.")
	}
	kb := Keyboard{}
	for _, u := range res.Items {
		dot := statusDot[u.Status]
		if u.Online {
			dot = "⚡️"
		}
		lim := "∞"
		if u.DataLimit > 0 {
			lim = size(u.DataLimit)
		}
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s / %s", dot, cut(u.Username, 18), size(u.UsedTraffic), lim), "uv", u.ID)})
	}
	if p := pager("ul", page, pages); p != nil {
		kb = append(kb, p)
	}
	// Filter row: the next filter in the cycle.
	next := userFilters[0]
	for i, f := range userFilters {
		if f.key == r.st.userFilter {
			next = userFilters[(i+1)%len(userFilters)]
		}
	}
	kb = append(kb, []Button{btn(T("➕ کاربر جدید"), "un"), btn(T("🔍 جستجو"), "us"), btn("🔀 "+next.label, "uf", next.key)})
	if r.st.userSearch != "" || r.st.userFilter != "" {
		kb = append(kb, []Button{btn(T("✖️ حذف فیلتر ها"), "usc")})
	}
	kb = append(kb, nav("home"))
	r.show(view{text: text, kb: kb})
}

func bar(used, limit int64) string {
	if limit <= 0 {
		return ""
	}
	p := float64(used) / float64(limit)
	if p > 1 {
		p = 1
	}
	n := int(p*10 + 0.5)
	return strings.Repeat("▰", n) + strings.Repeat("▱", 10-n)
}

func (r *req) getUser(id int64) (*users.View, error) {
	var u users.View
	if err := r.call("GET", fmt.Sprintf("/users/%d", id), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

var weekdays = []string{T("یکشنبه"), T("دوشنبه"), T("سه شنبه"), T("چهارشنبه"), T("پنجشنبه"), T("جمعه"), T("شنبه")}

func userText(u *users.View, ibs map[int64]string, loc *time.Location, now time.Time) string {
	t := header("👤", u.Username)
	st := statusLabel[u.Status]
	if u.Online {
		st += T(" · ⚡️ آنلاین")
	}
	t += T("وضعیت: ") + st + "\n"
	if u.SuspendReason == "window" {
		t += T("⏸ بیرون از بازه ساعت مجاز\n")
	} else if u.SuspendReason == "daily" {
		t += T("⏸ سقف روزانه پر شده است\n")
	}
	t += T("\n📦 مصرف: <b>") + size(u.UsedTraffic) + "</b>"
	if u.DataLimit > 0 {
		pct := u.UsedTraffic * 100 / u.DataLimit
		t += T(" از ") + size(u.DataLimit) + " (" + faInt(pct) + T("٪)\n") + bar(u.UsedTraffic, u.DataLimit) + "\n"
	} else {
		t += T(" · حجم نامحدود\n")
	}
	t += "↑ " + size(u.Up) + "  ↓ " + size(u.Down) + "\n"
	switch {
	case u.ExpireAt != nil:
		d := u.ExpireAt.Sub(now)
		rem := left(d) + T(" مانده")
		if d <= 0 {
			rem = left(d) + T(" پیش")
		}
		t += T("⏳ انقضا: ") + jdate(*u.ExpireAt, loc) + " (" + rem + ")\n"
	case u.OnHoldDays != nil:
		t += "⏳ " + faInt(*u.OnHoldDays) + T(" روز، از اولین اتصال\n")
	default:
		t += T("⏳ زمان نامحدود\n")
	}
	if u.LastOnlineAt != nil {
		t += T("🕘 آخرین اتصال: ") + jdate(*u.LastOnlineAt, loc) + "\n"
	}
	var names []string
	for _, id := range u.InboundIDs {
		if n, ok := ibs[id]; ok {
			names = append(names, esc(n))
		}
	}
	if len(names) == 0 {
		t += T("📥 ورودی ها: <i>هیچ</i>\n")
	} else {
		t += T("📥 ورودی ها: ") + strings.Join(names, T("، ")) + "\n"
	}
	var lim []string
	if u.IPLimit > 0 {
		lim = append(lim, "IP "+faInt(u.IPLimit))
	}
	if u.DeviceLimit > 0 {
		lim = append(lim, T("دستگاه ")+faInt(u.DeviceLimit))
	}
	if u.SpeedLimitDown > 0 || u.SpeedLimitUp > 0 {
		lim = append(lim, T("سرعت ↓")+mbps(u.SpeedLimitDown)+" ↑"+mbps(u.SpeedLimitUp))
	}
	if u.DailyLimit > 0 {
		lim = append(lim, T("روزانه ")+size(u.DailyLimit))
	}
	if u.TrafficMultiplier != 1 && u.TrafficMultiplier > 0 {
		lim = append(lim, T("ضریب ")+fa(strconv.FormatFloat(u.TrafficMultiplier, 'f', -1, 64)))
	}
	if u.WindowFrom != "" {
		lim = append(lim, T("ساعت ")+fa(u.WindowFrom+"-"+u.WindowTo))
	}
	if u.BlockTorrent {
		lim = append(lim, T("بدون تورنت"))
	}
	if len(lim) > 0 {
		t += T("🎛 محدودیت ها: ") + strings.Join(lim, " · ") + "\n"
	}
	if len(u.Firewall) > 0 {
		t += T("🛡 فایروال: ") + faInt(len(u.Firewall)) + T(" مورد بسته") + "\n"
	}
	if u.ResetStrategy != "" && u.ResetStrategy != "no_reset" {
		t += T("🔁 ریست دوره ای: ") + resetLabel[u.ResetStrategy] + "\n"
	}
	if u.Note != "" {
		t += T("🔒 یادداشت شخصی: ") + esc(cut(u.Note, 200)) + "\n"
	}
	if u.Description != "" {
		t += T("💬 توضیحات برای کاربر: ") + esc(cut(u.Description, 200)) + "\n"
	}
	return t
}

func mbps(v float64) string {
	if v <= 0 {
		return "∞"
	}
	return fa(strconv.FormatFloat(v, 'f', -1, 64))
}

var resetLabel = map[string]string{"no_reset": T("ندارد"), "day": T("روزانه"), "week": T("هفتگی"), "month": T("ماهانه")}

func userView(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	text := userText(u, r.loadInbounds(), r.b.loc(), r.b.now())
	toggle := btn(T("⏸ غیرفعال کردن"), "ut", id)
	if u.Status == "disabled" {
		toggle = btn(T("▶️ فعال کردن"), "ut", id)
	}
	r.show(view{text: text, kb: Keyboard{
		{btn(T("🔗 لینک و QR"), "uq", id), btn(T("♻️ تمدید"), "ur", id)},
		{btn(T("✏️ ویرایش"), "ue", id), btn(T("📡 اتصال ها"), "uc", id)},
		{toggle, btn(T("🗑 حذف"), "ud", id)},
		{btn(T("🔄 تازه سازی"), "uv", id)},
		nav("ul:1"),
	}})
}

func userQR(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	r.toast(T("⏳ در حال ساخت QR"))
	png, err := qrcode.Encode(u.SubURL, qrcode.Medium, 512)
	if err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	cap := T("🔗 <b>لینک اشتراک ") + esc(u.Username) + "</b>\n" + code(u.SubURL)
	if err := r.cl.SendPhoto(r.ctx, r.chat, png, cap); err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	// The menu moves under the photo.
	if r.msgID != 0 {
		_ = r.cl.Delete(r.ctx, r.chat, r.msgID)
	}
	r.msgID = 0
	userView(r, []string{strconv.FormatInt(id, 10)})
}

func userToggle(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	st := "disabled"
	if u.Status == "disabled" {
		st = "active"
	}
	if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), map[string]any{"status": st}, nil); err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	if st == "active" {
		r.toast(T("✅ فعال شد"))
	} else {
		r.toast(T("⏸ غیرفعال شد"))
	}
	userView(r, a)
}

func confirm(r *req, title, question, yes, no string) {
	r.show(view{text: header("⚠️", title) + question, kb: Keyboard{{Button{Text: T("✅ بله، انجام بده"), Data: yes}, Button{Text: T("✖️ نه"), Data: no}}}})
}

func userDeleteAsk(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	confirm(r, T("حذف کاربر"), T("کاربر <b>")+esc(u.Username)+T("</b> برای همیشه حذف شود؟ این کار برگشت ندارد."), cbd("udy", id), cbd("uv", id))
}

func userDelete(r *req, a []string) {
	id := argInt(a, 0)
	if err := r.call("DELETE", fmt.Sprintf("/users/%d", id), nil, nil); err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	r.toast(T("🗑 حذف شد"))
	usersList(r, []string{"1"})
}

func userRenew(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	text := header("♻️", T("تمدید ")+u.Username)
	if u.ExpireAt == nil && u.OnHoldDays == nil {
		text += T("این کاربر زمان نامحدود دارد؛ افزودن روز روی او اثری ندارد (برای گذاشتن تاریخ از ویرایش استفاده کنید).\n")
	}
	if u.DataLimit == 0 {
		text += T("حجم این کاربر نامحدود است.\n")
	} else {
		text += T("مصرف: ") + size(u.UsedTraffic) + T(" از ") + size(u.DataLimit) + "\n"
	}
	text += T("\nچه کاری انجام شود؟")
	r.show(view{text: text, kb: Keyboard{
		{btn(T("➕ ۷ روز"), "ura", id, 7), btn(T("➕ ۳۰ روز"), "ura", id, 30), btn(T("➕ ۹۰ روز"), "ura", id, 90)},
		{btn(T("✍️ روز دلخواه"), "urd", id)},
		{btn(T("➕ ۱۰ GB"), "urg", id, 10), btn(T("➕ ۵۰ GB"), "urg", id, 50), btn(T("➕ ۱۰۰ GB"), "urg", id, 100)},
		{btn(T("✍️ حجم دلخواه"), "urG", id)},
		{btn(T("🧹 صفر کردن مصرف"), "urr", id)},
		nav(cbd("uv", id)),
	}})
}

func userExtend(r *req, id, days int64) {
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	if u.ExpireAt == nil && u.OnHoldDays == nil {
		r.alert(T("این کاربر زمان نامحدود دارد."))
		userRenew(r, []string{strconv.FormatInt(id, 10)})
		return
	}
	if err := r.call("POST", "/users/bulk", map[string]any{"action": "extend", "ids": []int64{id}, "days": days}, nil); err != nil {
		r.fail(err, cbd("ur", id))
		return
	}
	r.toast("✅ " + faInt(days) + T(" روز اضافه شد"))
	userView(r, []string{strconv.FormatInt(id, 10)})
}

func userAddTraffic(r *req, id int64, n float64) {
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	if u.DataLimit == 0 {
		r.alert(T("حجم این کاربر نامحدود است."))
		userRenew(r, []string{strconv.FormatInt(id, 10)})
		return
	}
	if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), map[string]any{"data_limit": u.DataLimit + gb(n)}, nil); err != nil {
		r.fail(err, cbd("ur", id))
		return
	}
	r.toast(T("✅ حجم اضافه شد"))
	userView(r, []string{strconv.FormatInt(id, 10)})
}

func userPatch(r *req, id int64, body map[string]any, back string) {
	if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), body, nil); err != nil {
		r.fail(err, cbd(back, id))
		return
	}
	r.toast(T("✅ ذخیره شد"))
	if back == "ue" {
		userEditMenu(r, []string{strconv.FormatInt(id, 10)})
		return
	}
	userView(r, []string{strconv.FormatInt(id, 10)})
}

func userFlip(r *req, id int64, field string) {
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	cur := u.RequireHWID
	if field == "block_torrent" {
		cur = u.BlockTorrent
	}
	userPatch(r, id, map[string]any{field: !cur}, "ue")
}

// userFields are the fields edited by typing a value.
var userFields = map[string]struct{ label, prompt string }{
	"name": {T("نام کاربری"), T("نام کاربری تازه را بفرستید (۳ تا ۳۲ حرف انگلیسی، رقم، _ . @ -):")},
	"lim":  {T("حجم کل"), T("حجم کل به گیگابایت را بفرستید (۰ یعنی نامحدود):")},
	"exp":  {T("مدت اعتبار"), T("چند روز از امروز معتبر باشد؟ (۰ یعنی زمان نامحدود)")},
	"hold": {T("شروع از اولین اتصال"), T("چند روز بعد از اولین اتصال منقضی شود؟ (تاریخ فعلی برداشته می شود)")},
	"note": {T("یادداشت شخصی"), T("یادداشت شخصی را بفرستید؛ فقط مدیر ها آن را می بینند (- برای پاک کردن):")},
	"desc": {T("توضیحات برای کاربر"), T("توضیحات برای کاربر را بفرستید؛ پایین صفحه اشتراک کاربر نمایش داده می شود (حداکثر ۱۰۰۰ نویسه، - برای پاک کردن):")},
	"ipl":  {T("محدودیت IP"), T("حداکثر تعداد IP همزمان (۰ یعنی بدون محدودیت):")},
	"dev":  {T("محدودیت دستگاه"), T("حداکثر تعداد دستگاه (۰ یعنی بدون محدودیت):")},
	"spd":  {T("سرعت دانلود"), T("سقف سرعت دانلود به Mbps (۰ یعنی نامحدود):")},
	"spu":  {T("سرعت آپلود"), T("سقف سرعت آپلود به Mbps (۰ یعنی نامحدود):")},
	"mul":  {T("ضریب مصرف"), T("ضریب مصرف را بفرستید (۱ یعنی عادی، مثلا ۱.۵):")},
	"dly":  {T("سقف روزانه"), T("سقف مصرف روزانه به گیگابایت (۰ یعنی ندارد):")},
	"win":  {T("بازه ساعت مجاز"), T("بازه را به شکل 08:00-23:00 بفرستید (- برای همیشه مجاز):")},
	"pxu":  {T("نام کاربری پراکسی"), T("نام کاربری پراکسی برای ورودی های HTTP، Mixed و SOCKS را بفرستید (۳ تا ۳۲ حرف انگلیسی، رقم، . _ -؛ - برای خودکار):")},
	"pxp":  {T("رمز پراکسی"), T("رمز پراکسی برای ورودی های HTTP، Mixed و SOCKS را بفرستید (۸ تا ۶۴ نویسه؛ - برای خودکار):")},
}

func userEditMenu(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	f := func(k string) Button { return btn("✏️ "+userFields[k].label, "uef", id, k) }
	text := header("✏️", T("ویرایش ")+u.Username) + T("کدام مورد تغییر کند؟\n\n")
	text += T("🔁 ریست دوره ای: <b>") + resetLabel[u.ResetStrategy] + "</b>\n"
	text += onOff(u.RequireHWID) + T(" الزام شناسه دستگاه (HWID)\n") + onOff(u.BlockTorrent) + T(" مسدود کردن تورنت\n")
	days := T("همه روز ها")
	if u.WindowDays != 0 {
		var ds []string
		for i, d := range weekdays {
			if u.WindowDays&(1<<i) != 0 {
				ds = append(ds, d)
			}
		}
		days = strings.Join(ds, T("، "))
	}
	text += T("📅 روز های مجاز: ") + days + "\n"
	r.show(view{text: text, kb: Keyboard{
		{f("name"), f("lim")},
		{f("exp"), f("hold")},
		{f("ipl"), f("dev")},
		{f("spd"), f("spu")},
		{f("dly"), f("mul")},
		{f("win"), btn(T("📅 روز های مجاز"), "uef", id, "days")},
		{btn(T("📥 ورودی ها"), "uef", id, "inb"), btn(T("🏷 گروه"), "uef", id, "grp")},
		{btn(T("🔁 ریست دوره ای"), "uef", id, "rst"), f("note")},
		{f("desc")},
		{f("pxu"), f("pxp")},
		{btn(onOff(u.RequireHWID)+" HWID", "uht", id), btn(onOff(u.BlockTorrent)+T(" تورنت"), "utt", id)},
		{btn(T("🛡 فایروال")+fwCount(len(u.Firewall)), "ufw", id), btn(T("🔑 تعویض لینک اشتراک"), "urv", id)},
		nav(cbd("uv", id)),
	}})
}

// fwCount is the number shown on the firewall button (" (3)"; "" when
// the list is empty).
func fwCount(n int) string {
	if n == 0 {
		return ""
	}
	return " (" + faInt(n) + ")"
}

func userEditField(r *req, a []string) {
	id, k := argInt(a, 0), argStr(a, 1)
	back := cbd("ue", id)
	switch k {
	case "rst":
		r.show(view{text: header("🔁", T("ریست دوره ای مصرف")), kb: Keyboard{
			{btn(T("ندارد"), "urs", id, "no_reset"), btn(T("روزانه"), "urs", id, "day")},
			{btn(T("هفتگی"), "urs", id, "week"), btn(T("ماهانه"), "urs", id, "month")},
			nav(back),
		}})
	case "days":
		userWeekday(r, []string{strconv.FormatInt(id, 10)})
	case "inb":
		userInboundToggle(r, []string{strconv.FormatInt(id, 10)})
	case "grp":
		var gs []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := r.call("GET", "/groups", nil, &gs); err != nil {
			r.fail(err, back)
			return
		}
		bs := []Button{btn(T("بدون گروه"), "ugp", id, 0)}
		for _, g := range gs {
			bs = append(bs, btn("🏷 "+cut(g.Name, 20), "ugp", id, g.ID))
		}
		kb := chunk(bs, 2)
		r.show(view{text: header("🏷", T("گروه کاربر")) + T("گروه را انتخاب کنید:"), kb: append(kb, nav(back))})
	default:
		fd, ok := userFields[k]
		if !ok {
			userEditMenu(r, a)
			return
		}
		r.ask("ufield", []string{strconv.FormatInt(id, 10), k}, fd.label, fd.prompt, back)
	}
}

func userFieldInput(r *req, a []string, text string, _ *Document) {
	id, k := argInt(a, 0), argStr(a, 1)
	again := func(msg string) {
		fd := userFields[k]
		r.ask("ufield", a, fd.label, "⚠️ "+msg+"\n"+fd.prompt, cbd("ue", id))
	}
	body := map[string]any{}
	num := func() (float64, bool) {
		n, ok := parseNum(text)
		if !ok || n < 0 {
			again(T("یک عدد درست بفرستید."))
			return 0, false
		}
		return n, true
	}
	switch k {
	case "name":
		body["username"] = text
	case "note":
		if text == "-" {
			text = ""
		}
		body["note"] = text
	case "desc":
		if strings.TrimSpace(text) == "-" {
			text = ""
		}
		body["description"] = text
	case "pxu", "pxp":
		t := strings.TrimSpace(text)
		if t == "-" {
			t = ""
		}
		body[map[string]string{"pxu": "proxy_username", "pxp": "proxy_password"}[k]] = t
	case "lim":
		n, ok := num()
		if !ok {
			return
		}
		body["data_limit"] = gb(n)
	case "dly":
		n, ok := num()
		if !ok {
			return
		}
		body["daily_limit"] = gb(n)
	case "exp":
		n, ok := parseInt(text)
		if !ok || n < 0 {
			again(T("یک عدد صحیح بفرستید."))
			return
		}
		if n == 0 {
			body["expire_at"], body["on_hold_days"] = nil, nil
		} else {
			body["expire_at"] = r.b.now().Add(time.Duration(n) * 24 * time.Hour).Format(time.RFC3339)
			body["on_hold_days"] = nil
		}
	case "hold":
		n, ok := parseInt(text)
		if !ok || n < 1 {
			again(T("یک عدد صحیح از ۱ به بالا بفرستید."))
			return
		}
		body["on_hold_days"], body["expire_at"] = n, nil
	case "ipl", "dev":
		n, ok := parseInt(text)
		if !ok || n < 0 {
			again(T("یک عدد صحیح بفرستید."))
			return
		}
		if k == "ipl" {
			body["ip_limit"] = n
		} else {
			body["device_limit"] = n
		}
	case "spd", "spu", "mul":
		n, ok := num()
		if !ok {
			return
		}
		body[map[string]string{"spd": "speed_limit_down", "spu": "speed_limit_up", "mul": "traffic_multiplier"}[k]] = n
	case "win":
		t := strings.TrimSpace(toLatinDigits(text))
		if t == "-" || t == "" {
			body["window_from"], body["window_to"] = "", ""
		} else {
			from, to, ok := strings.Cut(strings.ReplaceAll(t, " ", ""), "-")
			if !ok {
				again(T("شکل درست: 08:00-23:00"))
				return
			}
			body["window_from"], body["window_to"] = from, to
		}
	}
	if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), body, nil); err != nil {
		r.fail(err, cbd("ue", id))
		return
	}
	userEditMenu(r, []string{strconv.FormatInt(id, 10)})
}

func userWeekday(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	if len(a) > 1 {
		bit := argInt(a, 1)
		mask := u.WindowDays ^ (1 << bit)
		if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), map[string]any{"window_days": mask}, nil); err != nil {
			r.fail(err, cbd("ue", id))
			return
		}
		u.WindowDays = mask
	}
	// Saturday first, as in the Persian week.
	order := []int{6, 0, 1, 2, 3, 4, 5}
	var bs []Button
	for _, i := range order {
		bs = append(bs, btn(onOff(u.WindowDays&(1<<i) != 0)+" "+weekdays[i], "uwd", id, i))
	}
	text := header("📅", T("روز های مجاز ")+u.Username) + T("روز هایی که کاربر می تواند وصل شود. اگر هیچ روزی انتخاب نشود، همه روز ها مجاز است.")
	r.show(view{text: text, kb: append(chunk(bs, 3), nav(cbd("ue", id)))})
}

type inboundRow struct {
	ID       int64  `json:"id"`
	Remark   string `json:"remark"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Enabled  bool   `json:"enabled"`
}

func userInboundToggle(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	var list []inboundRow
	if err := r.call("GET", "/inbounds", nil, &list); err != nil {
		r.fail(err, cbd("ue", id))
		return
	}
	have := map[int64]bool{}
	for _, x := range u.InboundIDs {
		have[x] = true
	}
	if len(a) > 1 {
		t := argInt(a, 1)
		have[t] = !have[t]
		ids := []int64{}
		for _, ib := range list {
			if have[ib.ID] {
				ids = append(ids, ib.ID)
			}
		}
		if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), map[string]any{"inbound_ids": ids}, nil); err != nil {
			r.fail(err, cbd("ue", id))
			return
		}
		r.toast(T("✅ ذخیره شد"))
	}
	kb := Keyboard{}
	for _, ib := range list {
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s:%s", onOff(have[ib.ID]), cut(ib.Remark, 20), ib.Protocol, fa(strconv.Itoa(ib.Port))), "uib", id, ib.ID)})
	}
	r.show(view{text: header("📥", T("ورودی های ")+u.Username) + T("با زدن هر ورودی، آن را برای کاربر روشن یا خاموش کنید."), kb: append(kb, nav(cbd("ue", id)))})
}

type connIP struct {
	ID       int64     `json:"id"`
	IP       string    `json:"ip"`
	Country  string    `json:"country"`
	LastSeen time.Time `json:"last_seen"`
	Blocked  bool      `json:"blocked"`
	Live     bool      `json:"live"`
}

type connDev struct {
	ID       int64     `json:"id"`
	HWID     string    `json:"hwid"`
	OS       string    `json:"os"`
	Model    string    `json:"model"`
	LastSeen time.Time `json:"last_seen"`
	Blocked  bool      `json:"blocked"`
}

type conns struct {
	IPs     []connIP  `json:"ips"`
	Devices []connDev `json:"devices"`
}

func userConns(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	var c conns
	if err := r.call("GET", fmt.Sprintf("/users/%d/connections", id), nil, &c); err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	loc := r.b.loc()
	sort.SliceStable(c.IPs, func(i, j int) bool { return c.IPs[i].LastSeen.After(c.IPs[j].LastSeen) })
	text := header("📡", T("اتصال های ")+u.Username)
	kb := Keyboard{}
	text += T("<b>IP ها</b> (") + faInt(len(c.IPs)) + ")\n"
	for i, ip := range c.IPs {
		if i >= 8 {
			text += "…\n"
			break
		}
		mark := "▫️"
		if ip.Live {
			mark = "⚡️"
		}
		if ip.Blocked {
			mark = "⛔️"
		}
		text += mark + " " + code(ip.IP) + " " + esc(ip.Country) + " · " + jdate(ip.LastSeen, loc) + "\n"
		act := T("⛔️ مسدود")
		if ip.Blocked {
			act = T("✅ آزاد")
		}
		kb = append(kb, []Button{btn(act+" "+ip.IP, "uci", id, ip.ID, "b"), btn("🗑", "uci", id, ip.ID, "d")})
	}
	text += T("\n<b>دستگاه ها</b> (") + faInt(len(c.Devices)) + ")\n"
	for i, d := range c.Devices {
		if i >= 6 {
			text += "…\n"
			break
		}
		mark := "📱"
		if d.Blocked {
			mark = "⛔️"
		}
		name := strings.TrimSpace(d.Model + " " + d.OS)
		if name == "" {
			name = cut(d.HWID, 16)
		}
		text += mark + " " + esc(name) + " · " + jdate(d.LastSeen, loc) + "\n"
		act := T("⛔️ مسدود")
		if d.Blocked {
			act = T("✅ آزاد")
		}
		kb = append(kb, []Button{btn(act+" "+cut(name, 18), "ucd", id, d.ID, "b"), btn("🗑", "ucd", id, d.ID, "d")})
	}
	kb = append(kb, []Button{btn(T("🔄 تازه سازی"), "uc", id)}, nav(cbd("uv", id)))
	r.show(view{text: text, kb: kb})
}

func userConnIP(r *req, a []string) {
	id, sid, op := argInt(a, 0), argInt(a, 1), argStr(a, 2)
	var c conns
	if err := r.call("GET", fmt.Sprintf("/users/%d/connections", id), nil, &c); err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	var ip *connIP
	for i := range c.IPs {
		if c.IPs[i].ID == sid {
			ip = &c.IPs[i]
		}
	}
	if ip == nil {
		userConns(r, a)
		return
	}
	var err error
	if op == "d" {
		err = r.call("DELETE", fmt.Sprintf("/users/%d/ips/%d", id, sid), nil, nil)
	} else {
		err = r.call("POST", fmt.Sprintf("/users/%d/ips", id), map[string]any{"ip": ip.IP, "blocked": !ip.Blocked}, nil)
	}
	if err != nil {
		r.fail(err, cbd("uc", id))
		return
	}
	r.toast(T("✅ انجام شد"))
	userConns(r, a)
}

func userConnDevice(r *req, a []string) {
	id, sid, op := argInt(a, 0), argInt(a, 1), argStr(a, 2)
	var err error
	if op == "d" {
		err = r.call("DELETE", fmt.Sprintf("/users/%d/devices/%d", id, sid), nil, nil)
	} else {
		var c conns
		if err = r.call("GET", fmt.Sprintf("/users/%d/connections", id), nil, &c); err == nil {
			blocked := false
			for _, d := range c.Devices {
				if d.ID == sid {
					blocked = d.Blocked
				}
			}
			err = r.call("PATCH", fmt.Sprintf("/users/%d/devices/%d", id, sid), map[string]any{"blocked": !blocked}, nil)
		}
	}
	if err != nil {
		r.fail(err, cbd("uc", id))
		return
	}
	r.toast(T("✅ انجام شد"))
	userConns(r, a)
}

// --- create ---------------------------------------------------------------

func userNew(r *req, _ []string) {
	r.st.draft = map[string]string{}
	r.ask("unname", nil, T("کاربر جدید (۱ از ۴)"), T("نام کاربری را بفرستید (۳ تا ۳۲ حرف انگلیسی، رقم، _ . @ -):"), "ul:1")
}

func userNewName(r *req, _ []string, text string, _ *Document) {
	r.st.draft["name"] = strings.TrimSpace(text)
	r.ask("ungb", nil, T("کاربر جدید (۲ از ۴)"), T("نام: ")+code(text)+T("\n\nحجم به گیگابایت را بفرستید:"), "ul:1",
		[]Button{btn(T("♾ حجم نامحدود"), "unl")})
}

func userNewGB(r *req, _ []string, text string, _ *Document) {
	n, ok := parseNum(text)
	if !ok || n < 0 {
		r.ask("ungb", nil, T("کاربر جدید (۲ از ۴)"), T("⚠️ یک عدد درست بفرستید. حجم به گیگابایت:"), "ul:1", []Button{btn(T("♾ حجم نامحدود"), "unl")})
		return
	}
	r.st.draft["gb"] = strconv.FormatFloat(n, 'f', -1, 64)
	userNewDaysAsk(r)
}

func userNewDaysAsk(r *req) {
	r.st.draft["hold"] = ""
	r.ask("undays", nil, T("کاربر جدید (۳ از ۴)"), T("چند روز اعتبار داشته باشد؟"), "ul:1",
		[]Button{btn(T("۳۰ روز"), "und", 30, ""), btn(T("۶۰ روز"), "und", 60, ""), btn(T("۹۰ روز"), "und", 90, "")},
		[]Button{btn(T("♾ زمان نامحدود"), "und", 0, ""), btn(T("🔵 از اولین اتصال"), "unh")})
}

func userNewDays(r *req, _ []string, text string, _ *Document) {
	n, ok := parseInt(text)
	if !ok || n < 0 {
		r.ask("undays", nil, T("کاربر جدید (۳ از ۴)"), T("⚠️ یک عدد صحیح بفرستید. چند روز؟"), "ul:1")
		return
	}
	r.st.draft["days"] = strconv.FormatInt(n, 10)
	userNewInbounds(r, nil)
}

func userNewInbounds(r *req, a []string) {
	var list []inboundRow
	if err := r.call("GET", "/inbounds", nil, &list); err != nil {
		r.fail(err, "ul:1")
		return
	}
	sel := map[string]bool{}
	for _, s := range strings.Split(r.st.draft["inb"], ",") {
		if s != "" {
			sel[s] = true
		}
	}
	if _, ok := r.st.draft["inb"]; !ok {
		// Every enabled inbound is chosen at first.
		for _, ib := range list {
			if ib.Enabled {
				sel[strconv.FormatInt(ib.ID, 10)] = true
			}
		}
	}
	if len(a) > 0 {
		k := a[0]
		sel[k] = !sel[k]
	}
	var keep []string
	for _, ib := range list {
		if k := strconv.FormatInt(ib.ID, 10); sel[k] {
			keep = append(keep, k)
		}
	}
	r.st.draft["inb"] = strings.Join(keep, ",")
	d := r.st.draft
	text := header("➕", T("کاربر جدید (۴ از ۴)")) + T("نام: ") + code(d["name"]) + "\n"
	if g, _ := parseNum(d["gb"]); g > 0 {
		text += T("حجم: ") + fa(d["gb"]) + " GB\n"
	} else {
		text += T("حجم: نامحدود\n")
	}
	switch {
	case d["hold"] == "1":
		text += T("زمان: ") + fa(d["days"]) + T(" روز از اولین اتصال\n")
	case d["days"] == "0":
		text += T("زمان: نامحدود\n")
	default:
		text += T("زمان: ") + fa(d["days"]) + T(" روز\n")
	}
	text += T("\nورودی های کاربر را انتخاب کنید و بعد «ساختن» را بزنید:")
	kb := Keyboard{}
	for _, ib := range list {
		k := strconv.FormatInt(ib.ID, 10)
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s", onOff(sel[k]), cut(ib.Remark, 22), ib.Protocol), "unib", ib.ID)})
	}
	kb = append(kb, []Button{btn(T("✅ ساختن کاربر"), "unok")}, []Button{btn(T("✖️ انصراف"), "ul", 1)})
	r.show(view{text: text, kb: kb})
}

func userNewCreate(r *req, _ []string) {
	d := r.st.draft
	if d["name"] == "" {
		userNew(r, nil)
		return
	}
	g, _ := parseNum(d["gb"])
	days, _ := strconv.ParseInt(d["days"], 10, 64)
	ids := []int64{}
	for _, s := range strings.Split(d["inb"], ",") {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, n)
		}
	}
	body := map[string]any{"username": d["name"], "data_limit": gb(g), "inbound_ids": ids}
	switch {
	case d["hold"] == "1" && days > 0:
		body["on_hold_days"] = days
	case days > 0:
		body["expire_at"] = r.b.now().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	}
	var u users.View
	if err := r.call("POST", "/users", body, &u); err != nil {
		r.fail(err, "un")
		return
	}
	r.st.draft = map[string]string{}
	r.toast(T("✅ کاربر ساخته شد"))
	userView(r, []string{strconv.FormatInt(u.ID, 10)})
}
