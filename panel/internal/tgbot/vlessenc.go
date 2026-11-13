package tgbot

import (
	"fmt"
	"strconv"
)

// VLESS Encryption of a VLESS inbound: view and edit through the panel API
// (PATCH /inbounds/{id} with "vless_encryption"; the server makes keys).

type vlessEnc struct {
	Enabled       bool   `json:"enabled"`
	Auth          string `json:"auth"`
	Mode          string `json:"mode"`
	RTT           string `json:"rtt"`
	TicketSeconds int    `json:"ticket_seconds"`
}

func encAuthLabel(a string) string {
	if a == "mlkem768" {
		return "ML-KEM-768"
	}
	return "X25519"
}

// encSummary is the one line shown on the inbound screen.
func encSummary(e *vlessEnc) string {
	if e == nil || !e.Enabled {
		return T("خاموش")
	}
	rtt := "1-RTT"
	if e.RTT == "0rtt" {
		rtt = "0-RTT " + fa(strconv.Itoa(e.TicketSeconds)) + "s"
	}
	return T("✅ روشن") + " · " + encAuthLabel(e.Auth) + " · " + e.Mode + " · " + rtt
}

// visionOK mirrors the server rule: Vision needs VLESS Encryption, or tcp
// with TLS/Reality.
func visionOK(ib *inbound, encOn bool) bool {
	return encOn || ib.Network == "tcp" && ib.Security != "none"
}

func init() {
	on("ve", encView)
	on("vet", func(r *req, a []string) {
		id := argInt(a, 0)
		ib, ok := encLoad(r, id)
		if !ok {
			return
		}
		on := ib.VLESSEnc == nil || !ib.VLESSEnc.Enabled
		body := map[string]any{"vless_encryption": map[string]any{"enabled": on}}
		// Turning it off takes Vision away where it needs the encryption.
		if !on && ib.Flow != "" && !visionOK(ib, false) {
			body["flow"] = ""
		}
		encPatch(r, id, body)
	})
	on("vea", func(r *req, a []string) {
		auth := "x25519"
		if argStr(a, 1) == "m" {
			auth = "mlkem768"
		}
		// A new key type needs a new key: the server makes one.
		encPatch(r, argInt(a, 0), map[string]any{"vless_encryption": map[string]any{"auth": auth, "private_key": ""}})
	})
	on("vem", func(r *req, a []string) {
		m := argStr(a, 1)
		if m != "native" && m != "xorpub" && m != "random" {
			m = "native"
		}
		encPatch(r, argInt(a, 0), map[string]any{"vless_encryption": map[string]any{"mode": m}})
	})
	on("ver", func(r *req, a []string) {
		e := map[string]any{"rtt": "1rtt", "ticket_seconds": 0}
		if argStr(a, 1) == "0" {
			e = map[string]any{"rtt": "0rtt", "ticket_seconds": 600}
		}
		encPatch(r, argInt(a, 0), map[string]any{"vless_encryption": e})
	})
	on("ves", func(r *req, a []string) {
		id := argInt(a, 0)
		r.ask("vesec", a, T("عمر بلیت 0-RTT"), T("عمر بلیت را به ثانیه بفرستید (۱ تا ۸۶۴۰۰). Xray هر بار عددی بین نصف تا کل آن برمی دارد."), cbd("ve", id),
			[]Button{btn("600", "vesp", id, 600), btn("3600", "vesp", id, 3600)})
	})
	on("vesp", func(r *req, a []string) {
		encPatch(r, argInt(a, 0), map[string]any{"vless_encryption": map[string]any{"rtt": "0rtt", "ticket_seconds": argInt(a, 1)}})
	})
	onInput("vesec", func(r *req, a []string, text string, _ *Document) {
		id := argInt(a, 0)
		n, ok := parseInt(text)
		if !ok || n < 1 || n > 86400 {
			r.ask("vesec", a, T("عمر بلیت 0-RTT"), T("⚠️ یک عدد از ۱ تا ۸۶۴۰۰ بفرستید:"), cbd("ve", id))
			return
		}
		encPatch(r, id, map[string]any{"vless_encryption": map[string]any{"rtt": "0rtt", "ticket_seconds": n}})
	})
	on("vek", func(r *req, a []string) {
		id := argInt(a, 0)
		confirm(r, T("کلید تازه"), T("کلید تازه ساخته شود؟ لینک همه کاربران این ورودی عوض می شود و باید اشتراکشان را به روز کنند."), cbd("veky", id), cbd("ve", id))
	})
	on("veky", func(r *req, a []string) {
		encPatch(r, argInt(a, 0), map[string]any{"vless_encryption": map[string]any{"private_key": ""}})
	})
	on("vef", func(r *req, a []string) {
		id := argInt(a, 0)
		ib, ok := encLoad(r, id)
		if !ok {
			return
		}
		flow := "xtls-rprx-vision"
		if ib.Flow != "" {
			flow = ""
		}
		encPatch(r, id, map[string]any{"flow": flow})
	})
}

func encLoad(r *req, id int64) (*inbound, bool) {
	var ib inbound
	if err := r.call("GET", fmt.Sprintf("/inbounds/%d", id), nil, &ib); err != nil {
		r.fail(err, "il")
		return nil, false
	}
	return &ib, true
}

func encPatch(r *req, id int64, body map[string]any) {
	r.toast("⏳")
	if err := r.call("PATCH", fmt.Sprintf("/inbounds/%d", id), body, nil); err != nil {
		r.fail(err, cbd("ve", id))
		return
	}
	encView(r, []string{strconv.FormatInt(id, 10)})
}

func encView(r *req, a []string) {
	id := argInt(a, 0)
	ib, ok := encLoad(r, id)
	if !ok {
		return
	}
	if ib.Protocol != "vless" {
		inboundView(r, a)
		return
	}
	e := ib.VLESSEnc
	if e == nil {
		e = &vlessEnc{Auth: "x25519", Mode: "native", RTT: "0rtt", TicketSeconds: 600}
	}
	t := header("🔐", T("رمزگذاری VLESS · ")+ib.Remark)
	t += T("لایه رمزگذاری پساکوانتومی خود VLESS. سرور کلید خصوصی را نگه می دارد و کلاینت ها کلید عمومی را از لینک می گیرند.\n\n")
	t += T("وضعیت: ") + encSummary(e) + "\n"
	flow := T("خاموش")
	if ib.Flow != "" {
		flow = "xtls-rprx-vision"
	}
	t += "Flow (Vision): " + flow + "\n"
	if e.Enabled {
		t += T("\nsing-box این قابلیت را ندارد و این ورودی در خروجی sing-box نمی آید. کلاینت باید Xray نسخه ۲۵.۹.۵ یا تازه تر داشته باشد.")
	}
	toggle := btn(T("✅ روشن کردن"), "vet", id)
	if e.Enabled {
		toggle = btn(T("⛔️ خاموش کردن"), "vet", id)
	}
	kb := Keyboard{{toggle}}
	if e.Enabled {
		kb = append(kb,
			[]Button{btn(onOff(e.Auth == "x25519")+" X25519", "vea", id, "x"), btn(onOff(e.Auth == "mlkem768")+" ML-KEM-768", "vea", id, "m")},
			[]Button{btn(onOff(e.Mode == "native")+" native", "vem", id, "native"), btn(onOff(e.Mode == "xorpub")+" xorpub", "vem", id, "xorpub"), btn(onOff(e.Mode == "random")+" random", "vem", id, "random")},
			[]Button{btn(onOff(e.RTT == "0rtt")+" 0-RTT", "ver", id, 0), btn(onOff(e.RTT == "1rtt")+" 1-RTT", "ver", id, 1)},
		)
		if e.RTT == "0rtt" {
			kb = append(kb, []Button{btn(T("⏱ عمر بلیت: ")+fa(strconv.Itoa(e.TicketSeconds))+"s", "ves", id)})
		}
		kb = append(kb, []Button{btn(T("🔑 کلید تازه"), "vek", id)})
	}
	if visionOK(ib, e.Enabled) {
		kb = append(kb, []Button{btn(onOff(ib.Flow != "")+" Vision (xtls-rprx-vision)", "vef", id)})
	}
	kb = append(kb, nav(cbd("iv", id)))
	r.show(view{text: t, kb: kb})
}
