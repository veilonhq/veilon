package xray

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/userfw"
	"github.com/veilonhq/veilon/panel/internal/userkey"
)

func routingRules(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var cfg struct {
		Routing struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Routing.Rules
}

// The user's own block list becomes two blackhole rules (names and
// addresses) that name the user (and its custom proxy username), ahead of
// the admin's routing rules.
func TestBuildUserFirewall(t *testing.T) {
	t.Cleanup(func() { userkey.Set(nil) })
	in := socksInput(t)
	in.Policy.Firewall = map[string]userfw.Rules{
		"8.bob":   userfw.Render([]string{"example.com", "full:api.example.org", "regexp:^ads\\.", "93.184.215.14", "10.0.0.0/8", "2001:db8::/32"}),
		"7.alice": userfw.Render([]string{"1.1.1.1"}),
	}
	tpl := DefaultTemplate()
	tpl.Routing.Rules = []map[string]any{{"domain": []any{"example.com"}, "outboundTag": TagDirect}}
	in.Template = tpl
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	rs := routingRules(t, b)
	idx := func(pred func(map[string]any) bool) int {
		for i, r := range rs {
			if pred(r) {
				return i
			}
		}
		return -1
	}
	users := func(r map[string]any) []any { u, _ := r["user"].([]any); return u }
	bobNames := idx(func(r map[string]any) bool {
		return r["domain"] != nil && reflect.DeepEqual(users(r), []any{"8.bob", "bob_px"})
	})
	bobIPs := idx(func(r map[string]any) bool {
		return r["ip"] != nil && reflect.DeepEqual(users(r), []any{"8.bob", "bob_px"})
	})
	aliceIPs := idx(func(r map[string]any) bool { return r["ip"] != nil && reflect.DeepEqual(users(r), []any{"7.alice"}) })
	admin := idx(func(r map[string]any) bool { return r["outboundTag"] == TagDirect })
	if bobNames < 0 || bobIPs < 0 || aliceIPs < 0 || admin < 0 {
		t.Fatalf("rules missing: %v", rs)
	}
	if !(bobNames < admin && bobIPs < admin && aliceIPs < admin) {
		t.Fatalf("firewall rules must come before the admin's rules: %v", rs)
	}
	if aliceIPs > bobNames {
		t.Fatalf("rules not sorted by user: %v", rs)
	}
	if !reflect.DeepEqual(rs[bobNames]["domain"], []any{"domain:example.com", "full:api.example.org", "regexp:^ads\\."}) ||
		!reflect.DeepEqual(rs[bobIPs]["ip"], []any{"10.0.0.0/8", "2001:db8::/32", "93.184.215.14"}) ||
		rs[bobNames]["outboundTag"] != TagBlocked || rs[bobIPs]["outboundTag"] != TagBlocked {
		t.Fatalf("bob rules %v / %v", rs[bobNames], rs[bobIPs])
	}
	if aliceNames := idx(func(r map[string]any) bool {
		return r["domain"] != nil && reflect.DeepEqual(users(r), []any{"7.alice"})
	}); aliceNames >= 0 {
		t.Fatal("a names rule for a user without names")
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(t.TempDir(), "c.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("xray -test: %v\n%s", err, out)
		}
		t.Log("xray run -test: OK")
	}
}

// Without any list the config is exactly what it was before.
func TestBuildNoFirewallUnchanged(t *testing.T) {
	in := socksInput(t)
	a, _ := Build(in)
	in = socksInput(t)
	in.Policy.Firewall = map[string]userfw.Rules{}
	b, _ := Build(in)
	if string(a) != string(b) {
		t.Fatal("an empty firewall map changed the config")
	}
}

// LoadInput reads the lists of served users (a disabled user's list is
// not rendered); node configs carry the same rules; the WireGuard inbound
// (Xray) names its peers by email, so the rules cover it too.
func TestLoadInputUserFirewall(t *testing.T) {
	t.Cleanup(func() { userkey.Set(nil) })
	g := testutil.DB(t)
	dir := t.TempDir()
	svc := &inbounds.Service{DB: g}
	vl, err := svc.Create(&inbounds.View{Remark: "VL", Enabled: true, Protocol: inbounds.VLESS, Port: 4443, Network: "tcp", Security: "none"})
	if err != nil {
		t.Fatal(err)
	}
	wg, err := svc.Create(&inbounds.View{Remark: "WG", Enabled: true, Protocol: inbounds.WireGuard, Port: 51820})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	mk := func(name, status, fw string) db.User {
		u := db.User{Username: name, Status: status, UUID: "8c5f8a2e-7a3c-4d5e-9f00-1122334455" + name[:2], Password: "pw-" + name, SubToken: "tok-" + name,
			Firewall: fw, CreatedAt: now, UpdatedAt: now}
		if err := g.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		g.Create(&db.UserInbound{UserID: u.ID, InboundID: vl.ID})
		g.Create(&db.UserInbound{UserID: u.ID, InboundID: wg.ID})
		return u
	}
	a := mk("aa-user", db.StatusActive, "example.com\n93.184.215.14")
	mk("bb-user", db.StatusActive, "")
	mk("cc-user", db.StatusDisabled, "blocked.example")
	in, err := LoadInput(g, dir, 31099)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Policy.Firewall) != 1 {
		t.Fatalf("policy firewall %v", in.Policy.Firewall)
	}
	r := in.Policy.Firewall[Email(&a)]
	if !reflect.DeepEqual(r.XrayDomains, []string{"domain:example.com"}) || !reflect.DeepEqual(r.XrayIPs, []string{"93.184.215.14"}) {
		t.Fatalf("rules %+v", r)
	}
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	_ = json.Unmarshal(b, &cfg)
	sawPeer := false
	for _, ib := range cfg.Inbounds {
		if ib["protocol"] == "wireguard" {
			for _, p := range ib["settings"].(map[string]any)["peers"].([]any) {
				if p.(map[string]any)["email"] == Email(&a) {
					sawPeer = true
				}
			}
		}
	}
	if !sawPeer {
		t.Fatal("wireguard peer without the user's email")
	}
	nc, _, err := NodeConfig(g, dir, []int64{vl.ID}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, rr := range routingRules(t, nc) {
		if u, _ := rr["user"].([]any); len(u) == 1 && u[0] == Email(&a) && rr["outboundTag"] == TagBlocked {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("node config firewall rules: %d", found)
	}
}
