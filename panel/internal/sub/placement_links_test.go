package sub

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// A node-only inbound never shows the main server's address in any format
// (share links, base64, Clash, sing-box, Xray JSON, the info endpoint
// behind the panel's copy and QR buttons). Hosts still win over the node
// address when they set one.
func TestNodeLinksEveryFormat(t *testing.T) {
	g := testutil.DB(t)
	svc := &inbounds.Service{DB: g}
	now := time.Now().UTC()
	n1 := db.Node{Name: "Frankfurt", Address: "203.0.113.10", APIPort: 62050, Enabled: true, CreatedAt: now, UpdatedAt: now}
	n2 := db.Node{Name: "Paris", Address: "paris.example.net", APIPort: 62050, Enabled: true, CreatedAt: now, UpdatedAt: now}
	g.Create(&n1)
	g.Create(&n2)
	_ = db.SaveSettings(g, db.Settings{ServerAddress: "198.51.100.1", SubPath: "sub", SubUpdateHours: 12, Language: "en", Timezone: "UTC"})
	v := inbounds.Defaults()
	// Shadowsocks: every format (sing-box included) carries it.
	v.Remark, v.Protocol, v.Port, v.SSMethod = "Edge", "shadowsocks", 4432, "chacha20-ietf-poly1305"
	v.NodeIDs, v.ServeMain = []int64{n1.ID, n2.ID}, false
	edge, err := svc.Create(&v)
	if err != nil {
		t.Fatal(err)
	}
	u := db.User{Username: "bob", Status: db.StatusActive, UUID: "8c5f8a2e-7a3c-4d5e-9f00-112233445566", Password: "pw", SubToken: "tok2", CreatedAt: now, UpdatedAt: now}
	g.Create(&u)
	g.Create(&db.UserInbound{UserID: u.ID, InboundID: edge.ID})
	s := &Service{DB: g}

	get := func(ua, query string, info bool) string {
		r := httptest.NewRequest("GET", "http://198.51.100.1:2053/sub/tok2"+query, nil)
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		s.Serve(w, r, "tok2", info)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d", ua, query, w.Code)
		}
		return w.Body.String()
	}
	b64 := get("v2rayNG/1.9", "", false)
	plain, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	bodies := map[string]string{
		"base64": string(plain), "clash": get("clash-verge/2", "", false), "singbox": get("SFA/1.11", "", false),
		"xray": get("x", "?format=json", false), "info": get("x", "", true),
	}
	for f, b := range bodies {
		if strings.Contains(b, "198.51.100.1") {
			t.Fatalf("%s mentions the main server address:\n%s", f, b)
		}
		if !strings.Contains(b, "203.0.113.10") || !strings.Contains(b, "paris.example.net") {
			t.Fatalf("%s lacks a node address:\n%s", f, b)
		}
	}
	if !strings.Contains(string(plain), "#Edge%20%C2%B7%20Frankfurt") || !strings.Contains(string(plain), "#Edge%20%C2%B7%20Paris") {
		t.Fatalf("node names missing from links:\n%s", plain)
	}
	if n := strings.Count(string(plain), "ss://"); n != 2 {
		t.Fatalf("%d links, want one per node:\n%s", n, plain)
	}

	// Hosts: a host without an address keeps each node's address (its
	// other overrides apply); a host with an address wins over the nodes.
	if _, err := svc.SetHosts(edge.ID, []inbounds.HostInput{{Remark: "CDN", SNI: "cdn.example.org"}}); err != nil {
		t.Fatal(err)
	}
	_, ibs, _ := s.Resolve("tok2")
	ps := BuildProxiesX(&u, ibs, db.LoadSettings(g), "", s.Extras(&u, ibs))
	if len(ps) != 2 || ps[0].Server != "203.0.113.10" || ps[1].Server != "paris.example.net" || ps[0].SNI != "cdn.example.org" || ps[0].Name != "CDN · Frankfurt" {
		t.Fatalf("host without address: %+v", ps)
	}
	if _, err := svc.SetHosts(edge.ID, []inbounds.HostInput{{Remark: "CDN", Address: "cdn.example.org"}}); err != nil {
		t.Fatal(err)
	}
	_, ibs, _ = s.Resolve("tok2")
	ps = BuildProxiesX(&u, ibs, db.LoadSettings(g), "", s.Extras(&u, ibs))
	if len(ps) != 1 || ps[0].Server != "cdn.example.org" {
		t.Fatalf("host address must win (and duplicates collapse): %+v", ps)
	}
}
