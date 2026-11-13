package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/cores"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// buttons returns the (label, data) pairs of a screen.
func buttons(c call) [][2]string {
	m, _ := c.p["reply_markup"].(map[string]any)
	rows, _ := m["inline_keyboard"].([]any)
	var out [][2]string
	for _, row := range rows {
		for _, b := range row.([]any) {
			bm := b.(map[string]any)
			out = append(out, [2]string{fmt.Sprint(bm["text"]), fmt.Sprint(bm["callback_data"])})
		}
	}
	return out
}

// waitAny waits for a screen after mark n whose text contains one of wants.
func (f *fakeTG) waitAny(n int, wants ...string) call {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range f.since(n) {
			if c.method != "sendMessage" && c.method != "editMessageText" {
				continue
			}
			for _, w := range wants {
				if strings.Contains(fmt.Sprint(c.p["text"]), w) {
					return c
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("no screen with %q; calls: %s", wants, f.dump(n))
	return call{}
}

// wizKey is the question on a wizard screen ("" = the review).
func wizKey(c call) string {
	for _, b := range buttons(c) {
		if k, ok := strings.CutPrefix(b[1], "wzb:"); ok {
			return k
		}
	}
	return ""
}

// wizRun answers the wizard on screen c. An answer is "=text" (typed), a
// label part of the button to press, or "+a,b" (switch these on, confirm).
// Questions without an answer get their first button (the suggestion);
// switch questions are confirmed as they are. It returns the review.
func (r *rig) wizRun(c call, answers map[string]string, seen map[string]bool) call {
	r.t.Helper()
	for i := 0; i < 80; i++ {
		k := wizKey(c)
		if k == "" {
			return c
		}
		if seen != nil {
			seen[k] = true
		}
		ans, has := answers[k]
		bs := buttons(c)
		n := r.tg.mark()
		switch {
		case strings.HasPrefix(ans, "="):
			r.tg.text(adminID, 900+n, ans[1:])
		case strings.HasPrefix(ans, "+"):
			for _, l := range strings.Split(ans[1:], ",") {
				m := r.tg.mark()
				r.tg.press(adminID, r.menu, pick(r.t, bs, "wzo:"+k+":", l))
				c = r.tg.waitAny(m, "مرحله")
				bs = buttons(c)
			}
			n = r.tg.mark()
			r.tg.press(adminID, r.menu, "wzn:"+k)
		default:
			var data string
			if has {
				data = pick(r.t, bs, "wzo:"+k+":", ans)
			} else {
				for _, b := range bs {
					if b[1] == "wzn:"+k {
						data = b[1]
					}
				}
				for _, b := range bs {
					if data != "" {
						break
					}
					if strings.HasPrefix(b[1], "wzo:"+k+":") {
						data = b[1]
						break
					}
					if b[1] == "wzn:"+k {
						data = b[1]
						break
					}
				}
			}
			if data == "" {
				r.t.Fatalf("question %s has no button and no answer: %v", k, c.p["text"])
			}
			r.tg.press(adminID, r.menu, data)
		}
		c = r.tg.waitAny(n, "مرحله", "بررسی نهایی", "مقدار ها را بررسی کنید")
		if strings.Contains(fmt.Sprint(c.p["text"]), "⚠️") {
			r.t.Fatalf("question %s refused %q: %v", k, ans, c.p["text"])
		}
	}
	r.t.Fatal("wizard did not end")
	return c
}

func pick(t *testing.T, bs [][2]string, prefix, label string) string {
	t.Helper()
	for _, b := range bs {
		if strings.HasPrefix(b[1], prefix) && strings.Contains(b[0], label) {
			return b[1]
		}
	}
	t.Fatalf("no %s button %q in %v", prefix, label, bs)
	return ""
}

// wizCreate makes an inbound through the bot wizard and returns it.
func (r *rig) wizCreate(proto string, answers map[string]string, seen map[string]bool) inbounds.View {
	r.t.Helper()
	r.press("in", "پروتکل")
	c := r.press("inp:"+proto, "مرحله")
	c = r.wizRun(c, answers, seen)
	if !hasButton(c, "ساختن ورودی") {
		r.t.Fatalf("no create button on the review: %v", c.p["text"])
	}
	n := r.tg.mark()
	r.tg.press(adminID, r.menu, "wzc")
	c = r.tg.waitAny(n, "👥", "انجام نشد")
	if strings.Contains(fmt.Sprint(c.p["text"]), "انجام نشد") {
		r.t.Fatalf("%s %v: %v", proto, answers, c.p["text"])
	}
	var list []inbounds.View
	st, raw := r.srv.Do(context.Background(), r.root(), "GET", "/inbounds", nil)
	if st != 200 {
		r.t.Fatalf("list %d", st)
	}
	_ = json.Unmarshal(raw, &list)
	var last inbounds.View
	for _, v := range list {
		if v.ID > last.ID {
			last = v
		}
	}
	return last
}

func newWizRig(t *testing.T) *rig {
	return newRigWith(t, func(srv *api.Server, _ *tgbot.Bot) {
		srv.Inbounds.SelfSigned = srv.Certs.SelfSigned
	})
}

type wizCase struct {
	proto   string
	answers map[string]string
	check   func(v inbounds.View) string
}

// TestInboundWizardAllCombinations creates, through the bot, an inbound of
// every protocol with every security and transport the shared table allows
// for it, then renders the Xray and sing-box configs; with VEILON_TEST_XRAY
// and VEILON_TEST_SINGBOX set the real cores must accept them.
func TestInboundWizardAllCombinations(t *testing.T) {
	r := newWizRig(t)
	if _, err := r.srv.Certs.SelfSigned("wiz.example.com"); err != nil {
		t.Fatal(err)
	}
	cp, kp, _ := certs.GenerateSelfSigned("naive.example.com")
	if _, err := r.srv.Certs.Upload("naive.example.com", cp, kp); err != nil {
		t.Fatal(err)
	}
	r.start()
	port := 20000
	next := func() string { port++; return fmt.Sprintf("=%d", port) }
	var cases []wizCase
	secLabel := map[string]string{"none": "بدون امنیت", "tls": "TLS", "reality": "Reality"}
	for _, p := range []string{"vless", "vmess", "trojan", "shadowsocks"} {
		for _, n := range inbounds.NetworksFor(p) {
			for _, s := range inbounds.SecuritiesFor(p, n) {
				p, n, s := p, n, s
				a := map[string]string{"port": next(), "net": n + " · ", "sec": secLabel[s], "sniff": "روشن"}
				if p == "vless" {
					a["enc"] = "خاموش"
				}
				if p == "shadowsocks" && s == "none" {
					a["ssm"] = "2022-blake3-aes-256-gcm"
				}
				if s == "tls" {
					a["alpn"] = "+h2,http/1.1"
					a["cert"] = "wiz.example.com"
				}
				if p == "vless" && inbounds.FlowAllowed(p, n, s, false) {
					a["flow"] = "xtls-rprx-vision"
				}
				cases = append(cases, wizCase{proto: p, answers: a, check: func(v inbounds.View) string {
					if v.Network != n || v.Security != s {
						return fmt.Sprintf("got %s/%s", v.Network, v.Security)
					}
					if s == "tls" && (v.TLS == nil || v.TLS.CertID == nil || strings.Join(v.TLS.ALPN, ",") != "h2,http/1.1" || !v.TLS.AllowInsecure || v.TLS.SNI != "wiz.example.com") {
						return fmt.Sprintf("tls %+v", v.TLS)
					}
					if s == "reality" && (v.Reality == nil || v.Reality.Dest != "www.microsoft.com:443" || len(v.Reality.ShortIDs) != 1) {
						return fmt.Sprintf("reality %+v", v.Reality)
					}
					if n == "grpc" && v.Transport.ServiceName != "grpc" || n == "ws" && v.Transport.Path != "/ws" || n == "xhttp" && v.Transport.Mode != "auto" {
						return fmt.Sprintf("transport %+v", v.Transport)
					}
					if p == "vless" && inbounds.FlowAllowed(p, n, s, false) && v.Flow != "xtls-rprx-vision" {
						return "flow " + v.Flow
					}
					return ""
				}})
			}
		}
	}
	// VLESS Encryption (ML-KEM, 1-RTT) over ws with Vision, a typed Reality
	// set, a new self-signed certificate made from the wizard.
	cases = append(cases,
		wizCase{proto: "vless", answers: map[string]string{"port": next(), "net": "tcp · ", "sec": "Reality", "enc": "روشن", "eauth": "X25519", "emode": "random", "ertt": "0-RTT", "eticket": "=900", "flow": "vision"},
			check: func(v inbounds.View) string {
				if v.VLESSEnc == nil || !v.VLESSEnc.Enabled || v.VLESSEnc.TicketSeconds != 900 || v.VLESSEnc.Mode != "random" || v.Flow != "xtls-rprx-vision" || v.Security != "reality" {
					return fmt.Sprintf("enc %+v flow %s", v.VLESSEnc, v.Flow)
				}
				return ""
			}},
		wizCase{proto: "vless", answers: map[string]string{"port": next(), "net": "ws · ", "sec": "بدون امنیت", "enc": "روشن", "eauth": "ML-KEM", "emode": "xorpub", "ertt": "1-RTT", "flow": "vision", "path": "=/v", "host": "=cdn.example.com"},
			check: func(v inbounds.View) string {
				if v.VLESSEnc == nil || !v.VLESSEnc.Enabled || v.VLESSEnc.Auth != "mlkem768" || v.VLESSEnc.Mode != "xorpub" || v.VLESSEnc.RTT != "1rtt" || v.Flow != "xtls-rprx-vision" || v.Transport.Path != "/v" || v.Transport.Host != "cdn.example.com" {
					return fmt.Sprintf("enc %+v flow %s %+v", v.VLESSEnc, v.Flow, v.Transport)
				}
				return ""
			}},
		wizCase{proto: "trojan", answers: map[string]string{"port": next(), "net": "grpc · ", "sec": "Reality", "dest": "=www.example.org", "sn": "=www.example.org, example.org", "sid": "=abcd, 0123456789abcdef", "rfp": "firefox", "spx": "=/s", "svc": "=svc1", "gmode": "multi", "listen": "=127.0.0.1"},
			check: func(v inbounds.View) string {
				r := v.Reality
				if r == nil || r.Dest != "www.example.org:443" || len(r.ServerNames) != 2 || len(r.ShortIDs) != 2 || r.Fingerprint != "firefox" || r.SpiderX != "/s" || v.Transport.ServiceName != "svc1" || v.Transport.Mode != "multi" || v.Listen != "127.0.0.1" {
					return fmt.Sprintf("%+v %+v %s", r, v.Transport, v.Listen)
				}
				return ""
			}},
		wizCase{proto: "vmess", answers: map[string]string{"port": next(), "net": "httpupgrade · ", "sec": "TLS", "cert": "ساختن گواهی خودامضا", "selfdom": "=self.example.net", "fp": "safari", "alpn": "+h3", "insecure": "روشن", "addr": "=edge.example.net"},
			check: func(v inbounds.View) string {
				if v.TLS == nil || v.TLS.SNI != "self.example.net" || v.TLS.Fingerprint != "safari" || strings.Join(v.TLS.ALPN, ",") != "h3" || v.AddressOverride != "edge.example.net" {
					return fmt.Sprintf("%+v %s", v.TLS, v.AddressOverride)
				}
				return ""
			}},
	)
	// The other protocols: each with its only or chosen security.
	cases = append(cases,
		wizCase{proto: "hysteria", answers: map[string]string{"port": next(), "obfs": "رمز تصادفی", "up": "=100", "down": "=200", "hop": "=30000-30100", "hopi": "=30", "cert": "خودامضای خودکار", "sni": "=hy.example.com", "fp": "chrome"},
			check: func(v inbounds.View) string {
				s := v.Settings
				if s.ObfsPassword == "" || s.UpMbps != 100 || s.DownMbps != 200 || s.HopPorts != "30000-30100" || s.HopInterval != 30 || v.TLS == nil || v.TLS.CertID == nil || v.TLS.SNI != "hy.example.com" {
					return fmt.Sprintf("%+v %+v", s, v.TLS)
				}
				return ""
			}},
		wizCase{proto: "tuic", answers: map[string]string{"port": next(), "cong": "cubic", "cert": "wiz.example.com", "insecure": "روشن"},
			check: func(v inbounds.View) string {
				if v.Settings.Congestion != "cubic" || !v.TLS.AllowInsecure {
					return fmt.Sprintf("%+v", v.Settings)
				}
				return ""
			}},
		wizCase{proto: "masque", answers: map[string]string{"port": next(), "alpn": "+h3,h2", "cert": "خودامضای خودکار"},
			check: func(v inbounds.View) string {
				if strings.Join(v.TLS.ALPN, ",") != "h3,h2" || v.Settings.MTU != 1280 {
					return fmt.Sprintf("%+v %+v", v.TLS, v.Settings)
				}
				return ""
			}},
		wizCase{proto: "anytls", answers: map[string]string{"port": next(), "pad": "=stop=2\n0=30-30\n1=100-200", "cert": "خودامضای خودکار"},
			check: func(v inbounds.View) string {
				if len(v.Settings.PaddingScheme) != 3 {
					return fmt.Sprintf("%+v", v.Settings.PaddingScheme)
				}
				return ""
			}},
		wizCase{proto: "naive", answers: map[string]string{"port": next(), "cert": "naive.example.com"},
			check: func(v inbounds.View) string {
				if v.TLS == nil || v.TLS.SNI != "naive.example.com" {
					return fmt.Sprintf("%+v", v.TLS)
				}
				return ""
			}},
		wizCase{proto: "shadowtls", answers: map[string]string{"port": next(), "hs": "=www.apple.com", "hsp": "=8443", "stm": "256"},
			check: func(v inbounds.View) string {
				if v.Settings.HandshakeServer != "www.apple.com" || v.Settings.HandshakePort != 8443 || v.SSMethod != "2022-blake3-aes-256-gcm" {
					return fmt.Sprintf("%+v %s", v.Settings, v.SSMethod)
				}
				return ""
			}},
		wizCase{proto: "http", answers: map[string]string{"port": next(), "sec": "HTTP ("}, check: func(v inbounds.View) string {
			if v.Security != "none" {
				return v.Security
			}
			return ""
		}},
		wizCase{proto: "http", answers: map[string]string{"port": next(), "sec": "HTTPS", "cert": "wiz.example.com", "sni": "=proxy.example.com"}, check: func(v inbounds.View) string {
			if v.Security != "tls" || v.TLS == nil || v.TLS.CertID == nil {
				return v.Security
			}
			return ""
		}},
		wizCase{proto: "mixed", answers: map[string]string{"port": next(), "udp": "خاموش"}, check: func(v inbounds.View) string {
			if v.Settings.UDP {
				return "udp on"
			}
			return ""
		}},
		wizCase{proto: "socks", answers: map[string]string{"port": next(), "udp": "روشن", "auth": "خاموش"}, check: func(v inbounds.View) string {
			if !v.Settings.UDP || !v.Settings.NoAuth {
				return fmt.Sprintf("%+v", v.Settings)
			}
			return ""
		}},
		wizCase{proto: "tunnel", answers: map[string]string{"port": next(), "taddr": "=1.1.1.1:53", "tnet": "TCP,UDP"}, check: func(v inbounds.View) string {
			if v.Settings.TargetAddress != "1.1.1.1" || v.Settings.TargetPort != 53 || v.Settings.TunnelNetwork != "tcp,udp" {
				return fmt.Sprintf("%+v", v.Settings)
			}
			return ""
		}},
		wizCase{proto: "mtproto", answers: map[string]string{"port": next(), "ftls": "=www.google.com"}, check: func(v inbounds.View) string {
			if v.Settings.FakeTLSDomain != "www.google.com" {
				return v.Settings.FakeTLSDomain
			}
			return ""
		}},
		wizCase{proto: "wireguard", answers: map[string]string{"port": next(), "wgaddr": "=10.66.66.1/24", "dns": "بدون DNS", "wgmtu": "=1380", "ka": "خاموش"}, check: func(v inbounds.View) string {
			s := v.Settings
			if s.Address != "10.66.66.1/24" || len(s.DNS) != 0 || s.MTU != 1380 || s.Keepalive != 0 || s.PublicKey == "" {
				return fmt.Sprintf("%+v", s)
			}
			return ""
		}},
		wizCase{proto: "amneziawg", answers: map[string]string{"port": next(), "awg": "=4 40 70 20 30 101 202 303 404"}, check: func(v inbounds.View) string {
			if a := v.Settings.AWG; a == nil || a.Jc != 4 || a.S2 != 30 || a.H4 != 404 {
				return fmt.Sprintf("%+v", v.Settings.AWG)
			}
			return ""
		}},
		wizCase{proto: "tun", answers: map[string]string{"tunname": "=wiztun0", "routes": "=10.10.0.0/16"}, check: func(v inbounds.View) string {
			if v.Settings.TunName != "wiztun0" || len(v.Settings.Routes) != 1 {
				return fmt.Sprintf("%+v", v.Settings)
			}
			return ""
		}},
	)
	protos := map[string]bool{}
	for _, c := range cases {
		protos[c.proto] = true
	}
	for _, p := range inbounds.Protocols {
		if !protos[p] {
			t.Errorf("protocol %s not covered", p)
		}
	}
	seen := map[string]bool{}
	for i, c := range cases {
		v := r.wizCreate(c.proto, c.answers, seen)
		if v.Protocol != c.proto {
			t.Fatalf("case %d: created %s, want %s", i, v.Protocol, c.proto)
		}
		if msg := c.check(v); msg != "" {
			t.Errorf("case %d %s %v: %s", i, c.proto, c.answers, msg)
		}
	}
	// Every question of a new inbound was asked at least once.
	for _, k := range []string{"remark", "port", "listen", "net", "path", "host", "xmode", "svc", "gmode", "ssm", "obfs", "up", "down", "hop", "hopi", "cong",
		"pool", "mmtu", "mpath", "hs", "hsp", "stm", "pad", "wgaddr", "dns", "wgmtu", "ka", "wgkey", "awg", "tunname", "tunmtu", "gw", "routes", "udp", "auth",
		"taddr", "tnet", "ftls", "sec", "cert", "selfdom", "sni", "fp", "alpn", "insecure", "dest", "sn", "sid", "rfp", "spx", "enc", "eauth", "emode", "ertt", "eticket", "flow", "sniff", "addr"} {
		if !seen[k] {
			t.Errorf("question %s never asked", k)
		}
	}
	r.coresAccept(len(cases) + 1)
}

// coresAccept renders the configs of every inbound the way the panel does;
// with VEILON_TEST_XRAY / VEILON_TEST_SINGBOX the real cores check them.
func (r *rig) coresAccept(want int) {
	r.t.Helper()
	dir := r.srv.Certs.DataDir
	// One user on every inbound, so each one has a client to serve.
	var ids []int64
	_, raw := r.srv.Do(context.Background(), r.root(), "GET", "/inbounds", nil)
	var list []inbounds.View
	_ = json.Unmarshal(raw, &list)
	for _, v := range list {
		ids = append(ids, v.ID)
	}
	if st, raw := r.srv.Do(context.Background(), r.root(), "POST", "/users", map[string]any{"username": fmt.Sprintf("wiz%d", len(ids)), "inbound_ids": ids}); st != 201 {
		r.t.Fatalf("user %d %s", st, raw)
	}
	in, err := xray.LoadInput(r.gdb, dir, 42099)
	if err != nil {
		r.t.Fatal(err)
	}
	if len(in.Inbounds) != want {
		r.t.Fatalf("%d inbounds, want %d", len(in.Inbounds), want)
	}
	in.TunOK = true
	b, err := xray.Build(in)
	if err != nil {
		r.t.Fatal(err)
	}
	if len(in.Skipped) > 0 {
		r.t.Fatalf("left out: %v", in.Skipped)
	}
	var cfg struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	_ = json.Unmarshal(b, &cfg)
	xrayN := 0
	for _, ib := range in.Inbounds {
		if inbounds.Engine(ib.Protocol) == inbounds.EngineXray {
			xrayN++
		}
	}
	tags := map[string]bool{}
	for _, x := range cfg.Inbounds {
		tags[fmt.Sprint(x["tag"])] = true
	}
	for _, ib := range in.Inbounds {
		if inbounds.Engine(ib.Protocol) == inbounds.EngineXray && !tags[ib.Tag()] {
			r.t.Errorf("inbound %s (%s) not in the Xray config", ib.Remark, ib.Protocol)
		}
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		// TUN needs root; the rest of the config is checked without it.
		in.TunOK = false
		b, _ = xray.Build(in)
		f := filepath.Join(r.t.TempDir(), "xray.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
			r.t.Fatalf("xray -test: %v\n%s", err, out)
		}
		r.t.Logf("xray -test OK (%d Xray inbounds)", xrayN)
	}
	sb, err := cores.SingboxConfig(in, 30555)
	if err != nil {
		r.t.Fatal(err)
	}
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" && sb != nil {
		f := filepath.Join(r.t.TempDir(), "sb.json")
		_ = os.WriteFile(f, sb, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
			r.t.Fatalf("sing-box check: %v\n%s", err, out)
		}
		r.t.Log("sing-box check OK")
	}
	for _, ib := range in.Inbounds {
		ib := ib
		if ib.Protocol == inbounds.MTProto && len(cores.MTGConfig(&ib, in, 3129)) == 0 {
			r.t.Error("empty mtg config")
		}
	}
}

// Back goes to the previous question with its answer kept; the review
// changes one value; cancel leaves nothing behind.
func TestInboundWizardBackAndReview(t *testing.T) {
	r := newWizRig(t)
	if _, err := r.srv.Certs.SelfSigned("wiz.example.com"); err != nil {
		t.Fatal(err)
	}
	r.start()
	r.press("in", "پروتکل")
	c := r.press("inp:trojan", "نام ورودی")
	if hasButton(c, "Reality") {
		t.Fatal("a later question shown first")
	}
	c = r.typeText("TR1", "پورت")
	c = r.typeText("abc", "⚠️")
	if wizKey(c) != "port" {
		t.Fatalf("bad port moved on: %v", c.p["text"])
	}
	c = r.typeText("31001", "آدرس گوش دادن")
	c = r.press("wzb:listen", "پورت")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "31001") {
		t.Fatalf("back lost the answer: %v", c.p["text"])
	}
	c = r.typeText("31002", "آدرس گوش دادن")
	c = r.wizRun(c, map[string]string{"net": "kcp · ", "sec": "TLS"}, nil)
	if hasButton(c, "Reality") || !strings.Contains(fmt.Sprint(c.p["text"]), "kcp") {
		t.Fatalf("review %v", c.p["text"])
	}
	// Reality is not offered over mKCP.
	c = r.press("wzj:sec", "امنیت")
	if hasButton(c, "Reality") {
		t.Fatal("reality offered over kcp")
	}
	// Moving to RAW opens Reality; choosing it asks its questions, then
	// comes back to the review.
	c = r.press("wzj:net", "نوع انتقال")
	c = r.press(pick(t, buttons(c), "wzo:net:", "tcp · "), "بررسی نهایی")
	// TLS still works on RAW: not asked again until chosen from the review.
	c = r.press("wzj:sec", "امنیت")
	if !hasButton(c, "✅ 🔒 TLS") {
		t.Fatalf("current answer not marked: %v", c.p["reply_markup"])
	}
	c = r.press(pick(t, buttons(c), "wzo:sec:", "Reality"), "مرحله")
	c = r.wizRun(c, nil, nil)
	if !strings.Contains(fmt.Sprint(c.p["text"]), "www.microsoft.com:443") {
		t.Fatalf("review %v", c.p["text"])
	}
	n := r.tg.mark()
	r.tg.press(adminID, r.menu, "wzc")
	r.tg.waitAny(n, "👥")
	var list []inbounds.View
	_, raw := r.srv.Do(context.Background(), r.root(), "GET", "/inbounds", nil)
	_ = json.Unmarshal(raw, &list)
	got := list[len(list)-1]
	if got.Remark != "TR1" || got.Port != 31002 || got.Network != "tcp" || got.Security != "reality" {
		t.Fatalf("%+v", got)
	}
	// Cancel: nothing is made.
	r.press("in", "پروتکل")
	r.press("inp:vmess", "نام ورودی")
	r.press("il", "ورودی ها")
	_, raw = r.srv.Do(context.Background(), r.root(), "GET", "/inbounds", nil)
	_ = json.Unmarshal(raw, &list)
	if len(list) != 2 {
		t.Fatalf("%d inbounds", len(list))
	}
}

// The security and transport of an existing inbound: VLESS Reality on RAW
// moves to WebSocket with TLS (Reality is asked again because it does not
// work there), then to gRPC with Reality; the config still passes.
func TestInboundWizardEdit(t *testing.T) {
	r := newWizRig(t)
	if _, err := r.srv.Certs.SelfSigned("wiz.example.com"); err != nil {
		t.Fatal(err)
	}
	r.start()
	id := r.ib
	c := r.press(fmt.Sprintf("iv:%d", id), "امنیت")
	c = r.press(button(t, c, "امنیت و انتقال"), "مقدار ها را بررسی کنید")
	if !hasButton(c, "مقصد Reality") || hasButton(c, "مسیر") {
		t.Fatalf("edit review %v", c.p["reply_markup"])
	}
	c = r.press("wzj:net", "نوع انتقال")
	c = r.press(pick(t, buttons(c), "wzo:net:", "ws · "), "مرحله")
	seen := map[string]bool{}
	c = r.wizRun(c, map[string]string{"path": "=/edit", "sec": "TLS"}, seen)
	for _, k := range []string{"path", "host", "sec", "cert", "sni", "fp", "alpn", "insecure"} {
		if !seen[k] {
			t.Errorf("%s not asked after the change", k)
		}
	}
	n := r.tg.mark()
	r.tg.press(adminID, r.menu, "wzc")
	c = r.tg.waitAny(n, "👥", "انجام نشد")
	if strings.Contains(fmt.Sprint(c.p["text"]), "انجام نشد") {
		t.Fatalf("save: %v", c.p["text"])
	}
	var v inbounds.View
	_, raw := r.srv.Do(context.Background(), r.root(), "GET", fmt.Sprintf("/inbounds/%d", id), nil)
	_ = json.Unmarshal(raw, &v)
	if v.Network != "ws" || v.Security != "tls" || v.Transport.Path != "/edit" || v.Flow != "" || v.Remark != "VL" || v.Port != 4443 {
		t.Fatalf("after edit %+v", v)
	}
	// Back to Reality over gRPC.
	r.press(fmt.Sprintf("wze:%d", id), "مقدار ها را بررسی کنید")
	c = r.press("wzj:net", "نوع انتقال")
	c = r.press(pick(t, buttons(c), "wzo:net:", "grpc · "), "مرحله")
	c = r.wizRun(c, nil, nil)
	c = r.press("wzj:sec", "امنیت")
	c = r.press(pick(t, buttons(c), "wzo:sec:", "Reality"), "مرحله")
	c = r.wizRun(c, nil, nil)
	n = r.tg.mark()
	r.tg.press(adminID, r.menu, "wzc")
	r.tg.waitAny(n, "👥")
	_, raw = r.srv.Do(context.Background(), r.root(), "GET", fmt.Sprintf("/inbounds/%d", id), nil)
	v = inbounds.View{}
	_ = json.Unmarshal(raw, &v)
	if v.Network != "grpc" || v.Security != "reality" || v.Reality == nil || v.Transport.ServiceName != "grpc" {
		t.Fatalf("after second edit %+v", v)
	}
	// Leaving the edit with back changes nothing.
	r.press(fmt.Sprintf("wze:%d", id), "مقدار ها را بررسی کنید")
	r.press("wzb", "👥")
	r.coresAccept(1)
}
