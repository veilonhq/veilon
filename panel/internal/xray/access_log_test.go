package xray

import (
	"encoding/json"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// The connection guard (IP limits, speed limits, connection log) learns
// client IPs from Xray's access log, which Xray writes to stdout when the
// "access" key is absent; the manager feeds stdout to the guard. The config
// must keep it that way: a file path or "none" would starve the guard.
func TestConfigKeepsAccessLogOnStdout(t *testing.T) {
	dir := t.TempDir()
	ibs := []inbounds.View{{ID: 1, Remark: "a", Protocol: "vless", Port: 443, Network: "tcp", Security: "none"}}
	users := map[int64][]db.User{1: {{ID: 7, Username: "alice", UUID: "uuid-7", IPLimit: 1}}}
	b, err := Build(&Input{Inbounds: ibs, Users: users, APIPort: 10085, DataDir: dir, AssetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Log      map[string]any   `json:"log"`
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if a, ok := cfg.Log["access"]; ok && a != "" {
		t.Fatalf("access log redirected away from stdout: %v", a)
	}
	if cfg.Log["loglevel"] == "none" {
		t.Fatal("loglevel none")
	}
	c := cfg.Inbounds[1]["settings"].(map[string]any)["clients"].([]any)[0].(map[string]any)
	if c["email"] != "7.alice" { // guard.ObserveEmail parses "ID.name"
		t.Fatalf("email %v", c["email"])
	}
}
