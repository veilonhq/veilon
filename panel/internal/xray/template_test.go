package xray

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func buildWith(t *testing.T, tpl *Template, dir string) map[string]any {
	t.Helper()
	ibs := []inbounds.View{{ID: 1, Remark: "a", Protocol: "vless", Port: 443, Network: "tcp", Security: "none"}}
	users := map[int64][]db.User{1: {{ID: 7, Username: "alice", UUID: "c4f4e5a0-1d3c-4a52-9f5e-0b8e2f9b3a11"}}}
	b, err := Build(&Input{Inbounds: ibs, Users: users, APIPort: 10085, DataDir: dir, AssetDir: dir,
		Template: tpl, Usernames: map[string]string{"alice": "7.alice"}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDefaultTemplateKeepsLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	a := buildWith(t, nil, dir)
	b := buildWith(t, DefaultTemplate(), dir)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("default template changed the config:\n%s\n%s", ja, jb)
	}
	outs := a["outbounds"].([]any)
	if len(outs) != 2 || outs[0].(map[string]any)["tag"] != "direct" || outs[1].(map[string]any)["tag"] != "blocked" {
		t.Fatalf("unexpected outbounds %v", outs)
	}
	if a["routing"].(map[string]any)["domainStrategy"] != "AsIs" || a["log"].(map[string]any)["loglevel"] != "warning" {
		t.Fatal("defaults changed")
	}
}

const sampleTemplate = `{
  "log": {"loglevel": "info"},
  "dns": {"servers": ["1.1.1.1", {"address": "8.8.8.8", "domains": ["geosite:google"]}], "queryStrategy": "UseIPv4"},
  "outbounds": [
    {"tag": "warp", "protocol": "socks", "settings": {"servers": [{"address": "127.0.0.1", "port": 40000}]}},
    {"tag": "direct", "protocol": "freedom", "settings": {"domainStrategy": "UseIPv4"}},
    {"tag": "nl-1", "protocol": "socks", "settings": {"servers": [{"address": "127.0.0.1", "port": 40001}]}},
    {"tag": "nl-2", "protocol": "socks", "settings": {"servers": [{"address": "127.0.0.1", "port": 40002}]}, "_disabled": true}
  ],
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {"domain": ["geosite:openai", "chat.example.com"], "outboundTag": "warp", "_name": "ai"},
      {"user": ["alice"], "balancerTag": "nl"},
      {"user": ["ghost-only"], "outboundTag": "blocked", "_disabled": true},
      {"port": "25", "outboundTag": "blocked"},
      {"inboundTag": ["inbound-1"], "outboundTag": "nl-2"}
    ],
    "balancers": [{"tag": "nl", "selector": ["nl-"], "strategy": {"type": "leastPing"}, "fallbackTag": "direct"}]
  },
  "block_private": false
}`

func TestTemplateRender(t *testing.T) {
	tpl, err := ParseTemplate([]byte(sampleTemplate))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "geoip.dat"), []byte("x"), 0o644)
	cfg := buildWith(t, tpl, dir)
	outs := cfg["outbounds"].([]any)
	var tags []string
	for _, o := range outs {
		tags = append(tags, o.(map[string]any)["tag"].(string))
		if _, ok := o.(map[string]any)["_disabled"]; ok {
			t.Fatal("metadata leaked into the config")
		}
	}
	// warp first (default outbound), disabled nl-2 gone, blocked appended.
	if strings.Join(tags, ",") != "warp,direct,nl-1,blocked" {
		t.Fatalf("outbounds %v", tags)
	}
	r := cfg["routing"].(map[string]any)
	if r["domainStrategy"] != "IPIfNonMatch" {
		t.Fatal("domain strategy")
	}
	rules := r["rules"].([]any)
	js, _ := json.Marshal(rules)
	s := string(js)
	if strings.Contains(s, "geoip:private") {
		t.Fatal("block_private=false must drop the private rule")
	}
	if !strings.Contains(s, `"user":["7.alice"]`) {
		t.Fatalf("username not mapped to email: %s", s)
	}
	if strings.Contains(s, "ghost-only") || strings.Contains(s, `"nl-2"`) || strings.Contains(s, "_name") {
		t.Fatalf("disabled rule / rule to disabled outbound / metadata leaked: %s", s)
	}
	if rules[0].(map[string]any)["outboundTag"] != "api" {
		t.Fatal("api rule must stay first")
	}
	if _, ok := r["balancers"]; !ok {
		t.Fatal("balancers missing")
	}
	obs, ok := cfg["observatory"].(map[string]any)
	if !ok || obs["subjectSelector"].([]any)[0] != "nl-" {
		t.Fatalf("leastPing balancer needs an observatory: %v", cfg["observatory"])
	}
	if cfg["dns"].(map[string]any)["queryStrategy"] != "UseIPv4" {
		t.Fatal("dns missing")
	}
	if cfg["log"].(map[string]any)["loglevel"] != "info" {
		t.Fatal("log level")
	}
	// round trip through the editor format
	again, err := ParseTemplate(MarshalTemplate(tpl))
	if err != nil {
		t.Fatal(err)
	}
	if string(MarshalTemplate(again)) != string(MarshalTemplate(tpl)) {
		t.Fatal("template does not round-trip")
	}
}

func TestTemplateValidation(t *testing.T) {
	bad := map[string]string{
		"unknown key":      `{"outbounds":[],"nope":1}`,
		"reserved tag":     `{"outbounds":[{"tag":"api","protocol":"freedom"}]}`,
		"duplicate":        `{"outbounds":[{"tag":"a","protocol":"freedom"},{"tag":"a","protocol":"freedom"}]}`,
		"bad protocol":     `{"outbounds":[{"tag":"a","protocol":"hysteria9"}]}`,
		"direct proto":     `{"outbounds":[{"tag":"direct","protocol":"socks"}]}`,
		"rule no target":   `{"routing":{"rules":[{"domain":["x.com"]}]}}`,
		"rule bad target":  `{"routing":{"rules":[{"domain":["x.com"],"outboundTag":"nope"}]}}`,
		"rule no matcher":  `{"routing":{"rules":[{"outboundTag":"direct"}]}}`,
		"rule empty list":  `{"routing":{"rules":[{"domain":[],"outboundTag":"direct"}]}}`,
		"balancer empty":   `{"routing":{"balancers":[{"tag":"b","selector":[]}]}}`,
		"balancer nomatch": `{"routing":{"balancers":[{"tag":"b","selector":["zz"]}]}}`,
		"bad loglevel":     `{"log":{"loglevel":"loud"}}`,
		"bad strategy":     `{"routing":{"domainStrategy":"Fast"}}`,
		"bad tag chars":    `{"outbounds":[{"tag":"a b","protocol":"freedom"}]}`,
	}
	for name, doc := range bad {
		if _, err := ParseTemplate([]byte(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// reserved outbounds are added when missing
	tpl, err := ParseTemplate([]byte(`{"outbounds":[{"tag":"w","protocol":"freedom"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tpl.Outbounds) != 3 {
		t.Fatalf("direct/blocked not added: %v", tpl.Outbounds)
	}
}

func TestParseShareLinks(t *testing.T) {
	cases := map[string]string{
		"vless://c4f4e5a0-1d3c-4a52-9f5e-0b8e2f9b3a11@1.2.3.4:443?encryption=none&security=reality&sni=www.example.com&fp=chrome&pbk=PUBKEY&sid=ab12&type=tcp&flow=xtls-rprx-vision#My Server": "vless",
		"vless://c4f4e5a0-1d3c-4a52-9f5e-0b8e2f9b3a11@ex.com:443?security=tls&type=ws&path=%2Fws&host=ex.com#ws":                                                                               "vless",
		"trojan://secret@ex.com:443?type=grpc&serviceName=svc&sni=ex.com#t":                                                                                                                    "trojan",
		"ss://YWVzLTI1Ni1nY206cGFzcw@1.2.3.4:8388#ss":                                                                                                                                          "shadowsocks",
		"ss://2022-blake3-aes-128-gcm:a2V5:dXNlcg@1.2.3.4:8388#ss22":                                                                                                                           "shadowsocks",
		"socks://dXNlcjpwYXNz@1.2.3.4:1080#s":                                                                                                                                                  "socks",
		"http://u:p@1.2.3.4:8080":                                                                                                                                                              "http",
		"wireguard://cHJpdg%3D%3D@1.2.3.4:51820?publickey=UFVC&address=10.0.0.2%2F32&reserved=1,2,3&mtu=1280#wg":                                                                               "wireguard",
		"vmess://eyJ2IjoiMiIsInBzIjoidm0iLCJhZGQiOiIxLjIuMy40IiwicG9ydCI6IjQ0MyIsImlkIjoiYzRmNGU1YTAtMWQzYy00YTUyLTlmNWUtMGI4ZTJmOWIzYTExIiwibmV0Ijoid3MiLCJwYXRoIjoiL3dzIiwidGxzIjoidGxzIn0=": "vmess",
	}
	for link, proto := range cases {
		o, err := ParseShareLink(link, "")
		if err != nil {
			t.Errorf("%s: %v", link, err)
			continue
		}
		if o["protocol"] != proto || o["tag"] == "" {
			t.Errorf("%s: got %v", link, o)
		}
		if _, err := ParseTemplate(mustJSON(t, map[string]any{"outbounds": []any{o}})); err != nil {
			t.Errorf("%s: parsed outbound rejected: %v", link, err)
		}
	}
	o, _ := ParseShareLink("vless://c4f4e5a0-1d3c-4a52-9f5e-0b8e2f9b3a11@1.2.3.4:443?security=reality&pbk=K&sid=ab&sni=a.com#My Server", "")
	if o["tag"] != "My-Server" {
		t.Fatalf("tag from name: %v", o["tag"])
	}
	rs := o["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
	if rs["publicKey"] != "K" || rs["shortId"] != "ab" || rs["serverName"] != "a.com" {
		t.Fatalf("reality: %v", rs)
	}
	for _, bad := range []string{"", "hello", "vless://@:0", "tuic://x@y:1", "vmess://notbase64!!"} {
		if _, err := ParseShareLink(bad, ""); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// realXray returns an Xray binary for end-to-end checks, if one is around
// (VEILON_TEST_XRAY or xray on PATH).
func realXray(t *testing.T) string {
	if p := os.Getenv("VEILON_TEST_XRAY"); p != "" {
		return p
	}
	if p, err := exec.LookPath("xray"); err == nil {
		return p
	}
	t.Skip("no xray binary (set VEILON_TEST_XRAY)")
	return ""
}

// TestTemplateWithRealXray renders the sample template and lets real Xray
// check it, then applies a bad one and expects it to be refused while the
// saved template stays.
func TestTemplateWithRealXray(t *testing.T) {
	src := realXray(t)
	gdb := testutil.DB(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray", "xray")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	b, _ := os.ReadFile(src)
	_ = os.WriteFile(bin, b, 0o755)
	for _, f := range []string{"geoip.dat", "geosite.dat"} {
		if d, err := os.ReadFile(filepath.Join(filepath.Dir(src), f)); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "xray", f), d, 0o644)
		}
	}
	m := NewManager(Options{DB: gdb, DataDir: dir, BinPath: bin, APIPort: 1})
	tpl, err := ParseTemplate([]byte(sampleTemplate))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if r := m.TestTemplate(ctx, tpl); !r.OK || !r.Tested {
		t.Fatalf("sample template rejected by xray: %+v", r)
	}
	// Xray-level error that the panel's validation does not know about.
	bad, _ := ParseTemplate([]byte(`{"outbounds":[{"tag":"x","protocol":"vless","settings":{"vnext":"oops"}}]}`))
	r, err := m.ApplyTemplate(ctx, bad)
	if err != nil || r.OK || r.Applied || r.Error == "" {
		t.Fatalf("bad template should be refused: %+v %v", r, err)
	}
	t.Logf("xray said: %s", r.Error)
	if db.GetSetting(gdb, KeyTemplate) != "" {
		t.Fatal("refused template must not be saved")
	}
}

// A template that passes "xray run -test" but makes Xray exit at runtime is
// rolled back: the previous template is restored and Xray runs again.
func TestApplyTemplateRollsBack(t *testing.T) {
	old := verifyWindow
	verifyWindow = 700 * time.Millisecond
	defer func() { verifyWindow = old }()
	gdb := testutil.DB(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "xray")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo \"Xray 1.0.0 (fake)\"; exit 0; fi\nif [ \"$2\" = -test ]; then exit 0; fi\nif grep -q crashme \"$3\"; then echo boom; exit 1; fi\nexec sleep 60\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(Options{DB: gdb, DataDir: dir, BinPath: bin, APIPort: 1, Debounce: 50 * time.Millisecond, StatsEvery: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, "start", m.isRunning)

	good, _ := ParseTemplate([]byte(`{"outbounds":[{"tag":"w1","protocol":"freedom"}]}`))
	r, err := m.ApplyTemplate(ctx, good)
	if err != nil || !r.OK || !r.Applied {
		t.Fatalf("good template: %+v %v", r, err)
	}
	saved := db.GetSetting(gdb, KeyTemplate)
	if !strings.Contains(saved, "w1") {
		t.Fatal("good template not saved")
	}
	bad, _ := ParseTemplate([]byte(`{"outbounds":[{"tag":"crashme","protocol":"freedom"}]}`))
	r, err = m.ApplyTemplate(ctx, bad)
	if err != nil || r.OK || !r.RolledBack {
		t.Fatalf("expected rollback: %+v %v", r, err)
	}
	if db.GetSetting(gdb, KeyTemplate) != saved {
		t.Fatal("previous template not restored")
	}
	waitFor(t, "running after rollback", m.isRunning)
	cfg, _ := os.ReadFile(m.ConfigPath())
	if strings.Contains(string(cfg), "crashme") {
		t.Fatal("config on disk still has the bad template")
	}
}
