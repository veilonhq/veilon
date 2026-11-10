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
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Regression (seen on v0.3.1): user "node-test" is on inbound 17 "Node
// test" (VLESS Reality :2096), which was assigned to node "Lightsail"
// (16.192.129.144) through the Nodes page. The subscription also carried a
// link to the main server (13.49.190.211). After the upgrade the stored rows
// are unchanged (serve_main defaults to TRUE, so the main server keeps
// listening on 2096 and nothing breaks), but every link of the inbound
// points at the node only.
func TestRegressionV031NodeInboundLinksUseNodeAddress(t *testing.T) {
	g := testutil.DB(t)
	now := time.Now().UTC()
	_ = db.SaveSettings(g, db.Settings{ServerAddress: "13.49.190.211", SubPath: "sub", SubUpdateHours: 12, Language: "fa", Timezone: "UTC"})
	// Rows exactly as v0.3.1 wrote them (no serve_main column in the insert,
	// as after the schema 8 upgrade).
	reality := `{"dest":"www.speedtest.net:443","server_names":["www.speedtest.net"],"private_key":"` + mustKey(t) + `","public_key":"","short_ids":["0123456789abcdef"],"fingerprint":"chrome","spider_x":"/"}`
	g.Exec(`INSERT INTO inbounds (id, remark, enabled, protocol, port, network, security, reality, transport, flow, sniffing, settings, created_at, updated_at)
		VALUES (17, 'Node test', TRUE, 'vless', 2096, 'tcp', 'reality', ?, '{}', 'xtls-rprx-vision', TRUE, '{}', ?, ?)`, reality, now, now)
	g.Exec(`INSERT INTO nodes (id, name, address, api_port, enabled, created_at, updated_at) VALUES (1, 'Lightsail', '16.192.129.144', 62050, TRUE, ?, ?)`, now, now)
	g.Exec(`INSERT INTO node_inbounds (node_id, inbound_id) VALUES (1, 17)`)
	u := db.User{ID: 9, Username: "node-test", Status: db.StatusActive, UUID: "8c5f8a2e-7a3c-4d5e-9f00-112233445599", Password: "pw", SubToken: "tok-nt", CreatedAt: now, UpdatedAt: now}
	g.Create(&u)
	g.Create(&db.UserInbound{UserID: 9, InboundID: 17})

	var sm bool
	g.Raw("SELECT serve_main FROM inbounds WHERE id = 17").Scan(&sm)
	if !sm {
		t.Fatal("existing row must keep serve_main TRUE")
	}
	in, err := xray.LoadMainInput(g, t.TempDir(), 62000)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Inbounds) != 1 || in.Inbounds[0].ID != 17 {
		t.Fatalf("the main server must keep serving inbound 17 after the upgrade: %+v", in.Inbounds)
	}

	s := &Service{DB: g}
	get := func(ua, q string, info bool) string {
		r := httptest.NewRequest("GET", "https://13.49.190.211:2053/sub/tok-nt"+q, nil)
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		s.Serve(w, r, "tok-nt", info)
		return w.Body.String()
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(get("v2rayNG/1.9", "", false)))
	if err != nil {
		t.Fatal(err)
	}
	for f, b := range map[string]string{"base64": string(raw), "clash": get("clash.meta", "", false), "xray": get("x", "?format=json", false), "info": get("x", "", true)} {
		if strings.Contains(b, "13.49.190.211") {
			t.Fatalf("%s still points at the main server:\n%s", f, b)
		}
		if !strings.Contains(b, "16.192.129.144") {
			t.Fatalf("%s lacks the node address:\n%s", f, b)
		}
	}
	links := strings.Fields(string(raw))
	if len(links) != 1 || !strings.HasPrefix(links[0], "vless://8c5f8a2e-7a3c-4d5e-9f00-112233445599@16.192.129.144:2096?") ||
		!strings.HasSuffix(links[0], "#Node%20test%20%C2%B7%20Lightsail") {
		t.Fatalf("links: %v", links)
	}

	// With the node switched off the main server (which still runs the
	// inbound) is the only way in, so its link comes back.
	g.Exec(`UPDATE nodes SET enabled = FALSE WHERE id = 1`)
	_, ibs, _ := s.Resolve("tok-nt")
	ps := BuildProxiesX(&u, ibs, db.LoadSettings(g), "", s.Extras(&u, ibs))
	if len(ps) != 1 || ps[0].Server != "13.49.190.211" {
		t.Fatalf("node off: %+v", ps)
	}
}

func mustKey(t *testing.T) string {
	k, err := inbounds.GenerateRealityKeys()
	if err != nil {
		t.Fatal(err)
	}
	return k.PrivateKey
}
