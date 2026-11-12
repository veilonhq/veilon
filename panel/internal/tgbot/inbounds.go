package tgbot

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
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
	AddressOverride string  `json:"address_override"`
	UserCount       int64   `json:"user_count"`
	HostCount       int64   `json:"host_count"`
	NodeIDs         []int64 `json:"node_ids"`
	ServeMain       bool    `json:"serve_main"`
	OnMain          bool    `json:"on_main"`
}

var protoNames = []struct{ key, label string }{
	{"vless", "VLESS"}, {"vmess", "VMess"}, {"trojan", "Trojan"}, {"shadowsocks", "Shadowsocks"},
	{"hysteria", "Hysteria2"}, {"tuic", "TUIC"}, {"wireguard", "WireGuard"}, {"amneziawg", "AmneziaWG"},
	{"mtproto", "MTProto"}, {"masque", "MASQUE"}, {"http", "HTTP"}, {"mixed", "Mixed"},
	{"tunnel", "Tunnel"}, {"tun", "TUN"},
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
		NeedsUpdate []struct {
			ID     int64  `json:"id"`
			Remark string `json:"remark"`
		} `json:"needs_update"`
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
			"remark": {"نام ورودی", "نام تازه را بفرستید:"},
			"port":   {"پورت", "پورت تازه را بفرستید (۱ تا ۶۵۵۳۵):"},
			"addr":   {"آدرس در لینک ها", "آدرس یا دامنه ای که در لینک ها نوشته شود را بفرستید (- برای آدرس پیش فرض سرور):"},
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
				r.ask("ifield", a, "پورت", "⚠️ یک عدد بفرستید:", cbd("iv", id))
				return
			}
			body["port"] = n
		case "addr":
			if text == "-" {
				text = ""
			}
			body["address_override"] = text
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
		q := "ورودی <b>" + esc(ib.Remark) + "</b> حذف شود؟"
		if ib.UserCount > 0 {
			q += "\n" + faInt(ib.UserCount) + " کاربر از این ورودی جدا می شوند."
		}
		confirm(r, "حذف ورودی", q, cbd("idy", id), cbd("iv", id))
	})
	on("idy", func(r *req, a []string) {
		id := argInt(a, 0)
		r.progress("در حال حذف ورودی")
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
		r.show(view{text: header("➕", "ورودی جدید") + "پروتکل را انتخاب کنید:", kb: append(kb, nav("il"))})
	})
	on("inp", func(r *req, a []string) {
		r.st.draft = map[string]string{"proto": argStr(a, 0)}
		r.ask("inremark", nil, "ورودی جدید · "+protoLabel(argStr(a, 0)), "یک نام برای ورودی بفرستید:", "in")
	})
	onInput("inremark", func(r *req, _ []string, text string, _ *Document) {
		r.st.draft["remark"] = text
		inboundAskPort(r, "")
	})
	on("inrp", func(r *req, a []string) {
		r.st.draft["port"] = argStr(a, 0)
		inboundAskExtra(r)
	})
	onInput("inport", func(r *req, _ []string, text string, _ *Document) {
		n, ok := parseInt(text)
		if !ok || n < 1 || n > 65535 {
			inboundAskPort(r, "⚠️ پورت باید عددی از ۱ تا ۶۵۵۳۵ باشد.\n")
			return
		}
		r.st.draft["port"] = strconv.FormatInt(n, 10)
		inboundAskExtra(r)
	})
	onInput("inextra", func(r *req, _ []string, text string, _ *Document) {
		r.st.draft["extra"] = strings.TrimSpace(text)
		inboundAskPlace(r, nil)
	})
	on("inx", func(r *req, _ []string) {
		r.st.draft["extra"] = ""
		inboundAskPlace(r, nil)
	})
	on("inpl", inboundAskPlace)
	on("inok", inboundCreate)
}

func inboundsList(r *req, _ []string) {
	var list []inbound
	if err := r.call("GET", "/inbounds", nil, &list); err != nil {
		r.fail(err, "home")
		return
	}
	text := header("📥", "ورودی ها") + "تعداد: <b>" + faInt(len(list)) + "</b>\n"
	if len(list) == 0 {
		text += "\nهنوز ورودی ای ساخته نشده است."
	}
	kb := Keyboard{}
	for _, ib := range list {
		mark := "✅"
		if !ib.Enabled {
			mark = "⛔️"
		}
		kb = append(kb, []Button{btn(fmt.Sprintf("%s %s · %s:%s · 👥%s", mark, cut(ib.Remark, 16), protoLabel(ib.Protocol), fa(strconv.Itoa(ib.Port)), faInt(ib.UserCount)), "iv", ib.ID)})
	}
	kb = append(kb, []Button{btn("➕ ورودی جدید", "in")}, nav("home"))
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
	st := "✅ روشن"
	if !ib.Enabled {
		st = "⛔️ خاموش"
	}
	t += "وضعیت: " + st + "\n"
	t += "پروتکل: <b>" + protoLabel(ib.Protocol) + "</b> · پورت: <b>" + fa(strconv.Itoa(ib.Port)) + "</b>\n"
	t += "شبکه: " + esc(ib.Network) + " · امنیت: " + esc(ib.Security) + "\n"
	if ib.Reality != nil && ib.Security == "reality" {
		t += "مقصد Reality: " + code(ib.Reality.Dest) + "\n"
	}
	if ib.TLS != nil && ib.Security == "tls" && ib.TLS.SNI != "" {
		t += "SNI: " + code(ib.TLS.SNI) + "\n"
	}
	if ib.AddressOverride != "" {
		t += "آدرس در لینک ها: " + code(ib.AddressOverride) + "\n"
	}
	t += "👥 کاربران: " + faInt(ib.UserCount) + " · 🔀 هاست ها: " + faInt(ib.HostCount) + "\n"
	t += "🖥 اجرا روی: " + placeText(&ib, ns) + "\n"
	toggle := btn("⛔️ خاموش کردن", "it", id)
	if !ib.Enabled {
		toggle = btn("✅ روشن کردن", "it", id)
	}
	kb := Keyboard{
		{toggle},
		{btn("✏️ نام", "ie", id, "remark"), btn("✏️ پورت", "ie", id, "port")},
		{btn("🌐 آدرس لینک ها", "ie", id, "addr"), btn("🧾 ویرایش کامل (JSON)", "ij", id)},
	}
	if len(ns) > 0 {
		kb = append(kb, []Button{btn("🖥 محل اجرا", "ip", id)})
	}
	kb = append(kb, []Button{btn("🗑 حذف", "id", id), btn("🔄 تازه سازی", "iv", id)}, nav("il"))
	r.show(view{text: t, kb: kb})
}

func placeText(ib *inbound, ns []nodeRow) string {
	names := map[int64]string{}
	for _, n := range ns {
		names[n.ID] = n.Name
	}
	var parts []string
	if ib.OnMain || len(ib.NodeIDs) == 0 {
		parts = append(parts, "سرور اصلی")
	}
	for _, id := range ib.NodeIDs {
		parts = append(parts, "نود "+esc(names[id]))
	}
	return strings.Join(parts, "، ")
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
	if err := r.cl.SendDocument(r.ctx, r.chat, fmt.Sprintf("inbound-%d.json", id), b, "🧾 تنظیمات کامل ورودی"); err != nil {
		r.fail(err, cbd("iv", id))
		return
	}
	r.msgID = 0
	r.ask("ijson", a, "ویرایش کامل ورودی", "فایل بالا را ویرایش کنید و همان را (به شکل فایل یا متن JSON) بفرستید. فقط کلید هایی که می فرستید تغییر می کنند.", cbd("iv", id))
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
		r.ask("ijson", a, "ویرایش کامل ورودی", "⚠️ JSON درست نیست: "+esc(err.Error())+"\nدوباره بفرستید:", cbd("iv", id))
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
	kb := Keyboard{{btn(onOff(sel["0"])+" سرور اصلی", "ipt", id, 0)}}
	for _, n := range ns {
		k := strconv.FormatInt(n.ID, 10)
		kb = append(kb, []Button{btn(onOff(sel[k])+" نود "+cut(n.Name, 20), "ipt", id, n.ID)})
	}
	kb = append(kb, []Button{btn("💾 ذخیره", "ips", id)}, nav(cbd("iv", id)))
	r.show(view{text: header("🖥", "محل اجرای "+ib.Remark) + "سرور هایی که این ورودی روی آن ها اجرا شود را انتخاب کنید. اگر هیچ نودی انتخاب نشود، روی سرور اصلی اجرا می شود.", kb: kb})
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

func inboundAskPort(r *req, warn string) {
	p := 10000 + rand.IntN(50000)
	r.ask("inport", nil, "ورودی جدید · "+protoLabel(r.st.draft["proto"]), warn+"پورت را بفرستید:", "in",
		[]Button{btn("🎲 پورت تصادفی "+fa(strconv.Itoa(p)), "inrp", p), btn("۴۴۳", "inrp", 443)})
}

// inboundAskExtra asks the one protocol-specific value the bot needs.
func inboundAskExtra(r *req) {
	p := r.st.draft["proto"]
	title := "ورودی جدید · " + protoLabel(p)
	switch p {
	case "vless":
		r.ask("inextra", nil, title, "دامنه مقصد Reality را بفرستید (مثلا www.microsoft.com):", "in",
			[]Button{btn("پیش فرض: www.microsoft.com", "inx")})
	case "trojan", "hysteria", "tuic", "masque":
		r.ask("inextra", nil, title, "SNI یا دامنه را بفرستید. بدون دامنه، یک گواهی خودامضا ساخته می شود.", "in",
			[]Button{btn("بدون دامنه (خودامضا)", "inx")})
	case "tunnel":
		r.ask("inextra", nil, title, "مقصد تونل را به شکل address:port بفرستید (مثلا 1.1.1.1:53):", "in")
	case "mtproto":
		r.ask("inextra", nil, title, "دامنه FakeTLS را بفرستید:", "in", []Button{btn("پیش فرض: www.cloudflare.com", "inx")})
	default:
		r.st.draft["extra"] = ""
		inboundAskPlace(r, nil)
	}
}

func inboundAskPlace(r *req, a []string) {
	ns := r.nodes()
	if len(ns) == 0 {
		r.st.draft["place"] = "0"
		inboundCreate(r, nil)
		return
	}
	if _, ok := r.st.draft["place"]; !ok {
		r.st.draft["place"] = "0"
	}
	sel := setOf(r.st.draft["place"])
	if len(a) > 0 {
		sel[a[0]] = !sel[a[0]]
		r.st.draft["place"] = joinSet(sel)
	}
	kb := Keyboard{{btn(onOff(sel["0"])+" سرور اصلی", "inpl", 0)}}
	for _, n := range ns {
		k := strconv.FormatInt(n.ID, 10)
		kb = append(kb, []Button{btn(onOff(sel[k])+" نود "+cut(n.Name, 20), "inpl", n.ID)})
	}
	kb = append(kb, []Button{btn("✅ ساختن ورودی", "inok")}, []Button{btn("✖️ انصراف", "il")})
	r.show(view{text: header("🖥", "ورودی جدید · محل اجرا") + "این ورودی روی کدام سرور ها اجرا شود؟", kb: kb})
}

// inboundBody builds a new inbound the way the web wizard does.
func inboundBody(d map[string]string) (map[string]any, error) {
	p := d["proto"]
	port, _ := strconv.Atoi(d["port"])
	extra := d["extra"]
	settings := map[string]any{}
	b := map[string]any{"remark": d["remark"], "enabled": true, "protocol": p, "port": port, "network": "tcp", "security": "none",
		"transport": map[string]any{"path": "", "host": "", "service_name": "", "mode": ""}, "sniffing": true}
	tls := func(alpn []string) map[string]any {
		return map[string]any{"cert_id": nil, "sni": extra, "alpn": alpn, "fingerprint": "chrome", "allow_insecure": false}
	}
	switch p {
	case "vless":
		dest := extra
		if dest == "" {
			dest = "www.microsoft.com"
		}
		host := strings.TrimSuffix(dest, ":443")
		b["security"], b["flow"] = "reality", "xtls-rprx-vision"
		b["reality"] = map[string]any{"dest": host + ":443", "server_names": []string{host}, "private_key": "", "public_key": "",
			"short_ids": []string{}, "fingerprint": "chrome", "spider_x": "/"}
	case "vmess":
		b["network"] = "ws"
		b["transport"] = map[string]any{"path": "/ws", "host": "", "service_name": "", "mode": ""}
	case "trojan":
		b["security"], b["tls"] = "tls", tls([]string{"h2", "http/1.1"})
	case "shadowsocks":
		b["ss_method"] = "2022-blake3-aes-128-gcm"
	case "hysteria":
		b["security"], b["tls"] = "tls", tls([]string{"h3"})
	case "tuic":
		b["security"], b["tls"] = "tls", tls([]string{"h3"})
		settings["congestion"] = "bbr"
	case "masque":
		b["security"], b["tls"] = "tls", tls([]string{"h3"})
		settings["pool"], settings["mtu"], settings["path"] = []string{"10.14.0.1/24", "fd14::1/64"}, 1280, "/.well-known/masque/ip/{target}/{ipproto}/"
	case "wireguard", "amneziawg":
		settings["dns"], settings["mtu"], settings["keepalive"] = []string{"1.1.1.1", "8.8.8.8"}, 1420, 25
	case "tun":
		settings["tun_name"], settings["gateway"], settings["mtu"] = "veilon-tun", []string{"10.77.0.1/24"}, 1500
	case "mixed":
		settings["udp"] = true
	case "tunnel":
		host, ps, ok := strings.Cut(extra, ":")
		tp, err := strconv.Atoi(ps)
		if !ok || err != nil || host == "" {
			return nil, fmt.Errorf("tunnel target must be address:port")
		}
		settings["target_address"], settings["target_port"], settings["tunnel_network"] = host, tp, "tcp"
	case "mtproto":
		dom := extra
		if dom == "" {
			dom = "www.cloudflare.com"
		}
		settings["faketls_domain"] = dom
	}
	switch p {
	case "vless", "vmess", "trojan", "shadowsocks", "http", "mixed", "tunnel":
	default:
		b["sniffing"] = false
	}
	b["settings"] = settings
	ids := []int64{}
	sel := setOf(d["place"])
	for k, v := range sel {
		if n, err := strconv.ParseInt(k, 10, 64); err == nil && v && n > 0 {
			ids = append(ids, n)
		}
	}
	b["node_ids"], b["serve_main"] = ids, sel["0"] || len(ids) == 0
	return b, nil
}

func inboundCreate(r *req, _ []string) {
	if r.st.draft["proto"] == "" {
		inboundsList(r, nil)
		return
	}
	body, err := inboundBody(r.st.draft)
	if err != nil {
		r.fail(err, "in")
		return
	}
	r.progress("در حال ساختن ورودی")
	var out inbound
	if err := r.call("POST", "/inbounds", body, &out); err != nil {
		r.fail(err, "in")
		return
	}
	r.st.draft = map[string]string{}
	inboundView(r, []string{strconv.FormatInt(out.ID, 10)})
}
