package xray

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/userkey"
)

func socksInput(t *testing.T) *Input {
	t.Helper()
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Protocol, err)
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 1, Remark: "socks", Protocol: inbounds.Socks, Port: 31001, Settings: inbounds.Settings{UDP: true}}),
		mk(inbounds.View{ID: 2, Remark: "open", Protocol: inbounds.Socks, Port: 31002, Settings: inbounds.Settings{NoAuth: true}}),
		mk(inbounds.View{ID: 3, Remark: "http", Protocol: inbounds.HTTP, Port: 31003}),
		mk(inbounds.View{ID: 4, Remark: "mixed", Protocol: inbounds.Mixed, Port: 31004}),
		mk(inbounds.View{ID: 5, Remark: "vless", Protocol: inbounds.VLESS, Port: 31005}),
	}
	auto := db.User{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "pw7"}
	custom := db.User{ID: 8, Username: "bob", UUID: "11111111-2222-4333-8444-555555555556", Password: "pw8",
		ProxyUsername: "bob_px", ProxyPassword: "Secret-123", BlockTorrent: true}
	users := map[int64][]db.User{}
	for _, ib := range ibs {
		users[ib.ID] = []db.User{auto, custom}
	}
	dir := t.TempDir()
	return &Input{Inbounds: ibs, Users: users, APIPort: 31099, DataDir: dir, AssetDir: dir,
		Policy:       Policy{TorrentUsers: []string{"8.bob"}, BlockedIPs: map[string][]string{"8.bob": {"9.9.9.9"}}},
		ProxyAliases: map[string]string{"8.bob": "bob_px"}}
}

func accountsOf(t *testing.T, ib map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	acc, _ := ib["settings"].(map[string]any)["accounts"].([]any)
	for _, a := range acc {
		m := a.(map[string]any)
		out[m["user"].(string)] = m["pass"].(string)
	}
	return out
}

// A standalone SOCKS inbound: Xray "socks" with password accounts (custom
// proxy login where set) or noauth; HTTP and Mixed use the custom login too;
// VLESS keeps the email; routing rules that name a user list its custom
// proxy username as well.
func TestBuildSocksAndCustomProxyLogin(t *testing.T) {
	in := socksInput(t)
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Skipped) != 0 {
		t.Fatalf("skipped: %v", in.Skipped)
	}
	ibs := inboundByTag(t, b)
	s := ibs["inbound-1"]
	st := s["settings"].(map[string]any)
	if s["protocol"] != "socks" || st["auth"] != "password" || st["udp"] != true {
		t.Fatalf("socks %v", s)
	}
	want := map[string]string{"7.alice": "pw7", "bob_px": "Secret-123"}
	for _, tag := range []string{"inbound-1", "inbound-3", "inbound-4"} {
		got := accountsOf(t, ibs[tag])
		if len(got) != 2 || got["7.alice"] != want["7.alice"] || got["bob_px"] != want["bob_px"] {
			t.Fatalf("%s accounts %v", tag, got)
		}
	}
	open := ibs["inbound-2"]["settings"].(map[string]any)
	if open["auth"] != "noauth" || open["accounts"] != nil || open["udp"] != false {
		t.Fatalf("noauth socks %v", open)
	}
	vl := ibs["inbound-5"]["settings"].(map[string]any)["clients"].([]any)
	if vl[1].(map[string]any)["email"] != "8.bob" {
		t.Fatalf("vless email changed: %v", vl)
	}
	var cfg struct {
		Routing struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	_ = json.Unmarshal(b, &cfg)
	found := 0
	for _, r := range cfg.Routing.Rules {
		us, _ := r["user"].([]any)
		if len(us) == 2 && us[0] == "8.bob" && us[1] == "bob_px" {
			found++
		}
	}
	if found != 2 { // blocked IP + torrent
		t.Fatalf("rules naming bob should list both names (%d): %v", found, cfg.Routing.Rules)
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(t.TempDir(), "c.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("xray -test: %v\n%s", err, out)
		}
	}
}

// Xray reports a custom proxy username in statistics, the online list and
// the access log; userkey maps it back to the user.
func TestCustomProxyUsernameStats(t *testing.T) {
	t.Cleanup(func() { userkey.Set(nil) })
	userkey.Set(map[string]int64{"bob_px": 8})
	tr := ParseStats([]Stat{
		{Name: "user>>>bob_px>>>traffic>>>uplink", Value: 100},
		{Name: "user>>>8.bob>>>traffic>>>downlink", Value: 50},
		{Name: "user>>>7.alice>>>traffic>>>uplink", Value: 5},
		{Name: "user>>>stranger>>>traffic>>>uplink", Value: 999},
	})
	if tr.Users[8] != [2]int64{100, 50} || tr.Users[7] != [2]int64{5, 0} || len(tr.Users) != 2 {
		t.Fatalf("users %v", tr.Users)
	}
	if id, ok := ParseEmail("bob_px"); !ok || id != 8 {
		t.Fatal("ParseEmail alias")
	}
}

// LoadInput fills the alias table from the database, and the node config
// carries it under AliasKey (left out when nobody has a custom name).
func TestNodeConfigAliases(t *testing.T) {
	t.Cleanup(func() { userkey.Set(nil) })
	g, dir, rid, _ := nodeFixture(t)
	svc := &inbounds.Service{DB: g}
	sk, err := svc.Create(&inbounds.View{Remark: "SK", Enabled: true, Protocol: inbounds.Socks, Port: 1090})
	if err != nil {
		t.Fatal(err)
	}
	var u db.User
	g.First(&u)
	g.Create(&db.UserInbound{UserID: u.ID, InboundID: sk.ID})
	plain, _, err := NodeConfig(g, dir, []int64{rid, sk.ID}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	if ConfigAliases(plain) != nil || strings.Contains(string(plain), AliasKey) {
		t.Fatal("aliases key present without custom proxy usernames")
	}
	g.Model(&db.User{}).Where("id = ?", u.ID).Updates(map[string]any{"proxy_username": "ali_px", "proxy_password": "Secret-123"})
	cfg, _, err := NodeConfig(g, dir, []int64{rid, sk.ID}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	if al := ConfigAliases(cfg); al["ali_px"] != u.ID || len(al) != 1 {
		t.Fatalf("node aliases %v", al)
	}
	if !strings.Contains(string(cfg), `"user": "ali_px"`) || !strings.Contains(string(cfg), `"pass": "Secret-123"`) {
		t.Fatalf("socks account not custom: %s", cfg)
	}
	if id, ok := userkey.ID("ali_px"); !ok || id != u.ID {
		t.Fatal("LoadInput did not fill the alias table")
	}
	if got := ConfigAliases([]byte(`{"inbounds":[]}`)); got != nil {
		t.Fatalf("no key: %v", got)
	}
	if got := ConfigAliases([]byte(`{"veilon_aliases":{"bob_px":8}}`)); got["bob_px"] != 8 {
		t.Fatalf("aliases %v", got)
	}
}
