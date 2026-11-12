package veiloncore

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSOCKSRestored: generic SOCKS5 (a standard Xray / sing-box protocol)
// is parsed from links and subscriptions, builds on sing-box (plain) or
// Xray (TLS), and survives ToLink → ParseLink.
func TestSOCKSRestored(t *testing.T) {
	links := map[string]struct {
		user, pass, sec string
		core            Core
	}{
		"socks5://alice:s3cret@198.51.100.10:1080#a":                        {"alice", "s3cret", "", CoreSingbox},
		"socks://198.51.100.10:1080#noauth":                                 {"", "", "", CoreSingbox},
		"socks5h://bob:pw@198.51.100.11:1081":                               {"bob", "pw", "", CoreSingbox},
		"socks://Ym9iOnB3@198.51.100.12:1082#v2rayn":                        {"bob", "pw", "", CoreSingbox},
		"socks5://u:p@proxy.example.com:443?security=tls&sni=s.example.com": {"u", "p", "tls", CoreXray},
	}
	for l, w := range links {
		p, err := ParseLink(l)
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		if p.Type != TypeSOCKS || p.Username != w.user || p.Password != w.pass || strings.TrimPrefix(p.Security, "none") != w.sec {
			t.Errorf("%s: %+v", l, p)
		}
		if c := CoreOf(p); c != w.core {
			t.Errorf("%s: core %s, want %s", l, c, w.core)
		}
		js, _ := json.Marshal(p)
		if msg := CheckProfile(string(js), ""); msg != "" {
			t.Errorf("%s: CheckProfile %s", l, msg)
		}
		out, err := ToLink(p)
		if err != nil || !strings.HasPrefix(out, "socks5://") {
			t.Fatalf("%s: ToLink %q %v", l, out, err)
		}
		p2, err := ParseLink(out)
		if err != nil || p2.Server != p.Server || p2.Port != p.Port || p2.Username != p.Username ||
			p2.Password != p.Password || p2.Security != p.Security || p2.SNI != p.SNI || p2.Name != p.Name {
			t.Errorf("%s: round trip %s → %+v %v", l, out, p2, err)
		}
	}

	// sing-box outbound (plain).
	p, _ := ParseLink("socks5://alice:s3cret@198.51.100.10:1080#a")
	o, _, err := singboxOutbound(p, "proxy")
	if err != nil || o["type"] != "socks" || o["version"] != "5" || o["username"] != "alice" || o["password"] != "s3cret" {
		t.Errorf("sing-box outbound %v %v", o, err)
	}
	// Xray outbound (TLS).
	p, _ = ParseLink("socks5://u:p@proxy.example.com:443?security=tls&sni=s.example.com")
	x, err := xrayOutbound(p)
	if err != nil || x["protocol"] != "socks" {
		t.Fatalf("xray outbound %v %v", x, err)
	}
	if ss, _ := x["streamSettings"].(obj); ss["security"] != "tls" {
		t.Errorf("xray stream %v", x["streamSettings"])
	}

	// Subscriptions: Clash, sing-box JSON, Xray JSON.
	clash := ParseContent("proxies:\n  - {name: c1, type: socks5, server: 198.51.100.20, port: 1080, username: u, password: p}\n  - {name: c2, type: socks5, server: s.example.com, port: 443, tls: true, sni: s.example.com}\n")
	if len(clash.Profiles) != 2 || clash.Profiles[0].Type != TypeSOCKS || clash.Profiles[0].Username != "u" || clash.Profiles[1].Security != "tls" {
		t.Errorf("clash: %+v %v", clash.Profiles, clash.Errors)
	}
	sb := ParseContent(`{"outbounds":[{"type":"socks","tag":"sb","server":"198.51.100.21","server_port":1080,"version":"5","username":"u","password":"p"},{"type":"direct","tag":"direct"}]}`)
	if len(sb.Profiles) != 1 || sb.Profiles[0].Type != TypeSOCKS || sb.Profiles[0].Password != "p" {
		t.Errorf("sing-box json: %+v %v", sb.Profiles, sb.Errors)
	}
	xr := ParseContent(`{"outbounds":[{"tag":"xr","protocol":"socks","settings":{"servers":[{"address":"198.51.100.22","port":1080,"users":[{"user":"u","pass":"p"}]}]}},{"protocol":"freedom","tag":"direct"}]}`)
	if len(xr.Profiles) != 1 || xr.Profiles[0].Type != TypeSOCKS || xr.Profiles[0].Username != "u" {
		t.Errorf("xray json: %+v %v", xr.Profiles, xr.Errors)
	}
	xf := ParseContent(`{"outbounds":[{"tag":"xf","protocol":"socks","settings":{"address":"198.51.100.23","port":1080,"user":"u2","pass":"p2"}}]}`)
	if len(xf.Profiles) != 1 || xf.Profiles[0].Type != TypeSOCKS || xf.Profiles[0].Username != "u2" {
		t.Errorf("xray flat json: %+v %v", xf.Profiles, xf.Errors)
	}
}
