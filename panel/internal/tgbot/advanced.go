package tgbot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Advanced settings of an inbound (the "advanced" object of the panel API):
// the common switches, key generation for Reality ML-DSA-65 and TLS ECH, and
// the reverse-proxy bridge config. Everything else stays in the web panel or
// the full JSON edit. The API replaces "advanced" whole, so every change
// reads the stored object, edits one key and sends it all back.

var advKeyNames = []struct {
	key   string
	label func() string
}{
	{"sniffing", func() string { return T("شنود") }},
	{"sockopt", func() string { return T("سوکت") }},
	{"fallbacks", func() string { return T("فال بک") }},
	{"tls", func() string { return "TLS" }},
	{"reality", func() string { return "Reality" }},
	{"tcp", func() string { return T("هدر HTTP") }},
	{"ws", func() string { return T("هدر ها") }},
	{"grpc", func() string { return "gRPC" }},
	{"xhttp", func() string { return "XHTTP" }},
	{"kcp", func() string { return "mKCP" }},
	{"hysteria", func() string { return "Hysteria 2" }},
	{"tuic", func() string { return "TUIC" }},
	{"ss", func() string { return T("UDP خاموش") }},
	{"masks", func() string { return T("ماسک ها") }},
	{"reverse", func() string { return T("پروکسی معکوس") }},
}

func isV2Proto(p string) bool {
	return p == "vless" || p == "vmess" || p == "trojan" || p == "shadowsocks"
}

// advApplies reports whether the advanced screen has anything for the
// inbound (Xray-core protocols with a stream).
func advApplies(ib *inbound) bool {
	return isV2Proto(ib.Protocol) || ib.Protocol == "hysteria" || ib.Protocol == "tuic"
}

// advSummary lists the advanced sections in use ("" = none).
func advSummary(adv map[string]any) string {
	var parts []string
	for _, k := range advKeyNames {
		if v, ok := adv[k.key]; ok && v != nil && !advEmpty(v) {
			parts = append(parts, k.label())
		}
	}
	return strings.Join(parts, T("، "))
}

func advEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		for _, y := range x {
			if !advEmpty(y) {
				return false
			}
		}
		return true
	case []any:
		return len(x) == 0
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	}
	return false
}

func advSub(adv map[string]any, key string) map[string]any {
	if m, ok := adv[key].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	adv[key] = m
	return m
}

func advBool(adv map[string]any, key, field string) bool {
	m, _ := adv[key].(map[string]any)
	b, _ := m[field].(bool)
	return b
}

func advStr(adv map[string]any, key, field string) string {
	m, _ := adv[key].(map[string]any)
	s, _ := m[field].(string)
	return s
}

// advLoad reads the inbound and a private copy of its advanced object.
func advLoad(r *req, id int64) (*inbound, map[string]any, bool) {
	var raw struct {
		Advanced map[string]any `json:"advanced"`
	}
	var body json.RawMessage
	if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &body); err != nil {
		r.fail(err, "il")
		return nil, nil, false
	}
	var ib inbound
	_ = json.Unmarshal(body, &ib)
	_ = json.Unmarshal(body, &raw)
	if raw.Advanced == nil {
		raw.Advanced = map[string]any{}
	}
	return &ib, raw.Advanced, true
}

func advSave(r *req, id int64, adv map[string]any) {
	r.toast("⏳")
	if err := r.call("PATCH", fmt.Sprintf("/inbounds/%d", id), map[string]any{"advanced": adv}, nil); err != nil {
		r.fail(err, cbd("xv", id))
		return
	}
	advView(r, []string{strconv.FormatInt(id, 10)})
}

func init() {
	on("xv", advView)
	on("xs", func(r *req, a []string) {
		id := argInt(a, 0)
		_, adv, ok := advLoad(r, id)
		if !ok {
			return
		}
		switch argStr(a, 1) {
		case "ro":
			m := advSub(adv, "sniffing")
			m["route_only"] = !advBool(adv, "sniffing", "route_only")
		case "pp":
			m := advSub(adv, "sockopt")
			m["accept_proxy_protocol"] = !advBool(adv, "sockopt", "accept_proxy_protocol")
		case "tfo":
			m := advSub(adv, "sockopt")
			m["tcp_fast_open"] = !advBool(adv, "sockopt", "tcp_fast_open")
		case "sni":
			m := advSub(adv, "tls")
			m["reject_unknown_sni"] = !advBool(adv, "tls", "reject_unknown_sni")
		case "pin":
			m := advSub(adv, "tls")
			m["pin_cert"] = !advBool(adv, "tls", "pin_cert")
		case "udp":
			if advBool(adv, "ss", "no_udp") {
				delete(adv, "ss")
			} else {
				adv["ss"] = map[string]any{"no_udp": true}
			}
		case "rv":
			if advBool(adv, "reverse", "enabled") {
				delete(adv, "reverse")
			} else {
				// the server makes the bridge id
				adv["reverse"] = map[string]any{"enabled": true, "uuid": "", "route_self": true}
			}
		case "ml":
			m := advSub(adv, "reality")
			if advStr(adv, "reality", "mldsa65_seed") != "" {
				m["mldsa65_seed"], m["mldsa65_verify"] = "", ""
			} else {
				var k struct {
					Server string `json:"server"`
				}
				if err := r.call("POST", "/inbounds/mldsa65-keys", nil, &k); err != nil {
					r.fail(err, cbd("xv", id))
					return
				}
				m["mldsa65_seed"], m["mldsa65_verify"] = k.Server, ""
			}
		case "ech":
			m := advSub(adv, "tls")
			if advStr(adv, "tls", "ech_server_keys") != "" {
				m["ech_server_keys"], m["ech_config_list"] = "", ""
			} else {
				var k struct {
					Server string `json:"server"`
				}
				if err := r.call("POST", "/inbounds/ech-keys", map[string]any{"server_name": ""}, &k); err != nil {
					r.fail(err, cbd("xv", id))
					return
				}
				m["ech_server_keys"], m["ech_config_list"] = k.Server, ""
			}
		default:
			advView(r, a)
			return
		}
		advSave(r, id, adv)
	})
	on("xb", func(r *req, a []string) {
		id := argInt(a, 0)
		var cfg json.RawMessage
		if err := r.call("GET", fmt.Sprintf("/inbounds/%d/reverse-bridge", id), nil, &cfg); err != nil {
			r.fail(err, cbd("xv", id))
			return
		}
		var pretty any
		_ = json.Unmarshal(cfg, &pretty)
		b, _ := json.MarshalIndent(pretty, "", "  ")
		if err := r.cl.SendDocument(r.ctx, r.chat, fmt.Sprintf("veilon-bridge-%d.json", id), append(b, '\n'),
			T("🌉 تنظیمات پل (bridge). روی سرور پل Xray نصب کنید و با این فایل اجرا کنید: xray run -c veilon-bridge.json")); err != nil {
			r.fail(err, cbd("xv", id))
			return
		}
		r.msgID = 0
		advView(r, a)
	})
}

func advView(r *req, a []string) {
	id := argInt(a, 0)
	ib, adv, ok := advLoad(r, id)
	if !ok {
		return
	}
	if !advApplies(ib) {
		inboundView(r, a)
		return
	}
	t := header("⚙️", T("تنظیمات پیشرفته · ")+esc(ib.Remark))
	sum := advSummary(adv)
	if sum == "" {
		sum = T("هیچ (همان تنظیمات پیش فرض)")
	}
	t += T("در حال استفاده: ") + sum + "\n"
	if fb, _ := adv["fallbacks"].([]any); len(fb) > 0 {
		t += T("↩️ فال بک ها: ") + faInt(len(fb)) + "\n"
	}
	v2 := isV2Proto(ib.Protocol)
	tcpNet := ib.Network != "kcp"
	var kb Keyboard
	if ib.Sniffing && ib.Protocol != "tuic" {
		kb = append(kb, []Button{btn(onOff(advBool(adv, "sniffing", "route_only"))+T(" شنود فقط برای مسیریابی"), "xs", id, "ro")})
	}
	if v2 && tcpNet {
		kb = append(kb, []Button{
			btn(onOff(advBool(adv, "sockopt", "accept_proxy_protocol"))+" PROXY protocol", "xs", id, "pp"),
			btn(onOff(advBool(adv, "sockopt", "tcp_fast_open"))+" TCP Fast Open", "xs", id, "tfo"),
		})
	}
	if v2 && ib.Security == "tls" {
		kb = append(kb, []Button{
			btn(onOff(advBool(adv, "tls", "reject_unknown_sni"))+T(" رد SNI ناشناس"), "xs", id, "sni"),
			btn(onOff(advBool(adv, "tls", "pin_cert"))+T(" پین گواهی"), "xs", id, "pin"),
		})
		kb = append(kb, []Button{btn(onOff(advStr(adv, "tls", "ech_server_keys") != "")+" ECH", "xs", id, "ech")})
		if advStr(adv, "tls", "ech_server_keys") != "" {
			t += T("🔒 ECH روشن است؛ کلاینت ها کلید را از اشتراک می گیرند. کلاینت های قدیمی Xray و برخی برنامه ها ECH ندارند.\n")
		}
	}
	if ib.Security == "reality" {
		on := advStr(adv, "reality", "mldsa65_seed") != ""
		kb = append(kb, []Button{btn(onOff(on)+" ML-DSA-65", "xs", id, "ml")})
		if on {
			t += T("🛡 ML-DSA-65 روشن است. مقصد Reality باید گواهی بزرگ داشته باشد (مثل dl.google.com)؛ مقصد هایی مثل www.speedtest.net یا github.com وصل نمی شوند.\n")
		}
	}
	if ib.Protocol == "shadowsocks" && ib.Network == "tcp" {
		kb = append(kb, []Button{btn(onOff(!advBool(adv, "ss", "no_udp"))+T(" UDP روی این پورت"), "xs", id, "udp")})
	}
	if ib.Protocol == "vless" {
		rv := advBool(adv, "reverse", "enabled")
		kb = append(kb, []Button{btn(onOff(rv)+T(" پروکسی معکوس (پل)"), "xs", id, "rv")})
		if rv {
			t += T("🌉 کاربران از طریق سرور پل به اینترنت می روند. تا پل وصل نباشد اینترنت ندارند.\n")
			kb = append(kb, []Button{btn(T("📄 فایل تنظیمات پل"), "xb", id)})
		}
	}
	t += T("\nبقیه تنظیمات (XHTTP، mKCP، Hysteria، ماسک ها، هدر ها، فال بک ها و ...) در پنل وب یا با 🧾 ویرایش کامل (JSON).")
	kb = append(kb, nav(cbd("iv", id)))
	r.show(view{text: t, kb: kb})
}
