package tgbot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
)

// Panel settings: the same fields as the web Settings page, each changed
// with one PATCH /settings. The bot token is not here: it is changed on the
// web page only.

type psField struct {
	key, label, hint string
	// num: a whole number between min and max.
	num      bool
	min, max int64
	// clear: may be emptied with a button.
	clear bool
	// risky: changes the panel's own address; asks for a confirmation.
	risky bool
}

var psFields = []psField{
	{key: "panel_port", label: T("🚪 پورت پنل"), hint: T("پورت تازه پنل را بفرستید (۱ تا ۶۵۵۳۵)."), num: true, min: 1, max: 65535, risky: true},
	{key: "panel_path", label: T("🔗 مسیر پنل"), hint: T("مسیر مخفی تازه پنل را بفرستید (۲ تا ۶۴ حرف انگلیسی، رقم، _ یا -)."), risky: true},
	{key: "panel_domain", label: T("🌐 دامنه پنل"), hint: T("دامنه پنل را بفرستید (بدون http). نمونه: <code>panel.example.com</code>"), clear: true},
	{key: "sub_path", label: T("🔗 مسیر اشتراک"), hint: T("مسیر لینک اشتراک را بفرستید (۲ تا ۶۴ حرف انگلیسی، رقم، _ یا -). لینک های قبلی کاربران با این تغییر کار نمی کنند."), risky: true},
	{key: "sub_domain", label: T("🌐 دامنه اشتراک"), hint: T("دامنه لینک های اشتراک را بفرستید (بدون http)."), clear: true},
	{key: "sub_title", label: T("🏷 عنوان اشتراک"), hint: T("عنوانی که برنامه های کاربران برای اشتراک نشان می دهند را بفرستید."), clear: true},
	{key: "sub_update_hours", label: T("🔄 بازه بروزرسانی اشتراک"), hint: T("هر چند ساعت برنامه های کاربران اشتراک را تازه کنند؟ (۱ تا ۱۶۸)"), num: true, min: 1, max: 168},
	{key: "server_address", label: T("🖥 آدرس سرور در لینک ها"), hint: T("آدرس IP یا دامنه ای که در کانفیگ های کاربران می آید را بفرستید."), clear: true},
}

var psZones = []string{"Asia/Tehran", "UTC", "Europe/Berlin", "Europe/Istanbul", "Asia/Dubai"}

func init() {
	on("ps", settingsView)
	on("pse", func(r *req, a []string) {
		i := int(argInt(a, 0))
		if i < 0 || i >= len(psFields) {
			settingsView(r, nil)
			return
		}
		f := psFields[i]
		var extra [][]Button
		if f.clear {
			extra = append(extra, []Button{btn(T("🧹 خالی کردن"), "psx", i)})
		}
		cur := r.settingsRaw()[f.key]
		prompt := f.hint
		if cur != nil {
			prompt += T("\n\nمقدار فعلی: ") + code(fmt.Sprint(cur))
		}
		r.ask("psval", []string{strconv.Itoa(i)}, f.label, prompt, "ps", extra...)
	})
	on("psx", func(r *req, a []string) { settingsSet(r, int(argInt(a, 0)), "") })
	onInput("psval", func(r *req, a []string, text string, _ *Document) { settingsSet(r, int(argInt(a, 0)), text) })
	on("psy", func(r *req, _ []string) {
		key, raw := r.st.draft["pskey"], r.st.draft["psval"]
		if key == "" {
			settingsView(r, nil)
			return
		}
		var v any
		_ = json.Unmarshal([]byte(raw), &v)
		settingsPatch(r, map[string]any{key: v})
	})
	on("psb", func(r *req, a []string) {
		key := argStr(a, 0)
		if key != "ad_block" && key != "block_torrent" {
			settingsView(r, nil)
			return
		}
		cur, _ := r.settingsRaw()[key].(bool)
		settingsPatch(r, map[string]any{key: !cur})
	})
	on("psl", func(r *req, _ []string) {
		cur, _ := r.settingsRaw()["language"].(string)
		next := "en"
		if cur == "en" {
			next = "fa"
		}
		settingsPatch(r, map[string]any{"language": next})
	})
	on("pst", func(r *req, _ []string) {
		var bs []Button
		for i, z := range psZones {
			bs = append(bs, btn(z, "psts", i))
		}
		kb := chunk(bs, 2)
		kb = append(kb, []Button{btn(T("✍️ نوشتن منطقه دیگر"), "pstw")}, nav("ps"))
		r.show(view{text: header("🕒", T("منطقه زمانی پنل")) + T("تاریخ ها، بازه های مصرف روزانه و نمودار ها با این منطقه زمانی حساب می شوند."), kb: kb})
	})
	on("psts", func(r *req, a []string) {
		i := int(argInt(a, 0))
		if i < 0 || i >= len(psZones) {
			settingsView(r, nil)
			return
		}
		settingsPatch(r, map[string]any{"timezone": psZones[i]})
	})
	on("pstw", func(r *req, _ []string) {
		r.ask("pstz", nil, T("منطقه زمانی"), T("نام منطقه زمانی را به شکل استاندارد بفرستید. نمونه: <code>Asia/Tehran</code> یا <code>Europe/London</code>"), "pst")
	})
	onInput("pstz", func(r *req, _ []string, text string, _ *Document) {
		if _, err := time.LoadLocation(text); err != nil || text == "" {
			r.ask("pstz", nil, T("منطقه زمانی"), T("⚠️ این منطقه زمانی شناخته نشد. دوباره بفرستید. نمونه: <code>Asia/Tehran</code>"), "pst")
			return
		}
		settingsPatch(r, map[string]any{"timezone": text})
	})
	on("psc", settingsCertPick)
	on("pscs", func(r *req, a []string) {
		id := argInt(a, 0)
		var v any
		if id > 0 {
			v = id
		}
		r.st.draft = map[string]string{"pskey": "panel_cert_id"}
		b, _ := json.Marshal(v)
		r.st.draft["psval"] = string(b)
		confirm(r, T("گواهی پنل"), T("گواهی HTTPS پنل عوض شود؟\nپنل وب چند ثانیه بعد با گواهی تازه دوباره بالا می آید و صفحه باز در مرورگر باید تازه شود."), "psy", "ps")
	})
	on("pn", notifyView)
	on("pnt", notifyToggle)
}

// settingsRaw returns the panel settings as JSON values.
func (r *req) settingsRaw() map[string]any {
	m := map[string]any{}
	_ = r.call("GET", "/settings", nil, &m)
	return m
}

func settingsView(r *req, _ []string) {
	var st db.Settings
	if err := r.call("GET", "/settings", nil, &st); err != nil {
		r.fail(err, "home")
		return
	}
	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return code(s)
	}
	t := header("⚙️", T("تنظیمات پنل"))
	t += T("<b>پنل</b>\n")
	t += T("🚪 پورت: ") + code(strconv.Itoa(st.PanelPort)) + T(" · 🔗 مسیر: ") + code("/"+st.PanelPath+"/") + "\n"
	t += T("🌐 دامنه: ") + dash(st.PanelDomain) + "\n"
	cert := T("بدون گواهی جدا")
	if st.PanelCertID != nil {
		cert = T("گواهی شماره ") + faInt(*st.PanelCertID)
	}
	t += T("🔒 گواهی: ") + cert + "\n\n"
	t += T("<b>اشتراک</b>\n")
	t += T("🔗 مسیر: ") + code("/"+st.SubPath+"/") + T(" · 🌐 دامنه: ") + dash(st.SubDomain) + "\n"
	t += T("🏷 عنوان: ") + dash(st.SubTitle) + T(" · 🔄 هر ") + faInt(st.SubUpdateHours) + T(" ساعت\n")
	t += T("🖥 آدرس سرور در لینک ها: ") + dash(st.ServerAddress) + "\n\n"
	lang := T("فارسی")
	if st.Language == "en" {
		lang = "English"
	}
	t += T("<b>عمومی</b>\n🕒 منطقه زمانی: ") + code(st.Timezone) + T(" · 🗣 زبان: ") + lang + "\n"
	t += onOff(st.AdBlock) + T(" بستن تبلیغات برای همه · ") + onOff(st.BlockTorrent) + T(" بستن تورنت برای همه\n")
	t += T("\nℹ️ توکن ربات فقط از صفحه تنظیمات وب عوض می شود.")
	var bs []Button
	for i, f := range psFields {
		bs = append(bs, btn(f.label, "pse", i))
	}
	kb := chunk(bs, 2)
	kb = append(kb,
		[]Button{btn(T("🔒 گواهی پنل"), "psc"), btn(T("🕒 منطقه زمانی"), "pst")},
		[]Button{btn(T("🗣 زبان: ")+lang, "psl")},
		[]Button{btn(onOff(st.AdBlock)+T(" بستن تبلیغات"), "psb", "ad_block"), btn(onOff(st.BlockTorrent)+T(" بستن تورنت"), "psb", "block_torrent")},
		[]Button{btn(T("🔔 اعلان های ربات"), "pn")},
		nav("home"))
	r.show(view{text: t, kb: kb})
}

func settingsSet(r *req, i int, text string) {
	if i < 0 || i >= len(psFields) {
		settingsView(r, nil)
		return
	}
	f := psFields[i]
	var v any = strings.TrimSpace(text)
	if f.num {
		n, ok := parseInt(text)
		if !ok || n < f.min || n > f.max {
			r.ask("psval", []string{strconv.Itoa(i)}, f.label, Tf("⚠️ عدد باید از %s تا %s باشد.\n", faInt(f.min), faInt(f.max))+f.hint, "ps")
			return
		}
		v = n
	}
	if f.risky {
		b, _ := json.Marshal(v)
		r.st.draft = map[string]string{"pskey": f.key, "psval": string(b)}
		q := f.label + T(" به ") + code(fmt.Sprint(v)) + T(" عوض شود؟")
		switch f.key {
		case "panel_port", "panel_path":
			q += T("\n\n⚠️ آدرس پنل وب عوض می شود و صفحه باز در مرورگر باید با آدرس تازه باز شود. ربات بدون تغییر کار می کند.")
		case "sub_path":
			q += T("\n\n⚠️ لینک اشتراک همه کاربران عوض می شود.")
		}
		confirm(r, T("تغییر ")+strings.TrimSpace(strings.TrimLeft(f.label, "🚪🔗🌐")), q, "psy", "ps")
		return
	}
	settingsPatch(r, map[string]any{f.key: v})
}

func settingsPatch(r *req, body map[string]any) {
	r.st.draft = map[string]string{}
	if err := r.call("PATCH", "/settings", body, nil); err != nil {
		r.fail(err, "ps")
		return
	}
	r.toast(T("✅ ذخیره شد"))
	settingsView(r, nil)
}

func settingsCertPick(r *req, _ []string) {
	var cs []struct {
		ID       int64     `json:"id"`
		Domain   string    `json:"domain"`
		Type     string    `json:"type"`
		NotAfter time.Time `json:"not_after"`
	}
	if err := r.call("GET", "/certs", nil, &cs); err != nil {
		r.fail(err, "ps")
		return
	}
	kb := Keyboard{{btn(T("بدون گواهی جدا"), "pscs", 0)}}
	for _, c := range cs {
		kb = append(kb, []Button{btn(Tf("🔒 %s · تا %s", cut(c.Domain, 26), jday(c.NotAfter, r.b.loc())), "pscs", c.ID)})
	}
	kb = append(kb, nav("ps"))
	t := header("🔒", T("گواهی پنل")) + T("گواهی HTTPS پنل وب را انتخاب کنید.")
	if len(cs) == 0 {
		t += T("\n\nهنوز گواهی ای ساخته نشده است؛ گواهی ها از صفحه گواهی ها در پنل وب ساخته می شوند.")
	}
	r.show(view{text: t, kb: kb})
}

// --- notification switches ------------------------------------------------

var notifyKinds = []struct{ key, label, hint string }{
	{"traffic", T("📊 حجم"), T("کاربری به ۹۰٪ و ۱۰۰٪ حجمش رسید")},
	{"expiry", T("⏳ انقضا"), T("کمتر از یک روز به انقضای کاربر مانده، و منقضی شد")},
	{"nodes", T("🌐 نود ها"), T("نودی آفلاین شد و دوباره وصل شد")},
	{"update", T("⬆️ نسخه تازه"), T("نسخه تازه پنل منتشر شد")},
}

func notifyGet(n Notify, key string) bool {
	switch key {
	case "traffic":
		return n.Traffic
	case "expiry":
		return n.Expiry
	case "nodes":
		return n.Nodes
	case "update":
		return n.Update
	}
	return false
}

func notifySet(n *Notify, key string, v bool) {
	switch key {
	case "traffic":
		n.Traffic = v
	case "expiry":
		n.Expiry = v
	case "nodes":
		n.Nodes = v
	case "update":
		n.Update = v
	}
}

func notifyView(r *req, _ []string) {
	c := LoadConfig(r.b.DB)
	t := header("🔔", T("اعلان های ربات")) + T("ربات این رویداد ها را برای همه ادمین های ربات می فرستد (هر رویداد فقط یک بار، چند رویداد با هم در یک پیام):\n\n")
	var kb Keyboard
	for _, k := range notifyKinds {
		on := notifyGet(c.Notify, k.key)
		t += onOff(on) + " <b>" + k.label + "</b>: " + k.hint + "\n"
		kb = append(kb, []Button{btn(onOff(on)+" "+k.label, "pnt", k.key)})
	}
	kb = append(kb, nav("ps"))
	r.show(view{text: t, kb: kb})
}

// notifyToggle switches one notification kind. It is stored directly (not
// through PUT /telegram), so the bot keeps running without a restart.
func notifyToggle(r *req, a []string) {
	key := argStr(a, 0)
	c := LoadConfig(r.b.DB)
	cur := notifyGet(c.Notify, key)
	found := false
	for _, k := range notifyKinds {
		found = found || k.key == key
	}
	if !found {
		notifyView(r, nil)
		return
	}
	notifySet(&c.Notify, key, !cur)
	if err := SaveConfig(r.b.DB, c); err != nil {
		r.fail(err, "pn")
		return
	}
	if actor, err := r.b.actor(r.from); err == nil {
		audit.Log(r.b.DB, actor, "telegram.notify", key+"="+strconv.FormatBool(!cur), "telegram")
	}
	r.toast(T("✅ ذخیره شد"))
	notifyView(r, nil)
}
