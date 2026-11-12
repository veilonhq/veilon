package veiloncore

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDirectDNSDoHDefault (1.0.8, audit M1): the core's fallback direct DNS
// equals the app's new-install default (DoH to AliDNS by IP); sing-box gets
// an https DNS server with no plain-UDP bootstrap for it, Xray a local DoH
// server. A plain IP keeps working as before.
func TestDirectDNSDoHDefault(t *testing.T) {
	if DefaultSettings().DirectDNS != "https://223.5.5.5/dns-query" {
		t.Fatalf("default direct DNS %q", DefaultSettings().DirectDNS)
	}
	st, err := parseSettings(`{"directDns":""}`)
	if err != nil || st.DirectDNS != DefaultDirectDNS {
		t.Fatalf("empty directDns → %q (%v)", st.DirectDNS, err)
	}
	p, err := ParseLink(configCases[0].link)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ spec, sbType, xray string }{
		{"https://223.5.5.5/dns-query", "https", "https+local://223.5.5.5/dns-query"},
		{"223.5.5.5", "udp", "223.5.5.5"},
		{"https://dns.example/dns-query", "https", "8.8.8.8"},
	} {
		st := DefaultSettings()
		st.DirectDNS = tc.spec
		cfg, err := checkProfile(p, st, true)
		if err != nil {
			t.Fatalf("%s: %v", tc.spec, err)
		}
		b, _ := json.Marshal(cfg)
		var c struct {
			DNS struct {
				Servers []map[string]any `json:"servers"`
			} `json:"dns"`
		}
		_ = json.Unmarshal(b, &c)
		var direct map[string]any
		for _, s := range c.DNS.Servers {
			if s["tag"] == "dns-direct" {
				direct = s
			}
		}
		if direct == nil || direct["type"] != tc.sbType {
			t.Errorf("%s: dns-direct %v", tc.spec, direct)
		}
		if tc.sbType == "https" && strings.HasPrefix(tc.spec, "https://223") {
			if direct["server"] != "223.5.5.5" || direct["domain_resolver"] != nil {
				t.Errorf("%s: dns-direct %v", tc.spec, direct)
			}
		}
		x, err := xrayConfig(p, 10809, "u", "p", st)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkXrayConfig(x); err != nil {
			t.Fatalf("%s: xray: %v", tc.spec, err)
		}
		xs := x["dns"].(obj)["servers"].([]any)
		if len(xs) != 1 || xs[0] != tc.xray {
			t.Errorf("%s: xray dns %v (want %s)", tc.spec, xs, tc.xray)
		}
	}
}
