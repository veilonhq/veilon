package tgbot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/xray"
)

// The Xray section edits a draft of the panel's Xray template (outbounds and
// routing rules). Every change is checked with the panel's own template
// validation right away, but nothing reaches Xray until the admin applies
// the draft (with a confirmation); the panel then tests the config with Xray
// and rolls back on its own if Xray does not stay up.

const (
	xrPerPage   = 8
	xrMaxUpload = 2 << 20
)

// xrState is the chat's Xray draft.
type xrState struct {
	t *xray.Template
	// dirty counts the changes not applied yet.
	dirty int
	// inbounds are the inbound tags rules may use (from the panel).
	inbounds []xrInbound
}

type xrInbound struct {
	ID     int64  `json:"id"`
	Tag    string `json:"tag"`
	Remark string `json:"remark"`
}

// rule condition kinds offered by the rule form.
var xrConds = []struct{ key, label, hint string }{
	{"domain", "🌍 دامنه", "دامنه ها را بفرستید، هر کدام در یک خط یا با کاما جدا. نمونه: <code>geosite:category-ads-all</code> یا <code>domain:example.com</code>"},
	{"ip", "🔢 IP", "IP ها یا رنج ها را بفرستید. نمونه: <code>geoip:ir</code> یا <code>10.0.0.0/8</code>"},
	{"port", "🚪 پورت", "پورت یا بازه پورت را بفرستید. نمونه: <code>53,443</code> یا <code>1000-2000</code>"},
	{"network", "📶 شبکه", ""},
	{"protocol", "🧩 پروتکل", ""},
	{"inboundTag", "📥 ورودی", ""},
	{"user", "👤 کاربر", "نام کاربری ها را بفرستید، هر کدام در یک خط یا با کاما جدا."},
}

var xrNetworks = []string{"tcp", "udp", "tcp,udp"}
var xrProtocols = []string{"http", "tls", "quic", "bittorrent"}

func xrCondLabel(k string) string {
	for _, c := range xrConds {
		if c.key == k {
			return c.label
		}
	}
	return k
}

func init() {
	on("xr", xrHome)
	on("xrr", func(r *req, _ []string) { r.st.xr = nil; xrHome(r, nil) })
	on("xo", xrOutList)
	on("xov", xrOutView)
	on("xot", xrOutToggle)
	on("xod", xrOutDeleteAsk)
	on("xody", xrOutDelete)
	on("xoe", func(r *req, a []string) {
		i := int(argInt(a, 0))
		o := r.xrOut(i)
		if o == nil {
			xrOutList(r, nil)
			return
		}
		r.ask("xoejson", []string{strconv.Itoa(i)}, "ویرایش خروجی "+str(o, "tag"),
			"متن JSON کامل این خروجی را بفرستید (به صورت متن یا فایل). متن فعلی:\n<pre>"+esc(cut(prettyJSON(o), 2500))+"</pre>", cbd("xov", i))
	})
	onInput("xoejson", func(r *req, a []string, text string, doc *Document) {
		i := int(argInt(a, 0))
		m, ok := r.xrReadObject(text, doc, cbd("xov", i))
		if !ok {
			return
		}
		r.xrChange(cbd("xov", i), func(t *xray.Template) error {
			if i < 0 || i >= len(t.Outbounds) {
				return fmt.Errorf("این خروجی دیگر وجود ندارد")
			}
			old := str(t.Outbounds[i], "tag")
			t.Outbounds[i] = m
			if nt := str(m, "tag"); nt != old {
				xrRenameRefs(t, old, nt)
			}
			return nil
		}, func() { xrOutView(r, []string{strconv.Itoa(i)}) })
	})
	on("xon", func(r *req, _ []string) {
		r.ask("xonlink", nil, "خروجی جدید",
			"لینک اشتراک (vless, vmess, trojan, ss, socks, http, wireguard) را بفرستید.\nیا متن JSON یک خروجی Xray را به صورت متن یا فایل بفرستید.", "xo")
	})
	onInput("xonlink", func(r *req, _ []string, text string, doc *Document) {
		var m map[string]any
		if doc == nil && strings.Contains(text, "://") && !strings.HasPrefix(strings.TrimSpace(text), "{") {
			var res struct {
				Outbound map[string]any `json:"outbound"`
			}
			if err := r.call("POST", "/xray/outbounds/parse", map[string]any{"link": text}, &res); err != nil {
				r.fail(err, "xo")
				return
			}
			m = res.Outbound
		} else {
			var ok bool
			if m, ok = r.xrReadObject(text, doc, "xo"); !ok {
				return
			}
		}
		var idx int
		r.xrChange("xo", func(t *xray.Template) error {
			tag := str(m, "tag")
			if tag == "" {
				tag = str(m, "protocol") + "-out"
			}
			m["tag"] = xrFreeTag(t, tag)
			// New outbounds go before the reserved ones, so "direct" stays
			// the default route.
			idx = len(t.Outbounds)
			for i, o := range t.Outbounds {
				if tg := str(o, "tag"); tg == xray.TagDirect || tg == xray.TagBlocked {
					idx = i
					break
				}
			}
			t.Outbounds = append(t.Outbounds[:idx], append([]map[string]any{m}, t.Outbounds[idx:]...)...)
			return nil
		}, func() {
			r.toast("➕ اضافه شد")
			xrOutView(r, []string{strconv.Itoa(idx)})
		})
	})

	on("xu", xrRuleList)
	on("xuv", xrRuleView)
	on("xut", xrRuleToggle)
	on("xum", xrRuleMove)
	on("xud", xrRuleDeleteAsk)
	on("xudy", xrRuleDelete)
	on("xug", func(r *req, a []string) {
		i := int(argInt(a, 0))
		if r.xrRule(i) == nil {
			xrRuleList(r, nil)
			return
		}
		xrPickTarget(r, "xugs", []any{i}, "مقصد قانون "+faInt(i+1), cbd("xuv", i))
	})
	on("xugs", func(r *req, a []string) {
		i, k := int(argInt(a, 0)), int(argInt(a, 1))
		r.xrChange(cbd("xuv", i), func(t *xray.Template) error {
			if i < 0 || i >= len(t.Routing.Rules) {
				return fmt.Errorf("این قانون دیگر وجود ندارد")
			}
			return xrSetTarget(t, t.Routing.Rules[i], k)
		}, func() { xrRuleView(r, []string{strconv.Itoa(i)}) })
	})
	on("xun", func(r *req, a []string) {
		i := int(argInt(a, 0))
		if r.xrRule(i) == nil {
			xrRuleList(r, nil)
			return
		}
		r.ask("xuname", []string{strconv.Itoa(i)}, "نام قانون "+faInt(i+1), "یک نام کوتاه برای این قانون بفرستید (حداکثر ۴۰ حرف):", cbd("xuv", i),
			[]Button{btn("🧹 بدون نام", "xunc", i)})
	})
	on("xunc", func(r *req, a []string) { xrRuleSetName(r, int(argInt(a, 0)), "") })
	onInput("xuname", func(r *req, a []string, text string, _ *Document) {
		xrRuleSetName(r, int(argInt(a, 0)), cut(text, 40))
	})
	on("xuj", func(r *req, a []string) {
		i := int(argInt(a, 0))
		ru := r.xrRule(i)
		if ru == nil {
			xrRuleList(r, nil)
			return
		}
		r.ask("xujson", []string{strconv.Itoa(i)}, "ویرایش قانون "+faInt(i+1),
			"متن JSON کامل این قانون را بفرستید (به صورت متن یا فایل). متن فعلی:\n<pre>"+esc(cut(prettyJSON(ru), 2500))+"</pre>", cbd("xuv", i))
	})
	onInput("xujson", func(r *req, a []string, text string, doc *Document) {
		i := int(argInt(a, 0))
		m, ok := r.xrReadObject(text, doc, cbd("xuv", i))
		if !ok {
			return
		}
		r.xrChange(cbd("xuv", i), func(t *xray.Template) error {
			if i < 0 || i >= len(t.Routing.Rules) {
				return fmt.Errorf("این قانون دیگر وجود ندارد")
			}
			t.Routing.Rules[i] = m
			return nil
		}, func() { xrRuleView(r, []string{strconv.Itoa(i)}) })
	})
	// new rule: condition kind -> values -> target
	on("xnr", func(r *req, _ []string) {
		if r.xrLoad() == nil {
			return
		}
		r.st.draft = map[string]string{}
		var bs []Button
		for i, c := range xrConds {
			bs = append(bs, btn(c.label, "xnc", i))
		}
		kb := chunk(bs, 2)
		kb = append(kb, nav("xu"))
		r.show(view{text: header("➕", "قانون جدید") + "این قانون برای چه ترافیکی است؟ نوع شرط را انتخاب کنید:", kb: kb})
	})
	on("xnc", func(r *req, a []string) {
		ci := int(argInt(a, 0))
		if ci < 0 || ci >= len(xrConds) {
			xrRuleList(r, nil)
			return
		}
		c := xrConds[ci]
		r.st.draft = map[string]string{"cond": c.key}
		title := "قانون جدید · " + c.label
		var opts []string
		switch c.key {
		case "network":
			opts = xrNetworks
		case "protocol":
			opts = xrProtocols
		case "inboundTag":
			st := r.xrLoad()
			if st == nil {
				return
			}
			var bs []Button
			for i, ib := range st.inbounds {
				bs = append(bs, btn(cut(ib.Remark, 20)+" · "+ib.Tag, "xnv", i))
			}
			if len(bs) == 0 {
				r.show(view{text: header("➕", title) + "هنوز ورودی ای ساخته نشده است.", kb: Keyboard{nav("xnr")}})
				return
			}
			kb := chunk(bs, 1)
			kb = append(kb, nav("xnr"))
			r.show(view{text: header("➕", title) + "کدام ورودی؟", kb: kb})
			return
		}
		if opts != nil {
			var bs []Button
			for i, o := range opts {
				bs = append(bs, btn(o, "xnv", i))
			}
			kb := chunk(bs, 2)
			kb = append(kb, nav("xnr"))
			r.show(view{text: header("➕", title) + "یکی را انتخاب کنید:", kb: kb})
			return
		}
		r.ask("xnvals", nil, title, c.hint, "xnr")
	})
	on("xnv", func(r *req, a []string) {
		k := int(argInt(a, 0))
		var v string
		switch r.st.draft["cond"] {
		case "network":
			if k >= 0 && k < len(xrNetworks) {
				v = xrNetworks[k]
			}
		case "protocol":
			if k >= 0 && k < len(xrProtocols) {
				v = xrProtocols[k]
			}
		case "inboundTag":
			if st := r.xrLoad(); st != nil && k >= 0 && k < len(st.inbounds) {
				v = st.inbounds[k].Tag
			}
		}
		if v == "" {
			xrRuleList(r, nil)
			return
		}
		r.st.draft["vals"] = v
		xrPickTarget(r, "xnt", nil, "قانون جدید · مقصد", "xnr")
	})
	onInput("xnvals", func(r *req, _ []string, text string, _ *Document) {
		if splitList(text) == nil {
			r.ask("xnvals", nil, "قانون جدید", "⚠️ چیزی دریافت نشد. دوباره بفرستید:", "xnr")
			return
		}
		r.st.draft["vals"] = text
		xrPickTarget(r, "xnt", nil, "قانون جدید · مقصد", "xnr")
	})
	on("xnt", func(r *req, a []string) {
		k := int(argInt(a, 0))
		cond, vals := r.st.draft["cond"], r.st.draft["vals"]
		if cond == "" || vals == "" {
			xrRuleList(r, nil)
			return
		}
		var idx int
		r.xrChange("xu", func(t *xray.Template) error {
			ru := map[string]any{"type": "field"}
			switch cond {
			case "network":
				ru[cond] = vals
			case "port":
				ru[cond] = strings.Join(splitList(vals), ",")
			default:
				ru[cond] = splitList(vals)
			}
			if err := xrSetTarget(t, ru, k); err != nil {
				return err
			}
			t.Routing.Rules = append(t.Routing.Rules, ru)
			idx = len(t.Routing.Rules) - 1
			return nil
		}, func() {
			r.st.draft = map[string]string{}
			r.toast("➕ اضافه شد")
			xrRuleView(r, []string{strconv.Itoa(idx)})
		})
	})

	on("xj", xrJSON)
	onInput("xjfile", func(r *req, _ []string, text string, doc *Document) {
		raw, ok := r.xrReadRaw(text, doc, "xj")
		if !ok {
			return
		}
		t, err := xray.ParseTemplate(raw)
		if err != nil {
			r.show(view{text: header("⚠️", "این JSON پذیرفته نشد") + esc(err.Error()) + "\n\nچیزی تغییر نکرد.", kb: Keyboard{{btn("📄 دوباره", "xj")}, nav("xr")}})
			return
		}
		st := r.xrLoad()
		if st == nil {
			return
		}
		r.progress("در حال بررسی با Xray")
		var res xray.TemplateResult
		if err := r.call("POST", "/xray/template/test", map[string]any{"template": t}, &res); err != nil {
			r.fail(err, "xj")
			return
		}
		if !res.OK {
			r.show(view{text: header("⚠️", "Xray این تنظیمات را نپذیرفت") + "<pre>" + esc(cut(res.Error, 2500)) + "</pre>\nچیزی تغییر نکرد.", kb: Keyboard{{btn("📄 دوباره", "xj")}, nav("xr")}})
			return
		}
		st.t = t
		st.dirty++
		text2 := header("✅", "JSON بررسی شد") + "تنظیمات تازه در پیش نویس قرار گرفت. برای اثر روی سرور، «اعمال تغییرات» را بزنید."
		if !res.Tested {
			text2 += "\nℹ️ Xray روی این سرور نصب نیست، فقط ساختار بررسی شد."
		}
		text2 += xrWarnings(res.Warnings)
		r.show(view{text: text2, kb: Keyboard{{btn("✅ اعمال تغییرات", "xa")}, nav("xr")}})
	})
	on("xa", func(r *req, _ []string) {
		st := r.xrLoad()
		if st == nil {
			return
		}
		confirm(r, "اعمال تنظیمات Xray",
			"تنظیمات Xray با "+faInt(st.dirty)+" تغییر اعمال شود؟\n\nپنل اول تنظیمات را با Xray آزمایش می کند؛ بعد Xray دوباره راه اندازی می شود و اتصال ها یک لحظه قطع می شوند. اگر Xray بالا نیاید، تنظیمات قبلی خودکار برمی گردد.",
			"xay", "xr")
	})
	on("xay", xrApply)
	on("xx", func(r *req, _ []string) {
		confirm(r, "دور انداختن تغییرات", "همه تغییرات اعمال نشده دور ریخته شود؟", "xxy", "xr")
	})
	on("xxy", func(r *req, _ []string) {
		r.st.xr = nil
		r.toast("↩️ دور ریخته شد")
		xrHome(r, nil)
	})
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func prettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// splitList reads values separated by commas, Persian commas or lines.
func splitList(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '،' || r == '\n' || r == ' ' || r == '\t' })
	if len(f) == 0 {
		return nil
	}
	return f
}

func xrDisabled(m map[string]any) bool { b, _ := m["_disabled"].(bool); return b }

// xrLoad returns the chat's draft, loading the panel's template first.
func (r *req) xrLoad() *xrState {
	if r.st.xr != nil {
		return r.st.xr
	}
	var res struct {
		Template json.RawMessage `json:"template"`
		Inbounds []xrInbound     `json:"inbounds"`
	}
	if err := r.call("GET", "/xray/template", nil, &res); err != nil {
		r.fail(err, "home")
		return nil
	}
	t, err := xray.ParseTemplate(res.Template)
	if err != nil {
		r.fail(err, "home")
		return nil
	}
	r.st.xr = &xrState{t: t, inbounds: res.Inbounds}
	return r.st.xr
}

func (r *req) xrOut(i int) map[string]any {
	st := r.xrLoad()
	if st == nil || i < 0 || i >= len(st.t.Outbounds) {
		return nil
	}
	return st.t.Outbounds[i]
}

func (r *req) xrRule(i int) map[string]any {
	st := r.xrLoad()
	if st == nil || i < 0 || i >= len(st.t.Routing.Rules) {
		return nil
	}
	return st.t.Routing.Rules[i]
}

// xrChange applies f to a copy of the draft and keeps it only when the
// result passes the panel's template validation.
func (r *req) xrChange(back string, f func(t *xray.Template) error, then func()) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	b, _ := json.Marshal(st.t)
	cp := &xray.Template{}
	_ = json.Unmarshal(b, cp)
	if err := f(cp); err != nil {
		r.show(view{text: header("⚠️", "انجام نشد") + esc(err.Error()), kb: Keyboard{nav(back)}})
		return
	}
	b, _ = json.Marshal(cp)
	t, err := xray.ParseTemplate(b)
	if err != nil {
		r.show(view{text: header("⚠️", "این تغییر پذیرفته نشد") + esc(err.Error()) + "\n\nپیش نویس تغییری نکرد.", kb: Keyboard{nav(back)}})
		return
	}
	st.t = t
	st.dirty++
	then()
}

// xrReadRaw reads JSON sent as text or as a file.
func (r *req) xrReadRaw(text string, doc *Document, back string) ([]byte, bool) {
	if doc != nil {
		b, err := r.cl.Download(r.ctx, doc.FileID, xrMaxUpload)
		if err != nil {
			r.fail(err, back)
			return nil, false
		}
		return bytes.TrimSpace(b), true
	}
	return []byte(strings.TrimSpace(text)), true
}

// xrReadObject reads one JSON object sent as text or as a file.
func (r *req) xrReadObject(text string, doc *Document, back string) (map[string]any, bool) {
	raw, ok := r.xrReadRaw(text, doc, back)
	if !ok {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		msg := "یک شیء JSON معتبر نبود"
		if err != nil {
			msg += ": " + err.Error()
		}
		r.show(view{text: header("⚠️", "پذیرفته نشد") + esc(msg), kb: Keyboard{nav(back)}})
		return nil, false
	}
	return m, true
}

// xrFreeTag returns tag, or tag-2, tag-3... when it is already used.
func xrFreeTag(t *xray.Template, tag string) string {
	used := map[string]bool{xray.TagAPI: true}
	for _, o := range t.Outbounds {
		used[str(o, "tag")] = true
	}
	for _, b := range t.Routing.Balancers {
		used[str(b, "tag")] = true
	}
	if !used[tag] {
		return tag
	}
	for i := 2; ; i++ {
		c := tag + "-" + strconv.Itoa(i)
		if !used[c] {
			return c
		}
	}
}

// xrRenameRefs points rules and balancers at an outbound's new tag.
func xrRenameRefs(t *xray.Template, old, nt string) {
	for _, ru := range t.Routing.Rules {
		if str(ru, "outboundTag") == old {
			ru["outboundTag"] = nt
		}
	}
	for _, b := range t.Routing.Balancers {
		if str(b, "fallbackTag") == old {
			b["fallbackTag"] = nt
		}
	}
}

// xrTargets lists rule targets: outbounds first, then balancers.
func xrTargets(t *xray.Template) (tags []string, balancer []bool) {
	for _, o := range t.Outbounds {
		tags = append(tags, str(o, "tag"))
		balancer = append(balancer, false)
	}
	for _, b := range t.Routing.Balancers {
		tags = append(tags, str(b, "tag"))
		balancer = append(balancer, true)
	}
	return
}

func xrSetTarget(t *xray.Template, ru map[string]any, k int) error {
	tags, bal := xrTargets(t)
	if k < 0 || k >= len(tags) {
		return fmt.Errorf("این مقصد دیگر وجود ندارد")
	}
	delete(ru, "outboundTag")
	delete(ru, "balancerTag")
	if bal[k] {
		ru["balancerTag"] = tags[k]
	} else {
		ru["outboundTag"] = tags[k]
	}
	return nil
}

func xrPickTarget(r *req, key string, args []any, title, back string) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	tags, bal := xrTargets(st.t)
	var bs []Button
	for k, tg := range tags {
		label := "📤 " + tg
		switch {
		case bal[k]:
			label = "⚖️ " + tg
		case tg == xray.TagDirect:
			label = "🟢 direct (مستقیم)"
		case tg == xray.TagBlocked:
			label = "⛔️ blocked (مسدود)"
		}
		bs = append(bs, btn(label, key, append(append([]any{}, args...), k)...))
	}
	kb := chunk(bs, 2)
	kb = append(kb, nav(back))
	r.show(view{text: header("🎯", title) + "ترافیکی که با این قانون جور باشد به کجا برود؟", kb: kb})
}

func xrWarnings(ws []string) string {
	if len(ws) == 0 {
		return ""
	}
	t := "\n\n⚠️ هشدار ها:"
	for i, w := range ws {
		if i == 5 {
			t += "\n• ..."
			break
		}
		t += "\n• " + esc(cut(w, 200))
	}
	return t
}

func xrHome(r *req, _ []string) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	t := st.t
	var offOut, offRule int
	for _, o := range t.Outbounds {
		if xrDisabled(o) {
			offOut++
		}
	}
	for _, ru := range t.Routing.Rules {
		if xrDisabled(ru) {
			offRule++
		}
	}
	text := header("🧭", "تنظیمات Xray")
	text += "📤 خروجی ها: <b>" + faInt(len(t.Outbounds)) + "</b>"
	if offOut > 0 {
		text += " (" + faInt(offOut) + " خاموش)"
	}
	text += "\n🧭 قوانین مسیریابی: <b>" + faInt(len(t.Routing.Rules)) + "</b>"
	if offRule > 0 {
		text += " (" + faInt(offRule) + " خاموش)"
	}
	if n := len(t.Routing.Balancers); n > 0 {
		text += "\n⚖️ متعادل کننده ها: " + faInt(n)
	}
	text += "\n"
	kb := Keyboard{
		{btn("📤 خروجی ها", "xo"), btn("🧭 قوانین", "xu")},
		{btn("📄 JSON کامل", "xj")},
	}
	if st.dirty > 0 {
		text += "\n✏️ <b>" + faInt(st.dirty) + " تغییر اعمال نشده</b> · تا «اعمال تغییرات» را نزنید، روی سرور اثری ندارد."
		kb = append(kb, []Button{btn("✅ اعمال تغییرات", "xa"), btn("↩️ دور انداختن", "xx")})
	} else {
		text += "\nتغییر ها اول در پیش نویس می مانند و با «اعمال تغییرات» روی سرور می روند."
		kb = append(kb, []Button{btn("🔄 تازه کردن", "xrr")})
	}
	kb = append(kb, nav("home"))
	r.show(view{text: text, kb: kb})
}

func xrOutLabel(o map[string]any) string {
	icon := "🟢"
	if xrDisabled(o) {
		icon = "⛔️"
	}
	return icon + " " + cut(str(o, "tag"), 24) + " · " + str(o, "protocol")
}

// xrOutAddr finds the server address of an outbound (when it has one).
func xrOutAddr(o map[string]any) string {
	s, _ := o["settings"].(map[string]any)
	for _, k := range []string{"vnext", "servers", "peers"} {
		l, _ := s[k].([]any)
		if len(l) == 0 {
			continue
		}
		m, _ := l[0].(map[string]any)
		addr := str(m, "address")
		if addr == "" {
			addr = str(m, "endpoint")
		}
		if p, ok := m["port"].(float64); ok && addr != "" {
			addr += ":" + strconv.Itoa(int(p))
		}
		if addr != "" {
			return addr
		}
	}
	return str(s, "address")
}

func xrOutList(r *req, a []string) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	outs := st.t.Outbounds
	page, pages := xrPage(a, len(outs))
	text := header("📤", "خروجی ها") + "مسیر هایی که ترافیک از آن ها از سرور بیرون می رود.\n🟢 روشن · ⛔️ خاموش\n<code>direct</code> و <code>blocked</code> همیشه هستند."
	if st.dirty > 0 {
		text += "\n\n✏️ " + faInt(st.dirty) + " تغییر اعمال نشده"
	}
	kb := Keyboard{}
	from := (page - 1) * xrPerPage
	for i := from; i < len(outs) && i < from+xrPerPage; i++ {
		kb = append(kb, []Button{btn(xrOutLabel(outs[i]), "xov", i)})
	}
	if p := pager("xo", page, pages); p != nil {
		kb = append(kb, p)
	}
	kb = append(kb, []Button{btn("➕ خروجی جدید", "xon")}, nav("xr"))
	r.show(view{text: text, kb: kb})
}

func xrPage(a []string, n int) (int, int) {
	page := int(argInt(a, 0))
	pages := (n + xrPerPage - 1) / xrPerPage
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	return page, pages
}

// xrOutUsers counts the rules and balancers that use an outbound.
func xrOutUsers(t *xray.Template, tag string) (rules, bals int) {
	for _, ru := range t.Routing.Rules {
		if str(ru, "outboundTag") == tag {
			rules++
		}
	}
	for _, b := range t.Routing.Balancers {
		if str(b, "fallbackTag") == tag {
			bals++
			continue
		}
		for _, p := range xrStrings(b["selector"]) {
			if strings.HasPrefix(tag, p) {
				bals++
				break
			}
		}
	}
	return
}

func xrStrings(v any) []string {
	switch x := v.(type) {
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprint(e))
			}
		}
		return out
	case []string:
		return x
	case string:
		return []string{x}
	case float64:
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	}
	return nil
}

func xrOutView(r *req, a []string) {
	i := int(argInt(a, 0))
	o := r.xrOut(i)
	if o == nil {
		xrOutList(r, nil)
		return
	}
	st := r.st.xr
	tag := str(o, "tag")
	reserved := tag == xray.TagDirect || tag == xray.TagBlocked
	text := header("📤", tag)
	text += "پروتکل: " + code(str(o, "protocol")) + "\n"
	if addr := xrOutAddr(o); addr != "" {
		text += "سرور: " + code(addr) + "\n"
	}
	state := "🟢 روشن"
	if xrDisabled(o) {
		state = "⛔️ خاموش (در تنظیمات Xray نمی آید و قوانینش نادیده گرفته می شوند)"
	}
	text += "وضعیت: " + state + "\n"
	if nr, nb := xrOutUsers(st.t, tag); nr+nb > 0 {
		text += "استفاده: " + faInt(nr) + " قانون"
		if nb > 0 {
			text += " · " + faInt(nb) + " متعادل کننده"
		}
		text += "\n"
	}
	if reserved {
		text += "\nℹ️ این خروجی همیشگی پنل است و حذف یا خاموش نمی شود.\n"
	}
	text += "\n<pre>" + esc(cut(prettyJSON(o), 1500)) + "</pre>"
	kb := Keyboard{}
	if !reserved {
		toggle := btn("⛔️ خاموش کردن", "xot", i)
		if xrDisabled(o) {
			toggle = btn("✅ روشن کردن", "xot", i)
		}
		kb = append(kb, []Button{toggle, btn("✏️ ویرایش JSON", "xoe", i)}, []Button{btn("🗑 حذف", "xod", i)})
	} else {
		kb = append(kb, []Button{btn("✏️ ویرایش JSON", "xoe", i)})
	}
	kb = append(kb, nav("xo"))
	r.show(view{text: text, kb: kb})
}

func xrOutToggle(r *req, a []string) {
	i := int(argInt(a, 0))
	r.xrChange(cbd("xov", i), func(t *xray.Template) error {
		if i < 0 || i >= len(t.Outbounds) {
			return fmt.Errorf("این خروجی دیگر وجود ندارد")
		}
		o := t.Outbounds[i]
		if tg := str(o, "tag"); tg == xray.TagDirect || tg == xray.TagBlocked {
			return fmt.Errorf("خروجی های همیشگی خاموش نمی شوند")
		}
		if xrDisabled(o) {
			delete(o, "_disabled")
		} else {
			o["_disabled"] = true
		}
		return nil
	}, func() { xrOutView(r, a) })
}

func xrOutDeleteAsk(r *req, a []string) {
	i := int(argInt(a, 0))
	o := r.xrOut(i)
	if o == nil {
		xrOutList(r, nil)
		return
	}
	tag := str(o, "tag")
	if tag == xray.TagDirect || tag == xray.TagBlocked {
		r.alert("خروجی های همیشگی حذف نمی شوند")
		xrOutView(r, a)
		return
	}
	if nr, nb := xrOutUsers(r.st.xr.t, tag); nr+nb > 0 {
		r.show(view{text: header("⚠️", "حذف نمی شود") + "خروجی <b>" + esc(tag) + "</b> در " + faInt(nr) + " قانون و " + faInt(nb) +
			" متعادل کننده استفاده شده است. اول مقصد آن قوانین را عوض کنید یا آن ها را حذف کنید.", kb: Keyboard{{btn("🧭 قوانین", "xu")}, nav(cbd("xov", i))}})
		return
	}
	confirm(r, "حذف خروجی", "خروجی <b>"+esc(tag)+"</b> از پیش نویس حذف شود؟\nتا «اعمال تغییرات» را نزنید، روی سرور اثری ندارد.", cbd("xody", i), cbd("xov", i))
}

func xrOutDelete(r *req, a []string) {
	i := int(argInt(a, 0))
	r.xrChange("xo", func(t *xray.Template) error {
		if i < 0 || i >= len(t.Outbounds) {
			return fmt.Errorf("این خروجی دیگر وجود ندارد")
		}
		if tg := str(t.Outbounds[i], "tag"); tg == xray.TagDirect || tg == xray.TagBlocked {
			return fmt.Errorf("خروجی های همیشگی حذف نمی شوند")
		}
		t.Outbounds = append(t.Outbounds[:i], t.Outbounds[i+1:]...)
		return nil
	}, func() {
		r.toast("🗑 حذف شد")
		xrOutList(r, nil)
	})
}

// xrRuleSummary describes a rule in one line.
func xrRuleSummary(ru map[string]any) string {
	var parts []string
	keys := make([]string, 0, len(ru))
	for k := range ru {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasPrefix(k, "_") || k == "type" || k == "outboundTag" || k == "balancerTag" || k == "ruleTag" {
			continue
		}
		vs := xrStrings(ru[k])
		if len(vs) == 0 {
			continue
		}
		s := strings.Join(vs, ", ")
		parts = append(parts, xrCondLabel(k)+" "+cut(s, 28))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

func xrRuleTarget(ru map[string]any) string {
	if b := str(ru, "balancerTag"); b != "" {
		return "⚖️ " + b
	}
	return str(ru, "outboundTag")
}

func xrRuleList(r *req, a []string) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	rules := st.t.Routing.Rules
	page, pages := xrPage(a, len(rules))
	text := header("🧭", "قوانین مسیریابی") + "قوانین به ترتیب از بالا بررسی می شوند و اولین قانونی که جور باشد، مقصد را تعیین می کند. ترافیکی که با هیچ قانونی جور نباشد از خروجی اول می رود.\n"
	if len(rules) == 0 {
		text += "\nهنوز قانونی ساخته نشده است."
	}
	if st.dirty > 0 {
		text += "\n✏️ " + faInt(st.dirty) + " تغییر اعمال نشده"
	}
	kb := Keyboard{}
	from := (page - 1) * xrPerPage
	for i := from; i < len(rules) && i < from+xrPerPage; i++ {
		ru := rules[i]
		label := faInt(i+1) + ". "
		if xrDisabled(ru) {
			label += "⛔️ "
		}
		if n := str(ru, "_name"); n != "" {
			label += cut(n, 18)
		} else {
			label += cut(xrRuleSummary(ru), 26)
		}
		label += " ← " + cut(xrRuleTarget(ru), 14)
		kb = append(kb, []Button{btn(label, "xuv", i)})
	}
	if p := pager("xu", page, pages); p != nil {
		kb = append(kb, p)
	}
	kb = append(kb, []Button{btn("➕ قانون جدید", "xnr")}, nav("xr"))
	r.show(view{text: text, kb: kb})
}

func xrRuleView(r *req, a []string) {
	i := int(argInt(a, 0))
	ru := r.xrRule(i)
	if ru == nil {
		xrRuleList(r, nil)
		return
	}
	n := len(r.st.xr.t.Routing.Rules)
	title := "قانون " + faInt(i+1) + " از " + faInt(n)
	text := header("🧭", title)
	if nm := str(ru, "_name"); nm != "" {
		text += "نام: <b>" + esc(nm) + "</b>\n"
	}
	state := "🟢 روشن"
	if xrDisabled(ru) {
		state = "⛔️ خاموش"
	}
	text += "وضعیت: " + state + "\n\n<b>شرط ها:</b>\n"
	keys := make([]string, 0, len(ru))
	for k := range ru {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasPrefix(k, "_") || k == "type" || k == "outboundTag" || k == "balancerTag" {
			continue
		}
		vs := xrStrings(ru[k])
		if len(vs) == 0 {
			continue
		}
		text += "• " + xrCondLabel(k) + ": " + code(cut(strings.Join(vs, ", "), 300)) + "\n"
	}
	text += "\n🎯 مقصد: <b>" + esc(xrRuleTarget(ru)) + "</b>\n"
	toggle := btn("⛔️ خاموش کردن", "xut", i)
	if xrDisabled(ru) {
		toggle = btn("✅ روشن کردن", "xut", i)
	}
	kb := Keyboard{{toggle, btn("🎯 تغییر مقصد", "xug", i)}}
	var move []Button
	if i > 0 {
		move = append(move, btn("⬆️ بالاتر", "xum", i, "u"))
	}
	if i < n-1 {
		move = append(move, btn("⬇️ پایین تر", "xum", i, "d"))
	}
	if move != nil {
		kb = append(kb, move)
	}
	kb = append(kb, []Button{btn("🏷 نام", "xun", i), btn("✏️ ویرایش JSON", "xuj", i)}, []Button{btn("🗑 حذف", "xud", i)}, nav(cbd("xu", i/xrPerPage+1)))
	r.show(view{text: text, kb: kb})
}

func xrRuleToggle(r *req, a []string) {
	i := int(argInt(a, 0))
	r.xrChange(cbd("xuv", i), func(t *xray.Template) error {
		if i < 0 || i >= len(t.Routing.Rules) {
			return fmt.Errorf("این قانون دیگر وجود ندارد")
		}
		ru := t.Routing.Rules[i]
		if xrDisabled(ru) {
			delete(ru, "_disabled")
		} else {
			ru["_disabled"] = true
		}
		return nil
	}, func() { xrRuleView(r, a) })
}

func xrRuleMove(r *req, a []string) {
	i := int(argInt(a, 0))
	j := i - 1
	if argStr(a, 1) == "d" {
		j = i + 1
	}
	r.xrChange(cbd("xuv", i), func(t *xray.Template) error {
		rs := t.Routing.Rules
		if i < 0 || i >= len(rs) || j < 0 || j >= len(rs) {
			return fmt.Errorf("جابجایی ممکن نیست")
		}
		rs[i], rs[j] = rs[j], rs[i]
		return nil
	}, func() { xrRuleView(r, []string{strconv.Itoa(j)}) })
}

func xrRuleSetName(r *req, i int, name string) {
	r.xrChange(cbd("xuv", i), func(t *xray.Template) error {
		if i < 0 || i >= len(t.Routing.Rules) {
			return fmt.Errorf("این قانون دیگر وجود ندارد")
		}
		if name == "" {
			delete(t.Routing.Rules[i], "_name")
		} else {
			t.Routing.Rules[i]["_name"] = name
		}
		return nil
	}, func() { xrRuleView(r, []string{strconv.Itoa(i)}) })
}

func xrRuleDeleteAsk(r *req, a []string) {
	i := int(argInt(a, 0))
	ru := r.xrRule(i)
	if ru == nil {
		xrRuleList(r, nil)
		return
	}
	confirm(r, "حذف قانون", "قانون "+faInt(i+1)+" ("+esc(cut(xrRuleSummary(ru), 60))+" ← "+esc(xrRuleTarget(ru))+") از پیش نویس حذف شود؟\nتا «اعمال تغییرات» را نزنید، روی سرور اثری ندارد.",
		cbd("xudy", i), cbd("xuv", i))
}

func xrRuleDelete(r *req, a []string) {
	i := int(argInt(a, 0))
	r.xrChange("xu", func(t *xray.Template) error {
		if i < 0 || i >= len(t.Routing.Rules) {
			return fmt.Errorf("این قانون دیگر وجود ندارد")
		}
		t.Routing.Rules = append(t.Routing.Rules[:i], t.Routing.Rules[i+1:]...)
		return nil
	}, func() {
		r.toast("🗑 حذف شد")
		xrRuleList(r, nil)
	})
}

// xrJSON sends the whole draft as a file and waits for an edited one.
func xrJSON(r *req, _ []string) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	b, _ := json.MarshalIndent(st.t, "", "  ")
	caption := "📄 تنظیمات Xray (پیش نویس فعلی)"
	if err := r.cl.SendDocument(r.ctx, r.chat, "veilon-xray.json", append(b, '\n'), caption); err != nil {
		r.fail(err, "xr")
		return
	}
	r.st.pending = &pending{kind: "xjfile", back: "xr"}
	r.fresh(view{text: header("📄", "JSON کامل Xray") +
		"فایل بالا تنظیمات فعلی است (خروجی ها، قوانین، متعادل کننده ها، DNS و گزارش).\n\nبرای جایگزینی، فایل ویرایش شده (یا متن JSON) را بفرستید. پنل اول ساختار و بعد خود Xray آن را بررسی می کند؛ تا «اعمال تغییرات» را نزنید روی سرور اثری ندارد.",
		kb: Keyboard{{btn("✖️ انصراف", "xr")}}})
}

func xrApply(r *req, _ []string) {
	st := r.xrLoad()
	if st == nil {
		return
	}
	r.progress("در حال آزمایش و اعمال تنظیمات Xray")
	var res struct {
		Result xray.TemplateResult `json:"result"`
	}
	if err := r.call("PUT", "/xray/template", map[string]any{"template": st.t}, &res); err != nil {
		r.fail(err, "xr")
		return
	}
	x := res.Result
	var text string
	switch {
	case x.Applied && !x.RolledBack:
		r.st.xr = nil
		text = header("✅", "تنظیمات Xray اعمال شد") + "Xray با تنظیمات تازه کار می کند."
		if !x.Tested {
			text += "\nℹ️ Xray روی این سرور نصب نیست؛ تنظیمات ذخیره شد."
		}
	case x.RolledBack:
		text = header("⚠️", "برگردانده شد") + "Xray با تنظیمات تازه بالا نیامد و تنظیمات قبلی برگردانده شد. پیش نویس شما سر جایش است."
		if x.Error != "" {
			text += "\n<pre>" + esc(cut(x.Error, 2000)) + "</pre>"
		}
	default:
		text = header("⚠️", "اعمال نشد") + "Xray این تنظیمات را نپذیرفت و چیزی روی سرور تغییر نکرد. پیش نویس شما سر جایش است."
		if x.Error != "" {
			text += "\n<pre>" + esc(cut(x.Error, 2000)) + "</pre>"
		}
	}
	text += xrWarnings(x.Warnings)
	r.show(view{text: text, kb: Keyboard{nav("xr")}})
}
