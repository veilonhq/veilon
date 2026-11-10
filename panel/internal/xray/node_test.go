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

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// nodeFixture: a VLESS Reality inbound, a VMess WS TLS inbound (with a
// certificate file on this server) and one user on both.
func nodeFixture(t *testing.T) (g *gorm.DB, dir string, reality, tls int64) {
	t.Helper()
	g = testutil.DB(t)
	dir = t.TempDir()
	cs := &certs.Service{DB: g, DataDir: dir}
	svc := &inbounds.Service{DB: g, SelfSigned: cs.SelfSigned}
	r, err := svc.Create(&inbounds.View{Remark: "VL", Enabled: true, Protocol: "vless", Port: 4443, Network: "tcp", Security: "reality",
		Flow: "xtls-rprx-vision", Reality: &inbounds.Reality{Dest: "www.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	certID, err := cs.SelfSigned("node.example.com")
	if err != nil {
		t.Fatal(err)
	}
	w, err := svc.Create(&inbounds.View{Remark: "VM", Enabled: true, Protocol: "vmess", Port: 8443, Network: "ws", Security: "tls",
		TLS: &inbounds.TLS{CertID: &certID, SNI: "node.example.com"}, Transport: inbounds.Transport{Path: "/ws"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	u := db.User{Username: "alice", Status: db.StatusActive, UUID: "8c5f8a2e-7a3c-4d5e-9f00-112233445566", Password: "pw", SubToken: "tok", CreatedAt: now, UpdatedAt: now}
	if err := g.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	g.Create(&db.UserInbound{UserID: u.ID, InboundID: r.ID})
	g.Create(&db.UserInbound{UserID: u.ID, InboundID: w.ID})
	return g, dir, r.ID, w.ID
}

func TestNodeConfigOnlyAssignedInboundsAndInlineCerts(t *testing.T) {
	gg, dir, rid, wid := nodeFixture(t)
	cfg, skipped, err := NodeConfig(gg, dir, []int64{rid, wid}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped: %v", skipped)
	}
	s := string(cfg)
	if strings.Contains(s, "certificateFile") || strings.Contains(s, "keyFile") || strings.Contains(s, dir) {
		t.Fatal("node config must not reference files on the master")
	}
	var c struct {
		Inbounds []struct {
			Tag  string `json:"tag"`
			Port int    `json:"port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	for _, ib := range c.Inbounds {
		tags[ib.Tag] = ib.Port
	}
	if tags["api"] != 62051 || len(c.Inbounds) != 3 {
		t.Fatalf("inbounds %v", tags)
	}
	if !strings.Contains(s, "BEGIN CERTIFICATE") || !strings.Contains(s, "PRIVATE KEY") || !strings.Contains(s, "1.alice") {
		t.Fatal("certificate or user missing")
	}
	// only the reality inbound
	one, _, err := NodeConfig(gg, dir, []int64{rid}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(one), "8443") || !strings.Contains(string(one), "4443") {
		t.Fatal("node config must hold only the assigned inbounds")
	}
	// none: just the stats API (what a disabled or removed node runs)
	none, _, _ := NodeConfig(gg, dir, nil, 62051)
	if strings.Contains(string(none), "4443") {
		t.Fatal("empty assignment must serve nothing")
	}
	// the same input renders the same bytes (the master compares hashes)
	again, _, _ := NodeConfig(gg, dir, []int64{rid, wid}, 62051)
	if string(again) != s {
		t.Fatal("node config is not deterministic")
	}

	if p := os.Getenv("VEILON_TEST_XRAY"); p != "" {
		f := filepath.Join(t.TempDir(), "node.json")
		_ = os.WriteFile(f, cfg, 0o600)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, p, "run", "-test", "-c", f)
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(p))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("xray rejected the node config: %v\n%s", err, out)
		}
	}
}
