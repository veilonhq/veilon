package sub

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func TestHostsExpandLinks(t *testing.T) {
	ibs := fixtures()
	hosts := map[int64][]db.Host{
		2: {
			{Remark: "CDN 1", Address: "cdn1.example.org", Port: 443, SNI: "sni1.example.org", Host: "h1.example.org", Path: "/p1", Fingerprint: "safari", ALPN: "h2"},
			{Address: "2.2.2.2"},
		},
	}
	ex := Extras{Hosts: hosts}
	ps := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "1.2.3.4"}, "x", ex)
	plain := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "1.2.3.4"}, "x", Extras{})
	if len(ps) != len(plain)+1 {
		t.Fatalf("want %d proxies, got %d", len(plain)+1, len(ps))
	}
	var got []Proxy
	for _, p := range ps {
		if p.InboundID == 2 {
			got = append(got, p)
		}
	}
	a, b := got[0], got[1]
	if a.Name != "CDN 1" || a.Server != "cdn1.example.org" || a.Port != 443 || a.SNI != "sni1.example.org" || a.Host != "h1.example.org" ||
		a.Path != "/p1" || a.Fingerprint != "safari" || len(a.ALPN) != 1 || a.ALPN[0] != "h2" {
		t.Fatalf("host 1 not applied: %+v", a)
	}
	// empty fields keep the inbound's values; the name falls back to the remark
	if b.Name != "VM WS TLS" || b.Server != "2.2.2.2" || b.Port != 8443 || b.SNI != "cdn.example.com" || b.Path != "/ws" || b.Host != "cdn.example.com" {
		t.Fatalf("host 2 should only change the address: %+v", b)
	}
	// other inbounds are untouched
	for i, p := range plain {
		if p.InboundID == 2 {
			continue
		}
		found := false
		for _, q := range ps {
			if q.InboundID == p.InboundID && URI(&q) == URI(&plain[i]) {
				found = true
			}
		}
		if !found {
			t.Fatalf("inbound %d link changed", p.InboundID)
		}
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(URI(&a), "vmess://"))
	var m map[string]string
	_ = json.Unmarshal(raw, &m)
	if m["add"] != "cdn1.example.org" || m["port"] != "443" || m["sni"] != "sni1.example.org" || m["host"] != "h1.example.org" || m["ps"] != "CDN 1" {
		t.Fatalf("bad share link %s", raw)
	}
}
