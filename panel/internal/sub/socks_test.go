package sub

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func socksFixtures(t *testing.T) []inbounds.View {
	t.Helper()
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Protocol, err)
		}
		return v
	}
	return []inbounds.View{
		mk(inbounds.View{ID: 31, Remark: "SOCKS", Protocol: inbounds.Socks, Port: 1080, Settings: inbounds.Settings{UDP: true}}),
		mk(inbounds.View{ID: 32, Remark: "Open", Protocol: inbounds.Socks, Port: 1081, Settings: inbounds.Settings{NoAuth: true}}),
		mk(inbounds.View{ID: 33, Remark: "HTTP", Protocol: inbounds.HTTP, Port: 3128}),
		mk(inbounds.View{ID: 34, Remark: "Mixed", Protocol: inbounds.Mixed, Port: 1082}),
		mk(inbounds.View{ID: 35, Remark: "MASQUE", Protocol: inbounds.Masque, Port: 9443}),
	}
}

func byName(ps []Proxy) map[string]Proxy {
	out := map[string]Proxy{}
	for _, p := range ps {
		out[p.Name] = p
	}
	return out
}

func socksUserinfo(t *testing.T, link string) (string, *url.URL) {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "socks" {
		t.Fatalf("socks link %q: %v", link, err)
	}
	if u.User == nil {
		return "", u
	}
	b, err := base64.RawURLEncoding.DecodeString(u.User.Username())
	if err != nil {
		t.Fatal(err)
	}
	return string(b), u
}

// Links of the standalone SOCKS inbound, with the automatic login.
func TestSocksLinksAuto(t *testing.T) {
	ps := byName(BuildProxiesX(user(), socksFixtures(t), db.Settings{ServerAddress: "srv.example.com"}, "h", Extras{}))
	s := ps["SOCKS"]
	ui, u := socksUserinfo(t, URI(&s))
	if ui != "7.alice:trojanpass" || u.Host != "srv.example.com:1080" || u.Fragment != "SOCKS" {
		t.Fatalf("socks %s %v", ui, u)
	}
	o := ps["Open"]
	if got := URI(&o); got != "socks://srv.example.com:1081#Open" {
		t.Fatalf("noauth socks link %s", got)
	}
	c := clashOf(&o)
	if c.Type != "socks5" || c.Username != "" || c.Password != "" || c.UDP {
		t.Fatalf("clash noauth %+v", c)
	}
	c = clashOf(&s)
	if c.Type != "socks5" || c.Username != "7.alice" || c.Password != "trojanpass" || !c.UDP {
		t.Fatalf("clash socks %+v", c)
	}
	sb, _ := singboxExtra(&o)
	if sb["type"] != "socks" || sb["version"] != "5" || sb["username"] != nil || sb["network"] != "tcp" {
		t.Fatalf("sing-box noauth %v", sb)
	}
	sb, _ = singboxExtra(&s)
	if sb["username"] != "7.alice" || sb["password"] != "trojanpass" || sb["network"] != nil {
		t.Fatalf("sing-box socks %v", sb)
	}
	x := xrayExtraOutbound(&o)
	srv := x["settings"].(obj)["servers"].([]any)[0].(obj)
	if x["protocol"] != "socks" || srv["users"] != nil || srv["port"] != 1081 {
		t.Fatalf("xray noauth %v", x)
	}
	for _, p := range []Proxy{s, o} {
		if !ClashSupported(&p) || !SingboxSupported(&p) || !XraySupported(&p) {
			t.Fatalf("%s must be in every format", p.Name)
		}
	}
}

// A custom proxy login replaces the automatic one on HTTP, Mixed and SOCKS
// only (MASQUE keeps the automatic identity).
func TestCustomProxyLoginLinks(t *testing.T) {
	u := user()
	u.ProxyUsername, u.ProxyPassword = "ali_px", "Secret-123"
	ps := byName(BuildProxiesX(u, socksFixtures(t), db.Settings{ServerAddress: "srv.example.com"}, "h", Extras{}))
	s := ps["SOCKS"]
	if ui, _ := socksUserinfo(t, URI(&s)); ui != "ali_px:Secret-123" {
		t.Fatalf("socks userinfo %s", ui)
	}
	mx := ps["Mixed"]
	if ui, _ := socksUserinfo(t, URI(&mx)); ui != "ali_px:Secret-123" {
		t.Fatalf("mixed userinfo %s", ui)
	}
	h := ps["HTTP"]
	if got := URI(&h); got != "http://ali_px:Secret-123@srv.example.com:3128#HTTP" {
		t.Fatalf("http %s", got)
	}
	if o := ps["Open"]; URI(&o) != "socks://srv.example.com:1081#Open" {
		t.Fatal("noauth socks must not carry a login")
	}
	if m := ps["MASQUE"]; m.Username != "7.alice" || m.Password != "trojanpass" {
		t.Fatalf("masque login changed: %s", m.Username)
	}
	cy, err := yaml.Marshal(clashOf(&s))
	if err != nil || !strings.Contains(string(cy), "username: ali_px") || !strings.Contains(string(cy), "password: Secret-123") {
		t.Fatalf("clash yaml %s", cy)
	}
	sb, _ := singboxExtra(&h)
	if sb["username"] != "ali_px" || sb["password"] != "Secret-123" {
		t.Fatalf("sing-box http %v", sb)
	}
	xj, _ := json.Marshal(xrayExtraOutbound(&mx))
	if !strings.Contains(string(xj), `"user":"ali_px"`) || !strings.Contains(string(xj), `"pass":"Secret-123"`) {
		t.Fatalf("xray mixed %s", xj)
	}
	// Only the username set: the password stays the generated one.
	u.ProxyPassword = ""
	ps = byName(BuildProxiesX(u, socksFixtures(t), db.Settings{ServerAddress: "srv.example.com"}, "h", Extras{}))
	h = ps["HTTP"]
	if got := URI(&h); got != "http://ali_px:trojanpass@srv.example.com:3128#HTTP" {
		t.Fatalf("http username only %s", got)
	}
}
