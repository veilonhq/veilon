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
	"active":   "🟢 فعال",
	"disabled": "⚪️ غیرفعال",
	"limited":  "🔴 حجم تمام شده",
	"expired":  "🟠 منقضی",
	"on_hold":  "🔵 در انتظار اولین اتصال",
}

var statusDot = map[string]string{"active": "🟢", "disabled": "⚪️", "limited": "🔴", "expired": "🟠", "on_hold": "🔵"}

var userFilters = []struct{ key, label string }{
	{"", "همه"}, {"online", "آنلاین"}, {"active", "فعال"}, {"expired", "منقضی"}, {"limited", "حجم تمام"}, {"disabled", "غیرفعال"}, {"on_hold", "در انتظار"},
}

func init() {
	on("ul", usersList)
	on("uf", func(r *req, a []string) {
		r.st.userFilter = argStr(a, 0)
		usersList(r, []string{"1"})
	})
	on("us", func(r *req, a []string) {
		r.ask("usearch", nil, "جستجوی کاربر", "بخشی از نام کاربری یا یادداشت را بفرستید:", "ul:1")
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
		r.ask("uextend", a, "تمدید زمان", "چند روز اضافه شود؟ (یک عدد، مثلا ۳۰)", cbd("ur", id))
	})
	onInput("uextend", func(r *req, a []string, text string, _ *Document) {
		n, ok := parseInt(text)
		if !ok || n < 1 {
			r.ask("uextend", a, "تمدید زمان", "⚠️ یک عدد درست از ۱ به بالا بفرستید:", cbd("ur", argInt(a, 0)))
			return
		}
		userExtend(r, argInt(a, 0), n)
	})
	on("urg", func(r *req, a []string) { userAddTraffic(r, argInt(a, 0), float64(argInt(a, 1))) })
	on("urG", func(r *req, a []string) {
		r.ask("uaddgb", a, "افزودن حجم", "چند گیگابایت اضافه شود؟ (مثلا ۲۰ یا ۰.۵)", cbd("ur", argInt(a, 0)))
	})
	onInput("uaddgb", func(r *req, a []string, text string, _ *Document) {
		n, ok := parseNum(text)
		if !ok || n <= 0 {
			r.ask("uaddgb", a, "افزودن حجم", "⚠️ یک عدد بزرگتر از صفر بفرستید:", cbd("ur", argInt(a, 0)))
			return
		}
		userAddTraffic(r, argInt(a, 0), n)
	})
	on("urr", func(r *req, a []string) {
		id := argInt(a, 0)
		confirm(r, "صفر کردن مصرف", "مصرف این کاربر صفر شود؟", cbd("urry", id), cbd("ur", id))
	})
	on("urry", func(r *req, a []string) {
		id := argInt(a, 0)
		if err := r.call("POST", fmt.Sprintf("/users/%d/reset-traffic", id), nil, nil); err != nil {
			r.fail(err, cbd("uv", id))
			return
		}
		r.toast("✅ مصرف صفر شد")
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
		confirm(r, "تعویض لینک اشتراک", "لینک اشتراک و کلید های این کاربر عوض می شود و لینک قبلی دیگر کار نمی کند. ادامه می دهید؟", cbd("urvy", id), cbd("ue", id))
	})
	on("urvy", func(r *req, a []string) {
		id := argInt(a, 0)
		if err := r.call("POST", fmt.Sprintf("/users/%d/revoke-sub", id), nil, nil); err != nil {
			r.fail(err, cbd("uv", id))
			return
		}
		r.toast("✅ لینک تازه ساخته شد")
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
		r.ask("undays", nil, "کاربر جدید", "شروع از اولین اتصال: چند روز بعد از اولین اتصال منقضی شود؟", "ul:1")
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
	text := header("👥", "کاربران")
	filter := "همه"
	for _, f := range userFilters {
		if f.key == r.st.userFilter {
			filter = f.label
		}
	}
	text += fmt.Sprintf("نمایش: <b>%s</b> · تعداد: <b>%s</b>\n", filter, faInt(res.Total))
	if r.st.userSearch != "" {
		text += "🔍 جستجو: " + code(r.st.userSearch) + "\n"
	}
	if len(res.Items) == 0 {
		text += "\nکاربری پیدا نشد."
	} else {
		text += "\nبرای دیدن جزئیات روی کاربر بزنید."
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
	kb = append(kb, []Button{btn("➕ کاربر جدید", "un"), btn("🔍 جستجو", "us"), btn("🔀 "+next.label, "uf", next.key)})
	if r.st.userSearch != "" || r.st.userFilter != "" {
		kb = append(kb, []Button{btn("✖️ حذف فیلتر ها", "usc")})
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

var weekdays = []string{"یکشنبه", "دوشنبه", "سه شنبه", "چهارشنبه", "پنجشنبه", "جمعه", "شنبه"}

func userText(u *users.View, ibs map[int64]string, loc *time.Location, now time.Time) string {
	t := header("👤", u.Username)
	st := statusLabel[u.Status]
	if u.Online {
		st += " · ⚡️ آنلاین"
	}
	t += "وضعیت: " + st + "\n"
	if u.SuspendReason == "window" {
		t += "⏸ بیرون از بازه ساعت مجاز\n"
	} else if u.SuspendReason == "daily" {
		t += "⏸ سقف روزانه پر شده است\n"
	}
	t += "\n📦 مصرف: <b>" + size(u.UsedTraffic) + "</b>"
	if u.DataLimit > 0 {
		pct := u.UsedTraffic * 100 / u.DataLimit
		t += " از " + size(u.DataLimit) + " (" + faInt(pct) + "٪)\n" + bar(u.UsedTraffic, u.DataLimit) + "\n"
	} else {
		t += " · حجم نامحدود\n"
	}
	t += "↑ " + size(u.Up) + "  ↓ " + size(u.Down) + "\n"
	switch {
	case u.ExpireAt != nil:
		d := u.ExpireAt.Sub(now)
		rem := left(d) + " مانده"
		if d <= 0 {
			rem = left(d) + " پیش"
		}
		t += "⏳ انقضا: " + jdate(*u.ExpireAt, loc) + " (" + rem + ")\n"
	case u.OnHoldDays != nil:
		t += "⏳ " + faInt(*u.OnHoldDays) + " روز، از اولین اتصال\n"
	default:
		t += "⏳ زمان نامحدود\n"
	}
	if u.LastOnlineAt != nil {
		t += "🕘 آخرین اتصال: " + jdate(*u.LastOnlineAt, loc) + "\n"
	}
	var names []string
	for _, id := range u.InboundIDs {
		if n, ok := ibs[id]; ok {
			names = append(names, esc(n))
		}
	}
	if len(names) == 0 {
		t += "📥 ورودی ها: <i>هیچ</i>\n"
	} else {
		t += "📥 ورودی ها: " + strings.Join(names, "، ") + "\n"
	}
	var lim []string
	if u.IPLimit > 0 {
		lim = append(lim, "IP "+faInt(u.IPLimit))
	}
	if u.DeviceLimit > 0 {
		lim = append(lim, "دستگاه "+faInt(u.DeviceLimit))
	}
	if u.SpeedLimitDown > 0 || u.SpeedLimitUp > 0 {
		lim = append(lim, "سرعت ↓"+mbps(u.SpeedLimitDown)+" ↑"+mbps(u.SpeedLimitUp))
	}
	if u.DailyLimit > 0 {
		lim = append(lim, "روزانه "+size(u.DailyLimit))
	}
	if u.TrafficMultiplier != 1 && u.TrafficMultiplier > 0 {
		lim = append(lim, "ضریب "+fa(strconv.FormatFloat(u.TrafficMultiplier, 'f', -1, 64)))
	}
	if u.WindowFrom != "" {
		lim = append(lim, "ساعت "+fa(u.WindowFrom+"-"+u.WindowTo))
	}
	if u.BlockTorrent {
		lim = append(lim, "بدون تورنت")
	}
	if len(lim) > 0 {
		t += "🎛 محدودیت ها: " + strings.Join(lim, " · ") + "\n"
	}
	if u.ResetStrategy != "" && u.ResetStrategy != "no_reset" {
		t += "🔁 ریست دوره ای: " + resetLabel[u.ResetStrategy] + "\n"
	}
	if u.Note != "" {
		t += "📝 " + esc(cut(u.Note, 200)) + "\n"
	}
	return t
}

func mbps(v float64) string {
	if v <= 0 {
		return "∞"
	}
	return fa(strconv.FormatFloat(v, 'f', -1, 64))
}

var resetLabel = map[string]string{"no_reset": "ندارد", "day": "روزانه", "week": "هفتگی", "month": "ماهانه"}

func userView(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	text := userText(u, r.loadInbounds(), r.b.loc(), r.b.now())
	toggle := btn("⏸ غیرفعال کردن", "ut", id)
	if u.Status == "disabled" {
		toggle = btn("▶️ فعال کردن", "ut", id)
	}
	r.show(view{text: text, kb: Keyboard{
		{btn("🔗 لینک و QR", "uq", id), btn("♻️ تمدید", "ur", id)},
		{btn("✏️ ویرایش", "ue", id), btn("📡 اتصال ها", "uc", id)},
		{toggle, btn("🗑 حذف", "ud", id)},
		{btn("🔄 تازه سازی", "uv", id)},
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
	r.toast("⏳ در حال ساخت QR")
	png, err := qrcode.Encode(u.SubURL, qrcode.Medium, 512)
	if err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	cap := "🔗 <b>لینک اشتراک " + esc(u.Username) + "</b>\n" + code(u.SubURL)
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
		r.toast("✅ فعال شد")
	} else {
		r.toast("⏸ غیرفعال شد")
	}
	userView(r, a)
}

func confirm(r *req, title, question, yes, no string) {
	r.show(view{text: header("⚠️", title) + question, kb: Keyboard{{Button{Text: "✅ بله، انجام بده", Data: yes}, Button{Text: "✖️ نه", Data: no}}}})
}

func userDeleteAsk(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	confirm(r, "حذف کاربر", "کاربر <b>"+esc(u.Username)+"</b> برای همیشه حذف شود؟ این کار برگشت ندارد.", cbd("udy", id), cbd("uv", id))
}

func userDelete(r *req, a []string) {
	id := argInt(a, 0)
	if err := r.call("DELETE", fmt.Sprintf("/users/%d", id), nil, nil); err != nil {
		r.fail(err, cbd("uv", id))
		return
	}
	r.toast("🗑 حذف شد")
	usersList(r, []string{"1"})
}

func userRenew(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	text := header("♻️", "تمدید "+u.Username)
	if u.ExpireAt == nil && u.OnHoldDays == nil {
		text += "این کاربر زمان نامحدود دارد؛ افزودن روز روی او اثری ندارد (برای گذاشتن تاریخ از ویرایش استفاده کنید).\n"
	}
	if u.DataLimit == 0 {
		text += "حجم این کاربر نامحدود است.\n"
	} else {
		text += "مصرف: " + size(u.UsedTraffic) + " از " + size(u.DataLimit) + "\n"
	}
	text += "\nچه کاری انجام شود؟"
	r.show(view{text: text, kb: Keyboard{
		{btn("➕ ۷ روز", "ura", id, 7), btn("➕ ۳۰ روز", "ura", id, 30), btn("➕ ۹۰ روز", "ura", id, 90)},
		{btn("✍️ روز دلخواه", "urd", id)},
		{btn("➕ ۱۰ GB", "urg", id, 10), btn("➕ ۵۰ GB", "urg", id, 50), btn("➕ ۱۰۰ GB", "urg", id, 100)},
		{btn("✍️ حجم دلخواه", "urG", id)},
		{btn("🧹 صفر کردن مصرف", "urr", id)},
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
		r.alert("این کاربر زمان نامحدود دارد.")
		userRenew(r, []string{strconv.FormatInt(id, 10)})
		return
	}
	if err := r.call("POST", "/users/bulk", map[string]any{"action": "extend", "ids": []int64{id}, "days": days}, nil); err != nil {
		r.fail(err, cbd("ur", id))
		return
	}
	r.toast("✅ " + faInt(days) + " روز اضافه شد")
	userView(r, []string{strconv.FormatInt(id, 10)})
}

func userAddTraffic(r *req, id int64, n float64) {
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	if u.DataLimit == 0 {
		r.alert("حجم این کاربر نامحدود است.")
		userRenew(r, []string{strconv.FormatInt(id, 10)})
		return
	}
	if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), map[string]any{"data_limit": u.DataLimit + gb(n)}, nil); err != nil {
		r.fail(err, cbd("ur", id))
		return
	}
	r.toast("✅ حجم اضافه شد")
	userView(r, []string{strconv.FormatInt(id, 10)})
}

func userPatch(r *req, id int64, body map[string]any, back string) {
	if err := r.call("PATCH", fmt.Sprintf("/users/%d", id), body, nil); err != nil {
		r.fail(err, cbd(back, id))
		return
	}
	r.toast("✅ ذخیره شد")
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
	"name": {"نام کاربری", "نام کاربری تازه را بفرستید (۳ تا ۳۲ حرف انگلیسی، رقم، _ . @ -):"},
	"lim":  {"حجم کل", "حجم کل به گیگابایت را بفرستید (۰ یعنی نامحدود):"},
	"exp":  {"مدت اعتبار", "چند روز از امروز معتبر باشد؟ (۰ یعنی زمان نامحدود)"},
	"hold": {"شروع از اولین اتصال", "چند روز بعد از اولین اتصال منقضی شود؟ (تاریخ فعلی برداشته می شود)"},
	"note": {"یادداشت", "یادداشت را بفرستید (- برای پاک کردن):"},
	"ipl":  {"محدودیت IP", "حداکثر تعداد IP همزمان (۰ یعنی بدون محدودیت):"},
	"dev":  {"محدودیت دستگاه", "حداکثر تعداد دستگاه (۰ یعنی بدون محدودیت):"},
	"spd":  {"سرعت دانلود", "سقف سرعت دانلود به Mbps (۰ یعنی نامحدود):"},
	"spu":  {"سرعت آپلود", "سقف سرعت آپلود به Mbps (۰ یعنی نامحدود):"},
	"mul":  {"ضریب مصرف", "ضریب مصرف را بفرستید (۱ یعنی عادی، مثلا ۱.۵):"},
	"dly":  {"سقف روزانه", "سقف مصرف روزانه به گیگابایت (۰ یعنی ندارد):"},
	"win":  {"بازه ساعت مجاز", "بازه را به شکل 08:00-23:00 بفرستید (- برای همیشه مجاز):"},
}

func userEditMenu(r *req, a []string) {
	id := argInt(a, 0)
	u, err := r.getUser(id)
	if err != nil {
		r.fail(err, "ul:1")
		return
	}
	f := func(k string) Button { return btn("✏️ "+userFields[k].label, "uef", id, k) }
	text := header("✏️", "ویرایش "+u.Username) + "کدام مورد تغییر کند؟\n\n"
	text += "🔁 ریست دوره ای: <b>" + resetLabel[u.ResetStrategy] + "</b>\n"
	text += onOff(u.RequireHWID) + " الزام شناسه دستگاه (HWID)\n" + onOff(u.BlockTorrent) + " مسدود کردن تورنت\n"
	days := "همه روز ها"
	if u.WindowDays != 0 {
		var ds []string
		for i, d := range weekdays {
			if u.WindowDays&(1<<i) != 0 {
				ds = append(ds, d)
			}
		}
		days = strings.Join(ds, "، ")
	}
	text += "📅 روز های مجاز: " + days + "\n"
	r.show(view{text: text, kb: Keyboard{
		{f("name"), f("lim")},
		{f("exp"), f("hold")},
		{f("ipl"), f("dev")},
		{f("spd"), f("spu")},
		{f("dly"), f("mul")},
		{f("win"), btn("📅 روز های مجاز", "uef", id, "days")},
		{btn("📥 ورودی ها", "uef", id, "inb"), btn("🏷 گروه", "uef", id, "grp")},
		{btn("🔁 ریست دوره ای", "uef", id, "rst"), f("note")},
		{btn(onOff(u.RequireHWID)+" HWID", "uht", id), btn(onOff(u.BlockTorrent)+" تورنت", "utt", id)},
		{btn("🔑 تعویض لینک اشتراک", "urv", id)},
		nav(cbd("uv", id)),
	}})
}

func userEditField(r *req, a []string) {
	id, k := argInt(a, 0), argStr(a, 1)
	back := cbd("ue", id)
	switch k {
	case "rst":
		r.show(view{text: header("🔁", "ریست دوره ای مصرف"), kb: Keyboard{
			{btn("ندارد", "urs", id, "no_reset"), btn("روزانه", "urs", id, "day")},
			{btn("هفتگی", "urs", id, "week"), btn("ماهانه", "urs", id, "month")},
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
		bs := []Button{btn("بدون گروه", "ugp", id, 0)}
		for _, g := range gs {
			bs = append(bs, btn("🏷 "+cut(g.Name, 20), "ugp", id, g.ID))
		}
		kb := chunk(bs, 2)
		r.show(view{text: header("🏷", "گروه کاربر") + "گروه را انتخاب کنید:", kb: append(kb, nav(back))})
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
			again("یک عدد درست بفرستید.")
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
			again("یک عدد صحیح بفرستید.")
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
			again("یک عدد صحیح از ۱ به بالا بفرستید.")
			return
		}
		body["on_hold_days"], body["expire_at"] = n, nil
	case "ipl", "dev":
		n, ok := parseInt(text)
		if !ok || n < 0 {
			again("یک عدد صحیح بفرستید.")
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
				again("شکل درست: 08:00-23:00")
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
	text := header("📅", "روز های مجاز "+u.Username) + "روز هایی که کاربر می تواند وصل شود. اگر هیچ روزی انتخاب نشود، همه روز ها مجاز است."
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
		r.toast("✅ ذخیره شد")
	}
	kb := Keyboard{}
	for _, ib := range list {
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s:%s", onOff(have[ib.ID]), cut(ib.Remark, 20), ib.Protocol, fa(strconv.Itoa(ib.Port))), "uib", id, ib.ID)})
	}
	r.show(view{text: header("📥", "ورودی های "+u.Username) + "با زدن هر ورودی، آن را برای کاربر روشن یا خاموش کنید.", kb: append(kb, nav(cbd("ue", id)))})
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
	text := header("📡", "اتصال های "+u.Username)
	kb := Keyboard{}
	text += "<b>IP ها</b> (" + faInt(len(c.IPs)) + ")\n"
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
		act := "⛔️ مسدود"
		if ip.Blocked {
			act = "✅ آزاد"
		}
		kb = append(kb, []Button{btn(act+" "+ip.IP, "uci", id, ip.ID, "b"), btn("🗑", "uci", id, ip.ID, "d")})
	}
	text += "\n<b>دستگاه ها</b> (" + faInt(len(c.Devices)) + ")\n"
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
		act := "⛔️ مسدود"
		if d.Blocked {
			act = "✅ آزاد"
		}
		kb = append(kb, []Button{btn(act+" "+cut(name, 18), "ucd", id, d.ID, "b"), btn("🗑", "ucd", id, d.ID, "d")})
	}
	kb = append(kb, []Button{btn("🔄 تازه سازی", "uc", id)}, nav(cbd("uv", id)))
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
	r.toast("✅ انجام شد")
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
	r.toast("✅ انجام شد")
	userConns(r, a)
}

// --- create ---------------------------------------------------------------

func userNew(r *req, _ []string) {
	r.st.draft = map[string]string{}
	r.ask("unname", nil, "کاربر جدید (۱ از ۴)", "نام کاربری را بفرستید (۳ تا ۳۲ حرف انگلیسی، رقم، _ . @ -):", "ul:1")
}

func userNewName(r *req, _ []string, text string, _ *Document) {
	r.st.draft["name"] = strings.TrimSpace(text)
	r.ask("ungb", nil, "کاربر جدید (۲ از ۴)", "نام: "+code(text)+"\n\nحجم به گیگابایت را بفرستید:", "ul:1",
		[]Button{btn("♾ حجم نامحدود", "unl")})
}

func userNewGB(r *req, _ []string, text string, _ *Document) {
	n, ok := parseNum(text)
	if !ok || n < 0 {
		r.ask("ungb", nil, "کاربر جدید (۲ از ۴)", "⚠️ یک عدد درست بفرستید. حجم به گیگابایت:", "ul:1", []Button{btn("♾ حجم نامحدود", "unl")})
		return
	}
	r.st.draft["gb"] = strconv.FormatFloat(n, 'f', -1, 64)
	userNewDaysAsk(r)
}

func userNewDaysAsk(r *req) {
	r.st.draft["hold"] = ""
	r.ask("undays", nil, "کاربر جدید (۳ از ۴)", "چند روز اعتبار داشته باشد؟", "ul:1",
		[]Button{btn("۳۰ روز", "und", 30, ""), btn("۶۰ روز", "und", 60, ""), btn("۹۰ روز", "und", 90, "")},
		[]Button{btn("♾ زمان نامحدود", "und", 0, ""), btn("🔵 از اولین اتصال", "unh")})
}

func userNewDays(r *req, _ []string, text string, _ *Document) {
	n, ok := parseInt(text)
	if !ok || n < 0 {
		r.ask("undays", nil, "کاربر جدید (۳ از ۴)", "⚠️ یک عدد صحیح بفرستید. چند روز؟", "ul:1")
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
	text := header("➕", "کاربر جدید (۴ از ۴)") + "نام: " + code(d["name"]) + "\n"
	if g, _ := parseNum(d["gb"]); g > 0 {
		text += "حجم: " + fa(d["gb"]) + " GB\n"
	} else {
		text += "حجم: نامحدود\n"
	}
	switch {
	case d["hold"] == "1":
		text += "زمان: " + fa(d["days"]) + " روز از اولین اتصال\n"
	case d["days"] == "0":
		text += "زمان: نامحدود\n"
	default:
		text += "زمان: " + fa(d["days"]) + " روز\n"
	}
	text += "\nورودی های کاربر را انتخاب کنید و بعد «ساختن» را بزنید:"
	kb := Keyboard{}
	for _, ib := range list {
		k := strconv.FormatInt(ib.ID, 10)
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s", onOff(sel[k]), cut(ib.Remark, 22), ib.Protocol), "unib", ib.ID)})
	}
	kb = append(kb, []Button{btn("✅ ساختن کاربر", "unok")}, []Button{btn("✖️ انصراف", "ul", 1)})
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
	r.toast("✅ کاربر ساخته شد")
	userView(r, []string{strconv.FormatInt(u.ID, 10)})
}
