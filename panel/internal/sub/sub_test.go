package sub

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func user() *db.User {
	return &db.User{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "trojanpass", SubToken: "tok",
		DataLimit: 1000, Up: 10, Down: 20}
}

func certID(i int64) *int64 { return &i }

func fixtures() []inbounds.View {
	return []inbounds.View{
		{ID: 1, Remark: "VL Reality", Protocol: "vless", Port: 443, Network: "tcp", Security: "reality", Flow: "xtls-rprx-vision",
			Reality: &inbounds.Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com", "example.com"},
				PublicKey: "PUBKEY", ShortIDs: []string{"abcd", "ef"}, Fingerprint: "firefox", SpiderX: "/"}},
		{ID: 2, Remark: "VM WS TLS", Protocol: "vmess", Port: 8443, Network: "ws", Security: "tls",
			Transport: inbounds.Transport{Path: "/ws", Host: "cdn.example.com"},
			TLS:       &inbounds.TLS{CertID: certID(1), SNI: "cdn.example.com", ALPN: []string{"h2", "http/1.1"}, Fingerprint: "chrome"}},
		{ID: 3, Remark: "TR gRPC", Protocol: "trojan", Port: 2083, Network: "grpc", Security: "tls",
			Transport: inbounds.Transport{ServiceName: "svc"}, TLS: &inbounds.TLS{CertID: certID(1), SNI: "t.example.com", AllowInsecure: true}},
		{ID: 4, Remark: "SS", Protocol: "shadowsocks", Port: 8388, Network: "tcp", Security: "none", SSMethod: "chacha20-ietf-poly1305"},
		{ID: 5, Remark: "SS22", Protocol: "shadowsocks", Port: 8389, Network: "tcp", Security: "none", SSMethod: "2022-blake3-aes-128-gcm",
			SSServerKey: "AAAAAAAAAAAAAAAAAAAAAA=="},
		{ID: 6, Remark: "VL XHTTP", Protocol: "vless", Port: 80, Network: "xhttp", Security: "none",
			Transport: inbounds.Transport{Path: "/x", Mode: "auto"}, AddressOverride: "edge.example.net"},
		{ID: 7, Remark: "VL HU", Protocol: "vless", Port: 81, Network: "httpupgrade", Security: "none",
			Transport: inbounds.Transport{Path: "/hu", Host: "h.example.com"}},
	}
}

func proxies() []Proxy {
	return BuildProxies(user(), fixtures(), db.Settings{ServerAddress: "1.2.3.4"}, "panel.local:9999", map[int64]string{1: "abcdef"})
}

func TestVLESSRealityURI(t *testing.T) {
	ps := proxies()
	u, err := url.Parse(URI(&ps[0]))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "vless" || u.User.Username() != user().UUID || u.Host != "1.2.3.4:443" || u.Fragment != "VL Reality" {
		t.Fatalf("bad uri %s", u)
	}
	q := u.Query()
	want := map[string]string{"type": "tcp", "security": "reality", "encryption": "none", "pbk": "PUBKEY", "sid": "abcd",
		"sni": "www.example.com", "fp": "firefox", "spx": "/", "flow": "xtls-rprx-vision"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s=%q want %q", k, q.Get(k), v)
		}
	}
}

func TestVMessURI(t *testing.T) {
	ps := proxies()
	s := URI(&ps[1])
	if !strings.HasPrefix(s, "vmess://") {
		t.Fatal(s)
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "vmess://"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"v": "2", "ps": "VM WS TLS", "add": "1.2.3.4", "port": "8443", "id": user().UUID, "aid": "0",
		"net": "ws", "path": "/ws", "host": "cdn.example.com", "tls": "tls", "sni": "cdn.example.com", "alpn": "h2,http/1.1", "fp": "chrome"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s=%q want %q", k, m[k], v)
		}
	}
}

func TestTrojanURI(t *testing.T) {
	ps := proxies()
	u, _ := url.Parse(URI(&ps[2]))
	q := u.Query()
	if u.Scheme != "trojan" || u.User.Username() != "trojanpass" || q.Get("type") != "grpc" || q.Get("serviceName") != "svc" ||
		q.Get("mode") != "gun" || q.Get("security") != "tls" || q.Get("sni") != "t.example.com" || q.Get("allowInsecure") != "1" ||
		q.Get("pcs") != "abcdef" {
		t.Fatalf("bad trojan uri %s", u)
	}
}

func TestShadowsocksURIs(t *testing.T) {
	ps := proxies()
	s := URI(&ps[3])
	u, _ := url.Parse(s)
	dec, err := base64.RawURLEncoding.DecodeString(u.User.Username())
	if err != nil || string(dec) != "chacha20-ietf-poly1305:trojanpass" || u.Host != "1.2.3.4:8388" {
		t.Fatalf("bad ss uri %s (%s)", s, dec)
	}
	s22 := URI(&ps[4])
	u22, err := url.Parse(s22)
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u22.User.Password()
	if u22.User.Username() != "2022-blake3-aes-128-gcm" || !strings.HasPrefix(pw, "AAAAAAAAAAAAAAAAAAAAAA==:") {
		t.Fatalf("bad ss2022 uri %s", s22)
	}
	userKey := strings.SplitN(pw, ":", 2)[1]
	if k, err := base64.StdEncoding.DecodeString(userKey); err != nil || len(k) != 16 {
		t.Fatalf("bad ss2022 user key %q", userKey)
	}
}

func TestAddressOverrideAndXHTTP(t *testing.T) {
	ps := proxies()
	u, _ := url.Parse(URI(&ps[5]))
	q := u.Query()
	if u.Hostname() != "edge.example.net" || q.Get("type") != "xhttp" || q.Get("path") != "/x" || q.Get("mode") != "auto" {
		t.Fatalf("bad xhttp uri %s", u)
	}
	// Fallback to request host when no server address configured.
	p2 := BuildProxies(user(), fixtures()[:1], db.Settings{}, "panel.local:9999", nil)
	if p2[0].Server != "panel.local" {
		t.Fatalf("server fallback %q", p2[0].Server)
	}
}

func TestV2RayBase64(t *testing.T) {
	body := V2Ray(proxies())
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 7 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, p := range []string{"vless://", "vmess://", "trojan://", "ss://", "ss://", "vless://", "vless://"} {
		if !strings.HasPrefix(lines[0], p) {
			t.Fatalf("line %q missing prefix %s", lines[0], p)
		}
		lines = lines[1:]
	}
}

func TestSingbox(t *testing.T) {
	b, err := Singbox(proxies())
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	byTag := map[string]map[string]any{}
	for _, o := range cfg.Outbounds {
		byTag[o["tag"].(string)] = o
	}
	if _, ok := byTag["VL XHTTP"]; ok {
		t.Fatal("xhttp should be skipped for sing-box")
	}
	if _, ok := byTag["VL Reality"]; ok {
		t.Fatal("reality should be skipped for sing-box")
	}
	if byTag["VL HU"]["transport"].(map[string]any)["type"] != "httpupgrade" {
		t.Fatal("httpupgrade transport missing")
	}
	if byTag["SS22"]["password"] != "AAAAAAAAAAAAAAAAAAAAAA==:"+strings.SplitN(proxies()[4].Password, ":", 2)[1] {
		t.Fatal("ss2022 password wrong")
	}
	sel := byTag["proxy"]["outbounds"].([]any)
	if sel[0] != "auto" || len(sel) != 6 { // auto + 5 supported
		t.Fatalf("selector %v", sel)
	}
}

func TestClash(t *testing.T) {
	b, err := Clash(proxies())
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Proxies     []map[string]any `yaml:"proxies"`
		ProxyGroups []map[string]any `yaml:"proxy-groups"`
		Rules       []string         `yaml:"rules"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Proxies) != 6 {
		t.Fatalf("want 6 proxies, got %d", len(cfg.Proxies))
	}
	vl := cfg.Proxies[0]
	ro := vl["reality-opts"].(map[string]any)
	if vl["type"] != "vless" || vl["tls"] != true || vl["servername"] != "www.example.com" || ro["public-key"] != "PUBKEY" || ro["support-x25519mlkem768"] != true || vl["client-fingerprint"] != "chrome" {
		t.Fatalf("bad vless %v", vl)
	}
	tr := cfg.Proxies[2]
	if tr["type"] != "trojan" || tr["sni"] != "t.example.com" || tr["network"] != "grpc" || tr["skip-cert-verify"] != true {
		t.Fatalf("bad trojan %v", tr)
	}
	hu := cfg.Proxies[5]
	if hu["network"] != "ws" || hu["ws-opts"].(map[string]any)["v2ray-http-upgrade"] != true {
		t.Fatalf("bad httpupgrade %v", hu)
	}
	if cfg.Rules[len(cfg.Rules)-1] != "MATCH,Proxy" {
		t.Fatal("missing MATCH rule")
	}
}

func TestXrayJSON(t *testing.T) {
	b, err := XrayJSON(proxies())
	if err != nil {
		t.Fatal(err)
	}
	var cs []map[string]any
	if err := json.Unmarshal(b, &cs); err != nil || len(cs) != 7 {
		t.Fatalf("%v %d", err, len(cs))
	}
	tls := cs[2]["outbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
	if _, bad := tls["allowInsecure"]; bad || tls["pinnedPeerCertSha256"] != "abcdef" {
		t.Fatalf("xray json tls must pin instead of allowInsecure: %v", tls)
	}
}

func TestDetectFormat(t *testing.T) {
	cases := map[string]string{
		"ClashMeta/1.18":           "clash",
		"mihomo/1.19.0":            "clash",
		"clash-verge/v1.3":         "clash",
		"Stash/2.4":                "clash",
		"sing-box 1.10.1":          "singbox",
		"SFA/1.9.0 (Android)":      "singbox",
		"SFI/1.9.0":                "singbox",
		"v2rayNG/1.8.5":            "v2ray",
		"Streisand/1.5":            "v2ray",
		"Mozilla/5.0 (Windows NT)": "v2ray",
		"":                         "v2ray",
	}
	for ua, want := range cases {
		if got := DetectFormat(ua); got != want {
			t.Errorf("%q -> %s want %s", ua, got, want)
		}
	}
}

func TestSubURL(t *testing.T) {
	id := int64(1)
	cases := []struct {
		s    db.Settings
		host string
		want string
	}{
		{db.Settings{SubPath: "sub", PanelPort: 8000}, "1.1.1.1:8000", "http://1.1.1.1:8000/sub/T"},
		{db.Settings{SubPath: "sub", PanelPort: 8000, ServerAddress: "9.9.9.9"}, "x", "http://9.9.9.9:8000/sub/T"},
		{db.Settings{SubPath: "s", PanelPort: 443, PanelCertID: &id, PanelDomain: "p.example.com"}, "x", "https://p.example.com/s/T"},
		{db.Settings{SubPath: "s", PanelPort: 8443, PanelCertID: &id, SubDomain: "sub.example.com:443"}, "x", "https://sub.example.com:443/s/T"},
	}
	for _, c := range cases {
		if got := SubURL(c.s, "T", c.host); got != c.want {
			t.Errorf("got %s want %s", got, c.want)
		}
	}
}

func TestHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SetHeaders(rec, user(), db.Settings{SubTitle: "My VPN", SubUpdateHours: 6})
	h := rec.Header()
	if h.Get("subscription-userinfo") != "upload=10; download=20; total=1000; expire=0" {
		t.Fatal(h.Get("subscription-userinfo"))
	}
	if h.Get("profile-title") != "base64:"+base64.StdEncoding.EncodeToString([]byte("My VPN")) || h.Get("profile-update-interval") != "6" {
		t.Fatal("bad profile headers")
	}
}
