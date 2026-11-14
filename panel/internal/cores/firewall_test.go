package cores

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/userfw"
)

// A user's own block list becomes sing-box reject rules (names after a
// sniff, addresses by ip_cidr) ahead of the user's bridge route; other
// users are untouched.
func TestSingboxUserFirewall(t *testing.T) {
	in := input(t)
	in.Policy.Firewall = map[string]userfw.Rules{
		"7.alice": userfw.Render([]string{"example.com", "full:api.example.org", "regexp:^ads\\.", "93.184.215.14", "2001:db8::/32"}),
	}
	b, err := SingboxConfig(in, 30555)
	if err != nil || b == nil {
		t.Fatal(err)
	}
	var cfg struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	rs := cfg.Route.Rules
	if len(rs) != 5 {
		t.Fatalf("rules %v", rs)
	}
	user := func(r map[string]any) string { return r["auth_user"].([]any)[0].(string) }
	if user(rs[0]) != "7.alice" || rs[0]["action"] != "sniff" {
		t.Fatalf("sniff first: %v", rs[0])
	}
	if rs[1]["action"] != "reject" || !reflect.DeepEqual(rs[1]["domain_suffix"], []any{".example.com"}) ||
		!reflect.DeepEqual(rs[1]["domain"], []any{"api.example.org", "example.com"}) || !reflect.DeepEqual(rs[1]["domain_regex"], []any{"^ads\\."}) {
		t.Fatalf("name rule %v", rs[1])
	}
	if rs[2]["action"] != "reject" || !reflect.DeepEqual(rs[2]["ip_cidr"], []any{"2001:db8::/32", "93.184.215.14/32"}) {
		t.Fatalf("ip rule %v", rs[2])
	}
	if rs[3]["outbound"] != "u-7" || user(rs[4]) != "8.bob" || rs[4]["outbound"] != "u-8" {
		t.Fatalf("user routes %v", rs[3:])
	}
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" {
		f := filepath.Join(t.TempDir(), "sb.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("sing-box check: %v\n%s", err, out)
		}
		t.Log("sing-box check: OK")
	}
	// Addresses only: no sniff rule.
	in.Policy.Firewall = map[string]userfw.Rules{"8.bob": userfw.Render([]string{"10.0.0.0/8"})}
	b, _ = SingboxConfig(in, 30555)
	cfg.Route.Rules = nil
	_ = json.Unmarshal(b, &cfg)
	if len(cfg.Route.Rules) != 3 || cfg.Route.Rules[1]["action"] != "reject" || user(cfg.Route.Rules[1]) != "8.bob" {
		t.Fatalf("ip-only %v", cfg.Route.Rules)
	}
}
