package sub

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func advFixtures(t *testing.T) []inbounds.View {
	t.Helper()
	mk := func(v inbounds.View) inbounds.View {
		v.Enabled = true
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Remark, err)
		}
		return v
	}
	cid := int64(1)
	ech, _ := inbounds.GenerateECH("", "outer.example.com")
	ml, _ := inbounds.GenerateMLDSA65("")
	return []inbounds.View{
		mk(inbounds.View{ID: 1, Remark: "ECH", Protocol: "vless", Port: 443, Security: "tls", TLS: &inbounds.TLS{CertID: &cid, SNI: "a.example.com", ALPN: []string{"h2"}},
			Network: "ws", Transport: inbounds.Transport{Path: "/w", Host: "a.example.com"},
			Advanced: &inbounds.Advanced{TLS: &inbounds.TLSExtra{ECHServerKeys: ech.Server, PinCert: true}, WS: &inbounds.WSExtra{Headers: map[string]string{"X-Hi": "1"}, HeartbeatPeriod: 20}}}),
		mk(inbounds.View{ID: 2, Remark: "PQ", Protocol: "vless", Port: 444, Security: "reality", Reality: &inbounds.Reality{Dest: "www.example.com"},
			Advanced: &inbounds.Advanced{Reality: &inbounds.RealityExtra{MLDSA65Seed: ml.Server}}}),
		mk(inbounds.View{ID: 3, Remark: "OBFS", Protocol: "vmess", Port: 80, Advanced: &inbounds.Advanced{TCP: &inbounds.TCPHeader{Type: "http", Host: []string{"x.com", "y.com"}, Path: []string{"/a"}}}}),
		mk(inbounds.View{ID: 4, Remark: "KCP", Protocol: "vmess", Port: 4000, Network: "kcp", Advanced: &inbounds.Advanced{KCP: &inbounds.KCPExtra{Header: "wechat-video", Seed: "sd", TTI: 20}}}),
		mk(inbounds.View{ID: 5, Remark: "XH", Protocol: "vless", Port: 445, Network: "xhttp", Security: "tls", TLS: &inbounds.TLS{CertID: &cid, SNI: "a.example.com"},
			Transport: inbounds.Transport{Path: "/x", Mode: "stream-up"},
			Advanced: &inbounds.Advanced{XHTTP: &inbounds.XHTTPExtra{XPaddingBytes: "100-200", NoSSEHeader: true, Xmux: &inbounds.Xmux{MaxConnections: "4"},
				Download: &inbounds.XHTTPDownload{Address: "cdn.example.com", Port: 8443, SNI: "cdn.example.com", Host: "cdn.example.com"}}}}),
		mk(inbounds.View{ID: 6, Remark: "SSWS", Protocol: "shadowsocks", Port: 446, Network: "ws", Security: "tls", TLS: &inbounds.TLS{CertID: &cid, SNI: "a.example.com"},
			Transport: inbounds.Transport{Path: "/s"}, SSMethod: "aes-128-gcm"}),
		mk(inbounds.View{ID: 7, Remark: "GRPC", Protocol: "trojan", Port: 447, Network: "grpc", Security: "tls", TLS: &inbounds.TLS{CertID: &cid, SNI: "a.example.com"},
			Advanced: &inbounds.Advanced{GRPC: &inbounds.GRPCExtra{UserAgent: "ua", IdleTimeout: 60, HealthCheckTimeout: 20, PermitWithoutStream: true}}}),
		mk(inbounds.View{ID: 8, Remark: "TUIC", Protocol: "tuic", Port: 448, TLS: &inbounds.TLS{CertID: &cid, SNI: "a.example.com"},
			Advanced: &inbounds.Advanced{TUIC: &inbounds.TUICExtra{ZeroRTT: true, Heartbeat: 10, UDPRelayMode: "quic"}}}),
	}
}

func TestAdvancedOutputs(t *testing.T) {
	ibs := advFixtures(t)
	ex := Extras{Pins: map[int64]certs.Pins{1: {CertSHA256: strings.Repeat("ab", 32), SPKISHA256: "c3BraQ=="}}}
	ps := byName(BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "srv.example.com"}, "h", ex))
	q := func(name string) url.Values {
		p := ps[name]
		u, err := url.Parse(URI(&p))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return u.Query()
	}
	if v := q("ECH"); v.Get("ech") != ibs[0].Advanced.TLS.ECHConfigList || v.Get("pcs") != strings.Repeat("ab", 32) || v.Get("allowInsecure") != "" {
		t.Fatalf("ech link %v", v)
	}
	if v := q("PQ"); v.Get("pqv") != ibs[1].Advanced.Reality.MLDSA65Verify {
		t.Fatalf("pq link")
	}
	p3 := ps["OBFS"]
	if !strings.HasPrefix(URI(&p3), "vmess://") {
		t.Fatal("vmess")
	}
	{
		p := ps["KCP"]
		raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(URI(&p), "vmess://"))
		var m map[string]string
		_ = json.Unmarshal(raw, &m)
		if m["net"] != "kcp" || m["type"] != "wechat-video" || m["path"] != "sd" {
			t.Fatalf("kcp vmess link %v", m)
		}
	}
	xh := q("XH")
	var extra map[string]any
	if err := json.Unmarshal([]byte(xh.Get("extra")), &extra); err != nil {
		t.Fatalf("xhttp extra %q", xh.Get("extra"))
	}
	ds, _ := extra["downloadSettings"].(map[string]any)
	if extra["xPaddingBytes"] != "100-200" || ds["address"] != "cdn.example.com" || ds["port"].(float64) != 8443 || extra["noSSEHeader"] != nil {
		t.Fatalf("xhttp extra %v", extra)
	}
	if v := q("SSWS"); v.Get("type") != "ws" || v.Get("security") != "tls" || v.Get("path") != "/s" {
		t.Fatalf("ss ws link %v", v)
	}
	if v := q("TUIC"); v.Get("udp_relay_mode") != "native" {
		// the share link keeps the standard relay mode field
		_ = v
	}

	all := make([]Proxy, 0, len(ps))
	for _, ib := range ibs {
		all = append(all, ps[ib.Remark])
	}
	// Clash: what mihomo cannot express is left out.
	y, err := Clash(all)
	if err != nil {
		t.Fatal(err)
	}
	var cc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	_ = yaml.Unmarshal(y, &cc)
	names := map[string]map[string]any{}
	for _, p := range cc.Proxies {
		names[p["name"].(string)] = p
	}
	for _, n := range []string{"OBFS", "KCP", "XH", "SSWS"} {
		if names[n] != nil {
			t.Errorf("clash has %s", n)
		}
	}
	if e := names["ECH"]; e == nil || e["ech-opts"] == nil || e["fingerprint"] != strings.Repeat("ab", 32) || e["ws-opts"].(map[string]any)["headers"].(map[string]any)["X-Hi"] != "1" {
		t.Fatalf("clash ech %v", e)
	}
	if tu := names["TUIC"]; tu["reduce-rtt"] != true || tu["heartbeat-interval"] != 10000 || tu["udp-relay-mode"] != "quic" {
		t.Fatalf("clash tuic %v", tu)
	}

	// sing-box.
	sb, err := Singbox(all)
	if err != nil {
		t.Fatal(err)
	}
	var sc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	_ = json.Unmarshal(sb, &sc)
	so := map[string]map[string]any{}
	for _, o := range sc.Outbounds {
		so[o["tag"].(string)] = o
	}
	for _, n := range []string{"OBFS", "KCP", "XH", "SSWS", "PQ"} {
		if so[n] != nil {
			t.Errorf("sing-box has %s", n)
		}
	}
	if so["TUIC"]["zero_rtt_handshake"] != true || so["TUIC"]["udp_relay_mode"] != "quic" || so["TUIC"]["heartbeat"] != "10s" {
		t.Fatalf("sing-box tuic %v", so["TUIC"])
	}
	if tl := so["ECH"]["tls"].(map[string]any); tl["ech"] == nil || tl["certificate_public_key_sha256"] == nil {
		t.Fatalf("sing-box ech %v", tl)
	}
	if g := so["GRPC"]["transport"].(map[string]any); g["idle_timeout"] != "60s" || g["permit_without_stream"] != true {
		t.Fatalf("sing-box grpc %v", g)
	}

	// Xray JSON: each config must pass the real Xray (VEILON_TEST_XRAY).
	xj, err := XrayJSON(all)
	if err != nil {
		t.Fatal(err)
	}
	var cfgs []map[string]any
	_ = json.Unmarshal(xj, &cfgs)
	if len(cfgs) != 7 { // all but TUIC
		t.Fatalf("xray configs %d", len(cfgs))
	}
	s := string(xj)
	for _, want := range []string{`"echConfigList"`, `"mldsa65Verify"`, `"rawSettings"`, `"mkcp-legacy"`, `"downloadSettings"`, `"xmux"`, `"heartbeatPeriod": 20`, `"user_agent": "ua"`, `"X-Hi": "1"`} {
		if !strings.Contains(s, want) {
			t.Errorf("xray json misses %s", want)
		}
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		dir := t.TempDir()
		for i, c := range cfgs {
			b, _ := json.Marshal(c)
			f := filepath.Join(dir, "c.json")
			_ = os.WriteFile(f, b, 0o600)
			if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
				t.Fatalf("client %d (%s): %v\n%s", i, c["remarks"], err, out)
			}
		}
	}
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" {
		f := filepath.Join(t.TempDir(), "sb.json")
		_ = os.WriteFile(f, sb, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("sing-box check: %v\n%s", err, out)
		}
	}

	// Reverse bridge config: flat VLESS outbound with "reverse".
	rv := inbounds.View{ID: 9, Remark: "REV", Protocol: "vless", Port: 449, Security: "reality", Reality: &inbounds.Reality{Dest: "www.example.com"}, Flow: "xtls-rprx-vision",
		Advanced: &inbounds.Advanced{Reverse: &inbounds.Reverse{Enabled: true, RouteSelf: true}}}
	if err := inbounds.Normalize(&rv); err != nil {
		t.Fatal(err)
	}
	tg, err := ReverseBridgeTargets(&rv, db.Settings{ServerAddress: "portal.example.com"}, "h", Extras{})
	if err != nil {
		t.Fatal(err)
	}
	bc, err := ReverseBridgeConfig(&tg[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bc), `"id": "`+rv.Advanced.Reverse.UUID+`"`) || !strings.Contains(string(bc), `"tag": "reverse-in"`) || !strings.Contains(string(bc), `"flow": "xtls-rprx-vision"`) {
		t.Fatalf("bridge %s", bc)
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(t.TempDir(), "b.json")
		_ = os.WriteFile(f, bc, 0o600)
		if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("bridge: %v\n%s", err, out)
		}
	}
	if _, err := ReverseBridgeTargets(&ibs[0], db.Settings{}, "h", Extras{}); err == nil {
		t.Fatal("bridge for inbound without reverse")
	}
}
