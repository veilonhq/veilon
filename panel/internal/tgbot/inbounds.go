package tgbot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// inbound is the part of the API inbound the bot reads.
type inbound struct {
	ID       int64  `json:"id"`
	Remark   string `json:"remark"`
	Enabled  bool   `json:"enabled"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Network  string `json:"network"`
	Security string `json:"security"`
	TLS      *struct {
		SNI string `json:"sni"`
	} `json:"tls"`
	Reality *struct {
		Dest        string   `json:"dest"`
		ServerNames []string `json:"server_names"`
	} `json:"reality"`
	Flow            string         `json:"flow"`
	Sniffing        bool           `json:"sniffing"`
	Advanced        map[string]any `json:"advanced"`
	VLESSEnc        *vlessEnc      `json:"vless_encryption"`
	AddressOverride string         `json:"address_override"`
	UserCount       int64          `json:"user_count"`
	HostCount       int64          `json:"host_count"`
	NodeIDs         []int64        `json:"node_ids"`
	ServeMain       bool           `json:"serve_main"`
	OnMain          bool           `json:"on_main"`
	XrayTooOld      []struct {
		NodeID  int64      `json:"node_id"`
		Version string     `json:"version"`
		Needs   []xrayNeed `json:"needs"`
	} `json:"xray_too_old"`
	Settings struct {
		HopPorts        string `json:"hop_ports"`
		HopInterval     int    `json:"hop_interval"`
		HandshakeServer string `json:"handshake_server"`
		HandshakePort   int    `json:"handshake_port"`
	} `json:"settings"`
}

// singboxProto: sing-box serves these protocols, on the main server only.
func singboxProto(p string) bool {
	return p == "tuic" || p == "shadowtls" || p == "anytls" || p == "naive"
}

// xrayNeed is a feature that needs a minimum Xray-core version.
type xrayNeed struct {
	Feature string `json:"feature"`
	Min     string `json:"min"`
}

// needsText: "Reality ML-DSA-65 ≥ 25.7.26، TLS ECH ≥ 25.7.26" (no brackets: they flip in RTL text).
func needsText(ns []xrayNeed) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		// Bidi isolate: keeps the brackets and "+" in place in Persian (RTL) text.
		parts = append(parts, "\u2066"+esc(n.Feature)+" ≥ "+esc(n.Min)+"\u2069")
	}
	return strings.Join(parts, T("، "))
}

var protoNames = []struct{ key, label string }{
	{"vless", "VLESS"}, {"vmess", "VMess"}, {"trojan", "Trojan"}, {"shadowsocks", "Shadowsocks"},
	{"hysteria", "Hysteria2"}, {"tuic", "TUIC"}, {"wireguard", "WireGuard"}, {"amneziawg", "AmneziaWG"},
	{"mtproto", "MTProto"}, {"masque", "MASQUE"}, {"http", "HTTP"}, {"mixed", "Mixed"}, {"socks", "SOCKS"},
	{"tunnel", "Tunnel"}, {"tun", "TUN"}, {"shadowtls", "ShadowTLS"}, {"anytls", "AnyTLS"}, {"naive", "NaiveProxy"},
}

func protoLabel(p string) string {
	for _, x := range protoNames {
		if x.key == p {
			return x.label
		}
	}
	return p
}

type nodeRow struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	APIPort    int     `json:"api_port"`
	Enabled    bool    `json:"enabled"`
	Up         int64   `json:"up"`
	Down       int64   `json:"down"`
	InboundIDs []int64 `json:"inbound_ids"`
	UpdateTo   string  `json:"update_to"`
	State      struct {
		Status      string  `json:"status"`
		Error       string  `json:"error"`
		Version     string  `json:"version"`
		XrayVersion string  `json:"xray_version"`
		Synced      bool    `json:"synced"`
		Load1       float64 `json:"load1"`
		MemUsed     float64 `json:"mem_used"`
		CPUs        int     `json:"cpus"`
		LatencyMS   int64   `json:"latency_ms"`
		CanUpdate   bool    `json:"can_update"`
		Arch        string  `json:"arch"`
		Update      *struct {
			Target     string `json:"target"`
			From       string `json:"from"`
			Phase      string `json:"phase"`
			Error      string `json:"error"`
			RolledBack bool   `json:"rolled_back"`
		} `json:"update"`
		CanUpdateXray bool `json:"can_update_xray"`
		XrayUpdate    *struct {
			Target     string `json:"target"`
			From       string `json:"from"`
			Phase      string `json:"phase"`
			Error      string `json:"error"`
			RolledBack bool   `json:"rolled_back"`
		} `json:"xray_update"`
		NeedsUpdate []struct {
			ID     int64  `json:"id"`
			Remark string `json:"remark"`
		} `json:"needs_update"`
		XrayTooOld []struct {
			ID     int64      `json:"id"`
			Remark string     `json:"remark"`
			Needs  []xrayNeed `json:"needs"`
		} `json:"xray_too_old"`
		UserFWNeedsUpdate bool `json:"userfw_needs_update"`
	} `json:"state"`
}

func (r *req) nodes() []nodeRow {
	var ns []nodeRow
	_ = r.call("GET", "/nodes", nil, &ns)
	return ns
}

func init() {
	on("il", inboundsList)
	on("iv", inboundView)
	on("it", func(r *req, a []string) {
		id := argInt(a, 0)
		var ib inbound
		if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &ib); err != nil {
			r.fail(err, "il")
			return
		}
		inboundPatch(r, id, map[string]any{"enabled": !ib.Enabled})
	})
	on("ie", func(r *req, a []string) {
		id, f := argInt(a, 0), argStr(a, 1)
		p := map[string][2]string{
			"remark": {T("نام ورودی"), T("نام تازه را بفرستید:")},
			"port":   {T("پورت"), T("پورت تازه را بفرستید (۱ تا ۶۵۵۳۵):")},
			"addr":   {T("آدرس در لینک ها"), T("آدرس یا دامنه ای که در لینک ها نوشته شود را بفرستید (- برای آدرس پیش فرض سرور):")},
			"hop":    {T("پرش پورت"), T("بازه پورت های UDP را به شکل شروع-پایان بفرستید و چند بازه را با ویرگول جدا کنید. نمونه:\n20000-30000\nکلاینت ها هر چند ثانیه بین این پورت ها جابجا می شوند. برای خاموش کردن - بفرستید:")},
			"hs":     {T("سرور دست دهی"), T("دامنه یک سایت با TLS 1.3 را بفرستید (مثلا www.microsoft.com یا www.microsoft.com:443):")},
		}[f]
		r.ask("ifield", a, p[0], p[1], cbd("iv", id))
	})
	onInput("ifield", func(r *req, a []string, text string, _ *Document) {
		id, f := argInt(a, 0), argStr(a, 1)
		body := map[string]any{}
		switch f {
		case "remark":
			body["remark"] = text
		case "port":
			n, ok := parseInt(text)
			if !ok {
				r.ask("ifield", a, T("پورت"), T("⚠️ یک عدد بفرستید:"), cbd("iv", id))
				return
			}
			body["port"] = n
		case "addr":
			if text == "-" {
				text = ""
			}
			body["address_override"] = text
		case "hop":
			text = strings.TrimSpace(text)
			if text == "-" {
				text = ""
			}
			body["settings"] = map[string]any{"hop_ports": text}
		case "hs":
			host, port := strings.TrimSpace(text), 443
			if h, ps, ok := strings.Cut(host, ":"); ok {
				n, ok := parseInt(ps)
				if !ok {
					r.ask("ifield", a, T("سرور دست دهی"), T("⚠️ پورت درست نیست. دوباره بفرستید:"), cbd("iv", id))
					return
				}
				host, port = h, int(n)
			}
			body["settings"] = map[string]any{"handshake_server": host, "handshake_port": port}
		}
		inboundPatch(r, id, body)
	})
	on("ij", inboundJSON)
	onInput("ijson", inboundJSONInput)
	on("ip", inboundPlacement)
	on("ipt", inboundPlacement)
	on("ips", inboundPlacementSave)
	on("id", func(r *req, a []string) {
		id := argInt(a, 0)
		var ib inbound
		if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &ib); err != nil {
			r.fail(err, "il")
			return
		}
		q := T("ورودی <b>") + esc(ib.Remark) + T("</b> حذف شود؟")
		if ib.UserCount > 0 {
			q += "\n" + faInt(ib.UserCount) + T(" کاربر از این ورودی جدا می شوند.")
		}
		confirm(r, T("حذف ورودی"), q, cbd("idy", id), cbd("iv", id))
	})
	on("idy", func(r *req, a []string) {
		id := argInt(a, 0)
		r.progress(T("در حال حذف ورودی"))
		if err := r.call("DELETE", fmt.Sprintf("/inbounds/%d", id), nil, nil); err != nil {
			r.fail(err, cbd("iv", id))
			return
		}
		inboundsList(r, nil)
	})
	// create
	on("in", func(r *req, _ []string) {
		r.st.draft = map[string]string{}
		var bs []Button
		for _, p := range protoNames {
			bs = append(bs, btn(p.label, "inp", p.key))
		}
		kb := chunk(bs, 3)
		r.show(view{text: header("➕", T("ورودی جدید")) + T("پروتکل را انتخاب کنید:"), kb: append(kb, nav("il"))})
	})
}

func inboundsList(r *req, _ []string) {
	var list []inbound
	if err := r.call("GET", "/inbounds", nil, &list); err != nil {
		r.fail(err, "home")
		return
	}
	text := header("📥", T("ورودی ها")) + T("تعداد: <b>") + faInt(len(list)) + "</b>\n"
	if len(list) == 0 {
		text += T("\nهنوز ورودی ای ساخته نشده است.")
	}
	kb := Keyboard{}
	for _, ib := range list {
		mark := "✅"
		if !ib.Enabled {
			mark = "⛔️"
		}
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s:%s · 👥%s", mark, cut(ib.Remark, 16), protoLabel(ib.Protocol), fa(strconv.Itoa(ib.Port)), faInt(ib.UserCount)), "iv", ib.ID)})
	}
	kb = append(kb, []Button{btn(T("➕ ورودی جدید"), "in")}, nav("home"))
	r.show(view{text: text, kb: kb})
}

func inboundView(r *req, a []string) {
	id := argInt(a, 0)
	var ib inbound
	if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &ib); err != nil {
		r.fail(err, "il")
		return
	}
	ns := r.nodes()
	t := header("📥", ib.Remark)
	st := T("✅ روشن")
	if !ib.Enabled {
		st = T("⛔️ خاموش")
	}
	t += T("وضعیت: ") + st + "\n"
	t += T("پروتکل: <b>") + protoLabel(ib.Protocol) + T("</b> · پورت: <b>") + fa(strconv.Itoa(ib.Port)) + "</b>\n"
	t += T("شبکه: ") + esc(ib.Network) + T(" · امنیت: ") + esc(ib.Security) + "\n"
	if ib.Reality != nil && ib.Security == "reality" {
		t += T("مقصد Reality: ") + code(ib.Reality.Dest) + "\n"
	}
	if ib.TLS != nil && ib.Security == "tls" && ib.TLS.SNI != "" {
		t += "SNI: " + code(ib.TLS.SNI) + "\n"
	}
	if ib.Protocol == "vless" {
		t += T("🔐 رمزگذاری VLESS: ") + encSummary(ib.VLESSEnc) + "\n"
	}
	if ib.Protocol == "shadowtls" {
		t += T("سرور دست دهی: ") + code(fmt.Sprintf("%s:%d", ib.Settings.HandshakeServer, ib.Settings.HandshakePort)) + "\n"
	}
	if ib.Protocol == "hysteria" {
		if ib.Settings.HopPorts != "" {
			t += T("🔀 پرش پورت: ") + code(ib.Settings.HopPorts) + "\n"
		} else {
			t += T("🔀 پرش پورت: خاموش") + "\n"
		}
	}
	if singboxProto(ib.Protocol) {
		t += T("ℹ️ با sing-box و فقط روی سرور اصلی اجرا می شود.") + "\n"
	}
	if sum := advSummary(ib.Advanced); sum != "" {
		t += T("⚙️ پیشرفته: ") + sum + "\n"
	}
	if ib.AddressOverride != "" {
		t += T("آدرس در لینک ها: ") + code(ib.AddressOverride) + "\n"
	}
	t += T("👥 کاربران: ") + faInt(ib.UserCount) + T(" · 🔀 هاست ها: ") + faInt(ib.HostCount) + "\n"
	t += T("🖥 اجرا روی: ") + placeText(&ib, ns) + "\n"
	for _, o := range ib.XrayTooOld {
		where := T("سرور اصلی")
		if o.NodeID != 0 {
			where = T("نود ") + esc(nodeName(ns, o.NodeID))
		}
		t += "⚠️ " + where + T(" با Xray-core ") + code(o.Version) + T(" این ورودی را اجرا نمی کند؛ نیاز: ") + needsText(o.Needs) + T(". Xray آن را بروزرسانی کنید.") + "\n"
	}
	toggle := btn(T("⛔️ خاموش کردن"), "it", id)
	if !ib.Enabled {
		toggle = btn(T("✅ روشن کردن"), "it", id)
	}
	kb := Keyboard{
		{toggle},
		{btn(T("✏️ نام"), "ie", id, "remark"), btn(T("✏️ پورت"), "ie", id, "port")},
		{btn(T("🌐 آدرس لینک ها"), "ie", id, "addr"), btn(T("🧾 ویرایش کامل (JSON)"), "ij", id)},
	}
	if ib.Protocol == "vless" {
		kb = append(kb, []Button{btn(T("🔐 رمزگذاری VLESS"), "ve", id)})
	}
	if ib.Protocol == "hysteria" {
		kb = append(kb, []Button{btn(T("🔀 پرش پورت"), "ie", id, "hop")})
	}
	if ib.Protocol == "shadowtls" {
		kb = append(kb, []Button{btn(T("🤝 سرور دست دهی"), "ie", id, "hs")})
	}
	if inbounds.IsV2(ib.Protocol) || ib.Protocol == inbounds.HTTP || inbounds.TLSRequired(ib.Protocol) {
		kb = append(kb, []Button{btn(T("🔧 امنیت و انتقال"), "wze", id)})
	}
	if advApplies(&ib) {
		kb = append(kb, []Button{btn(T("⚙️ تنظیمات پیشرفته"), "xv", id)})
	}
	if len(ns) > 0 && !singboxProto(ib.Protocol) {
		kb = append(kb, []Button{btn(T("🖥 محل اجرا"), "ip", id)})
	}
	kb = append(kb, []Button{btn(T("🗑 حذف"), "id", id), btn(T("🔄 تازه سازی"), "iv", id)}, nav("il"))
	r.show(view{text: t, kb: kb})
}

func nodeName(ns []nodeRow, id int64) string {
	for _, n := range ns {
		if n.ID == id {
			return n.Name
		}
	}
	return "#" + strconv.FormatInt(id, 10)
}

func placeText(ib *inbound, ns []nodeRow) string {
	names := map[int64]string{}
	for _, n := range ns {
		names[n.ID] = n.Name
	}
	var parts []string
	if ib.OnMain || len(ib.NodeIDs) == 0 {
		parts = append(parts, T("سرور اصلی"))
	}
	for _, id := range ib.NodeIDs {
		parts = append(parts, T("نود ")+esc(names[id]))
	}
	return strings.Join(parts, T("، "))
}

func inboundPatch(r *req, id int64, body map[string]any) {
	r.toast("⏳")
	if err := r.call("PATCH", fmt.Sprintf("/inbounds/%d", id), body, nil); err != nil {
		r.fail(err, cbd("iv", id))
		return
	}
	inboundView(r, []string{strconv.FormatInt(id, 10)})
}

func inboundJSON(r *req, a []string) {
	id := argInt(a, 0)
	var raw map[string]any
	if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &raw); err != nil {
		r.fail(err, "il")
		return
	}
	for _, k := range []string{"id", "user_count", "host_count", "on_main", "created_at"} {
		delete(raw, k)
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	if err := r.cl.SendDocument(r.ctx, r.chat, fmt.Sprintf("inbound-%d.json", id), b, T("🧾 تنظیمات کامل ورودی")); err != nil {
		r.fail(err, cbd("iv", id))
		return
	}
	r.msgID = 0
	r.ask("ijson", a, T("ویرایش کامل ورودی"), T("فایل بالا را ویرایش کنید و همان را (به شکل فایل یا متن JSON) بفرستید. فقط کلید هایی که می فرستید تغییر می کنند."), cbd("iv", id))
}

func inboundJSONInput(r *req, a []string, text string, doc *Document) {
	id := argInt(a, 0)
	data := []byte(text)
	if doc != nil {
		b, err := r.cl.Download(r.ctx, doc.FileID, 1<<20)
		if err != nil {
			r.fail(err, cbd("iv", id))
			return
		}
		data = b
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		r.ask("ijson", a, T("ویرایش کامل ورودی"), T("⚠️ JSON درست نیست: ")+esc(err.Error())+T("\nدوباره بفرستید:"), cbd("iv", id))
		return
	}
	inboundPatch(r, id, body)
}

// inboundPlacement chooses where an inbound runs (main server, nodes).
func inboundPlacement(r *req, a []string) {
	id := argInt(a, 0)
	var ib inbound
	if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &ib); err != nil {
		r.fail(err, "il")
		return
	}
	key := "pl" + strconv.FormatInt(id, 10)
	if _, ok := r.st.draft[key]; !ok || len(a) == 1 {
		sel := []string{}
		if ib.OnMain || len(ib.NodeIDs) == 0 {
			sel = append(sel, "0")
		}
		for _, n := range ib.NodeIDs {
			sel = append(sel, strconv.FormatInt(n, 10))
		}
		r.st.draft[key] = strings.Join(sel, ",")
	}
	sel := setOf(r.st.draft[key])
	if len(a) > 1 {
		k := a[1]
		sel[k] = !sel[k]
		r.st.draft[key] = joinSet(sel)
	}
	ns := r.nodes()
	kb := Keyboard{{btn(onOff(sel["0"])+T(" سرور اصلی"), "ipt", id, 0)}}
	for _, n := range ns {
		k := strconv.FormatInt(n.ID, 10)
		kb = append(kb, []Button{btn(onOff(sel[k])+T(" نود ")+cut(n.Name, 20), "ipt", id, n.ID)})
	}
	kb = append(kb, []Button{btn(T("💾 ذخیره"), "ips", id)}, nav(cbd("iv", id)))
	r.show(view{text: header("🖥", T("محل اجرای ")+ib.Remark) + T("سرور هایی که این ورودی روی آن ها اجرا شود را انتخاب کنید. اگر هیچ نودی انتخاب نشود، روی سرور اصلی اجرا می شود."), kb: kb})
}

func inboundPlacementSave(r *req, a []string) {
	id := argInt(a, 0)
	sel := setOf(r.st.draft["pl"+strconv.FormatInt(id, 10)])
	ids := []int64{}
	for k, v := range sel {
		if n, err := strconv.ParseInt(k, 10, 64); err == nil && v && n > 0 {
			ids = append(ids, n)
		}
	}
	delete(r.st.draft, "pl"+strconv.FormatInt(id, 10))
	inboundPatch(r, id, map[string]any{"node_ids": ids, "serve_main": sel["0"] || len(ids) == 0})
}

func setOf(s string) map[string]bool {
	m := map[string]bool{}
	for _, x := range strings.Split(s, ",") {
		if x != "" {
			m[x] = true
		}
	}
	return m
}

func joinSet(m map[string]bool) string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return strings.Join(out, ",")
}
