package xray

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// VLESS inbounds carry the server "decryption" string when VLESS
// Encryption is on and "none" otherwise; Vision stays on the clients when
// chosen (also over ws with encryption).
func TestBuildVLESSEncryption(t *testing.T) {
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Remark, err)
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 1, Remark: "x", Protocol: inbounds.VLESS, Port: 32001, Flow: "xtls-rprx-vision", VLESSEnc: &inbounds.VLESSEnc{Enabled: true}}),
		mk(inbounds.View{ID: 2, Remark: "pq", Protocol: inbounds.VLESS, Port: 32002, Network: "ws", Flow: "xtls-rprx-vision",
			VLESSEnc: &inbounds.VLESSEnc{Enabled: true, Auth: inbounds.EncAuthMLKEM, Mode: "xorpub", RTT: "1rtt"}}),
		mk(inbounds.View{ID: 3, Remark: "off", Protocol: inbounds.VLESS, Port: 32003}),
	}
	u := db.User{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "pw7"}
	users := map[int64][]db.User{1: {u}, 2: {u}, 3: {u}}
	dir := t.TempDir()
	b, err := Build(&Input{Inbounds: ibs, Users: users, APIPort: 32099, DataDir: dir, AssetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, x := range cfg["inbounds"].([]any) {
		m := x.(map[string]any)
		if tag, _ := m["tag"].(string); tag != "api" {
			got[tag] = m["settings"].(map[string]any)
		}
	}
	if d := got["inbound-1"]["decryption"]; d != ibs[0].VLESSEnc.Decryption() || d == "none" {
		t.Fatalf("inbound 1 decryption %v", d)
	}
	if d := got["inbound-2"]["decryption"].(string); d != "mlkem768x25519plus.xorpub.0s."+ibs[1].VLESSEnc.PrivateKey {
		t.Fatalf("inbound 2 decryption %v", d)
	}
	if d := got["inbound-3"]["decryption"]; d != "none" {
		t.Fatalf("inbound 3 decryption %v", d)
	}
	c := got["inbound-2"]["clients"].([]any)[0].(map[string]any)
	if c["flow"] != "xtls-rprx-vision" {
		t.Fatalf("vision client %v", c)
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(dir, "c.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("xray -test: %v\n%s", err, out)
		}
	}
}

// Nodes get the same server "decryption" in their pushed config, and the
// node version check refuses an old node Xray.
func TestNodeConfigVLESSEncryption(t *testing.T) {
	gg, dir, rid, _ := nodeFixture(t)
	g := &inbounds.Service{DB: gg}
	n := db.Node{Name: "edge", Address: "10.0.0.2", APIPort: 62050, Enabled: true}
	gg.Create(&n)
	v, err := g.Get(rid)
	if err != nil {
		t.Fatal(err)
	}
	v.VLESSEnc = &inbounds.VLESSEnc{Enabled: true, Auth: inbounds.EncAuthMLKEM, Mode: "xorpub"}
	v.NodeIDs = []int64{n.ID}
	g.XrayVersion = func(id int64) string {
		if id == n.ID {
			return "25.8.31"
		}
		return "26.10.31"
	}
	if _, err := g.Update(v); err == nil || !strings.Contains(err.Error(), `node "edge"`) {
		t.Fatalf("old node xray accepted: %v", err)
	}
	g.XrayVersion = func(int64) string { return "26.11.9" }
	v.NodeIDs = []int64{n.ID}
	out, err := g.Update(v)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := NodeConfig(gg, dir, []int64{rid}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(cfg, &m)
	for _, x := range m["inbounds"].([]any) {
		ib := x.(map[string]any)
		if ib["tag"] == "inbound-"+strconv.FormatInt(rid, 10) {
			if d := ib["settings"].(map[string]any)["decryption"]; d != out.VLESSEnc.Decryption() || !strings.HasPrefix(d.(string), "mlkem768x25519plus.xorpub.600s.") {
				t.Fatalf("node decryption %v", d)
			}
			return
		}
	}
	t.Fatal("inbound missing from node config")
}
