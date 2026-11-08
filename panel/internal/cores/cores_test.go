package cores

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/awgd"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

func input(t *testing.T) *xray.Input {
	t.Helper()
	dir := t.TempDir()
	cp, kp, _ := certs.GenerateSelfSigned("www.bing.com")
	cf, kf := certs.Paths(dir, 1)
	_ = os.MkdirAll(filepath.Dir(cf), 0o700)
	_ = os.WriteFile(cf, []byte(cp), 0o600)
	_ = os.WriteFile(kf, []byte(kp), 0o600)
	one := int64(1)
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatal(err)
		}
		if v.TLS != nil {
			v.TLS.CertID = &one
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 1, Remark: "tuic", Protocol: inbounds.TUIC, Port: 31001}),
		mk(inbounds.View{ID: 2, Remark: "mt", Protocol: inbounds.MTProto, Port: 31002}),
		mk(inbounds.View{ID: 3, Remark: "awg", Protocol: inbounds.AmneziaWG, Port: 31003}),
		mk(inbounds.View{ID: 4, Remark: "vless", Protocol: inbounds.VLESS, Port: 31004}),
	}
	us := []db.User{{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "pw7"},
		{ID: 8, Username: "bob", UUID: "21111111-2222-4333-8444-555555555555", Password: "pw8"}}
	users := map[int64][]db.User{1: us, 2: us, 3: us, 4: us}
	_, pub7, _ := inbounds.GenerateWGKeys()
	_, pub8, _ := inbounds.GenerateWGKeys()
	peers := map[int64]map[int64]db.WGPeer{3: {
		7: {PublicKey: pub7, PresharedKey: inbounds.GeneratePSK(), Address: ibs[2].Settings.Address[:strings.LastIndex(ibs[2].Settings.Address, ".")] + ".2/32"},
		8: {PublicKey: pub8, Address: ibs[2].Settings.Address[:strings.LastIndex(ibs[2].Settings.Address, ".")] + ".3/32"},
	}}
	return &xray.Input{Inbounds: ibs, Users: users, Peers: peers, DataDir: dir}
}

func TestSingboxConfig(t *testing.T) {
	in := input(t)
	b, err := SingboxConfig(in, 30555)
	if err != nil || b == nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules []map[string]any `json:"rules"`
			Final string           `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0]["type"] != "tuic" || cfg.Inbounds[0]["congestion_control"] != "bbr" {
		t.Fatalf("inbounds %v", cfg.Inbounds)
	}
	if len(cfg.Inbounds[0]["users"].([]any)) != 2 {
		t.Fatal("users")
	}
	// One bridge outbound per user to Xray, unauthenticated traffic blocked.
	if len(cfg.Outbounds) != 3 || cfg.Outbounds[1]["username"] != "7.alice" || cfg.Outbounds[1]["server_port"].(float64) != 30555 || cfg.Route.Final != "block" {
		t.Fatalf("outbounds %v final %s", cfg.Outbounds, cfg.Route.Final)
	}
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" {
		f := filepath.Join(t.TempDir(), "sb.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("sing-box check: %v\n%s", err, out)
		}
		t.Log("sing-box check: OK")
	}
	// Nothing to serve -> nil.
	in.Inbounds = in.Inbounds[1:]
	if b, _ := SingboxConfig(in, 1); b != nil {
		t.Fatal("expected nil config without tuic inbounds")
	}
}

func TestMTGConfig(t *testing.T) {
	in := input(t)
	ib := &in.Inbounds[1]
	c := string(MTGConfig(ib, in, 40001))
	for _, want := range []string{`bind-to = "0.0.0.0:31002"`, `api-bind-to = "127.0.0.1:40001"`,
		`"7.alice" = "` + inbounds.MTProtoSecret("www.cloudflare.com", "pw7") + `"`, "[secrets]"} {
		if !strings.Contains(c, want) {
			t.Fatalf("missing %q in\n%s", want, c)
		}
	}
	if strings.Index(c, "[secrets]") < strings.LastIndex(c, "[defense") {
		t.Fatal("[secrets] must be the last section")
	}
}

func TestAWGConfig(t *testing.T) {
	in := input(t)
	b, rk, owners, err := AWGConfig(in)
	if err != nil || b == nil {
		t.Fatal(err)
	}
	var c awgd.Config
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Interfaces) != 1 || c.Interfaces[0].Name != "vawg3" || len(c.Interfaces[0].Peers) != 2 || c.Interfaces[0].Params.Jc == 0 {
		t.Fatalf("awg config %s", b)
	}
	if len(owners) != 2 {
		t.Fatal("owners")
	}
	// Peer changes keep the reload key (SIGHUP instead of restart).
	in.Users[3] = in.Users[3][:1]
	_, rk2, _, _ := AWGConfig(in)
	if rk != rk2 {
		t.Fatal("reload key changed on a peer-only change")
	}
	in.Inbounds[2].Settings.MTU = 1400
	_, rk3, _, _ := AWGConfig(in)
	if rk3 == rk {
		t.Fatal("reload key unchanged after an interface change")
	}
	if _, err := awgd.UAPI(c.Interfaces[0], false); err != nil {
		t.Fatal(err)
	}
}

func TestCounter(t *testing.T) {
	c := counter{}
	if u, d := c.delta("a", 1, 10, 20); u != 10 || d != 20 {
		t.Fatal("first")
	}
	if u, d := c.delta("a", 1, 15, 25); u != 5 || d != 5 {
		t.Fatal("delta")
	}
	if u, d := c.delta("a", 2, 3, 4); u != 3 || d != 4 {
		t.Fatal("restart")
	}
	if u, d := c.delta("a", 2, 1, 1); u != 1 || d != 1 {
		t.Fatal("counter reset")
	}
}

// TUIC users go through Xray's loopback bridge, so sing-box itself must log
// connections (client IP + user, level info) for the connection guard and
// reject the client IPs an admin blocked for a user.
func TestSingboxConfigGuardHooks(t *testing.T) {
	in := input(t)
	in.Policy.BlockedIPs = map[string][]string{
		"7.alice": {"1.2.3.4", "2001:db8::1", "not-an-ip", "1.2.3.4", "::ffff:5.6.7.8"},
		"9.ghost": {"9.9.9.9"}, // not on the inbound
	}
	b, err := SingboxConfig(in, 30555)
	if err != nil || b == nil {
		t.Fatal(err)
	}
	var cfg struct {
		Log   map[string]any `json:"log"`
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Log["level"] != "info" || cfg.Log["timestamp"] != true || cfg.Log["output"] != nil || cfg.Log["disabled"] != nil {
		t.Fatalf("log %v", cfg.Log)
	}
	r := cfg.Route.Rules
	if len(r) != 3 {
		t.Fatalf("rules %v", r)
	}
	// The reject rule must come before the user's bridge rule.
	if r[0]["action"] != "reject" || r[0]["auth_user"].([]any)[0] != "7.alice" || r[1]["outbound"] != "u-7" || r[2]["outbound"] != "u-8" {
		t.Fatalf("rules %v", r)
	}
	if got := r[0]["source_ip_cidr"].([]any); len(got) != 3 || got[0] != "1.2.3.4/32" || got[1] != "2001:db8::1/128" || got[2] != "5.6.7.8/32" {
		t.Fatalf("cidrs %v", got)
	}
	if strings.Contains(string(b), "9.9.9.9") {
		t.Fatal("rule for a user not on the inbound")
	}
	// Only invalid entries -> no rule at all.
	in.Policy.BlockedIPs = map[string][]string{"7.alice": {"bogus"}}
	b, _ = SingboxConfig(in, 30555)
	if strings.Contains(string(b), "reject") {
		t.Fatal("rule from invalid IPs")
	}
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" {
		in.Policy.BlockedIPs = map[string][]string{"7.alice": {"1.2.3.4", "2001:db8::1"}}
		b, _ = SingboxConfig(in, 30555)
		f := filepath.Join(t.TempDir(), "sb.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("sing-box check: %v\n%s", err, out)
		}
	}
}

func TestSingboxLineHook(t *testing.T) {
	var got []string
	e := NewSingbox(Options{DataDir: t.TempDir(), OnLine: func(l string) {
		if l == "boom" {
			panic("boom")
		}
		got = append(got, l)
	}})
	hook := e.Supervisor.spec.LineHook
	if hook == nil {
		t.Fatal("no line hook")
	}
	if hook("main", 1, "a") || hook("main", 1, "boom") || hook("main", 1, "b") {
		t.Fatal("lines must stay in the log")
	}
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("got %v", got)
	}
	// Without a callback the hook is a no-op.
	if NewSingbox(Options{DataDir: t.TempDir()}).Supervisor.spec.LineHook("main", 1, "x") {
		t.Fatal("consumed")
	}
}

// Output lines reach the hook and the log without ANSI colors.
func TestSupervisorStripsANSI(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	hooked := make(chan string, 4)
	s := NewSupervisor(Spec{
		Name: "t",
		Bin:  func() string { return "/bin/sh" },
		Plan: func() ([]Instance, error) {
			return []Instance{{Key: "main", ConfigFile: filepath.Join(dir, "c"), Config: []byte("x"),
				Args: []string{"-c", `printf '\033[36mINFO\033[0m [\033[38;5;1m42\033[0m 0ms] hello\n'; exec sleep 30`}}}, nil
		},
		LineHook: func(_ string, _ uint64, l string) bool { hooked <- l; return false },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	select {
	case l := <-hooked:
		if l != "INFO [42 0ms] hello" {
			t.Fatalf("hook got %q", l)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no line")
	}
	for _, l := range s.Logs(0) {
		if strings.Contains(l, "\x1b") {
			t.Fatalf("escape in log %q", l)
		}
	}
}

// Each AmneziaWG inbound gets its own sidecar config (process), since
// amneziawg-go keeps the obfuscation headers process-wide.
func TestAWGConfigsPerInbound(t *testing.T) {
	in := input(t)
	ib := in.Inbounds[2]
	ib.ID, ib.Port = 9, ib.Port+1
	awg := *ib.Settings.AWG
	awg.H1++
	settings := ib.Settings
	settings.AWG = &awg
	ib.Settings = settings
	in.Inbounds = append(in.Inbounds, ib)
	list, owners, err := AWGConfigs(in)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %d", err, len(list))
	}
	seen := map[string]uint32{}
	for _, it := range list {
		var c awgd.Config
		if err := json.Unmarshal(it.Config, &c); err != nil || len(c.Interfaces) != 1 || c.Interfaces[0].Name != it.Iface {
			t.Fatalf("%s %v", it.Config, err)
		}
		seen[it.Iface] = uint32(c.Interfaces[0].Params.H1)
	}
	if seen["vawg3"] == 0 || seen["vawg9"] == 0 || seen["vawg3"] == seen["vawg9"] {
		t.Fatalf("headers %v", seen)
	}
	if len(owners) != 2 { // inbound 9 has no users
		t.Fatalf("owners %v", owners)
	}
	if list, _, _ := AWGConfigs(&xray.Input{}); len(list) != 0 {
		t.Fatal("no inbounds must mean no instances")
	}
}
