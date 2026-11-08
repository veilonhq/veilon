package xray

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// fixtureAll returns an Input covering every protocol Xray serves, with a
// real self-signed certificate on disk.
func fixtureAll(t *testing.T) *Input {
	t.Helper()
	dir := t.TempDir()
	cp, kp, err := certs.GenerateSelfSigned("www.bing.com")
	if err != nil {
		t.Fatal(err)
	}
	cf, kf := certs.Paths(dir, 1)
	_ = os.MkdirAll(filepath.Dir(cf), 0o700)
	_ = os.WriteFile(cf, []byte(cp), 0o600)
	_ = os.WriteFile(kf, []byte(kp), 0o600)
	one := int64(1)
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Protocol, err)
		}
		if v.TLS != nil && v.TLS.CertID == nil {
			v.TLS.CertID = &one
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 1, Remark: "hy", Protocol: inbounds.Hysteria, Port: 30001, Sniffing: true, Settings: inbounds.Settings{ObfsPassword: "ob"}}),
		mk(inbounds.View{ID: 2, Remark: "wg", Protocol: inbounds.WireGuard, Port: 30002}),
		mk(inbounds.View{ID: 3, Remark: "http", Protocol: inbounds.HTTP, Port: 30003}),
		mk(inbounds.View{ID: 4, Remark: "https", Protocol: inbounds.HTTP, Port: 30004, Security: "tls", TLS: &inbounds.TLS{CertID: &one}}),
		mk(inbounds.View{ID: 5, Remark: "mixed", Protocol: inbounds.Mixed, Port: 30005, Settings: inbounds.Settings{UDP: true}}),
		mk(inbounds.View{ID: 6, Remark: "tunnel", Protocol: inbounds.Tunnel, Port: 30006, Settings: inbounds.Settings{TargetAddress: "1.1.1.1", TargetPort: 53, TunnelNetwork: "tcp,udp"}}),
		mk(inbounds.View{ID: 7, Remark: "tun", Protocol: inbounds.TUN, Settings: inbounds.Settings{Routes: []string{"10.200.0.0/24"}}}),
		mk(inbounds.View{ID: 8, Remark: "masque", Protocol: inbounds.Masque, Port: 30008}),
		mk(inbounds.View{ID: 9, Remark: "tuic", Protocol: inbounds.TUIC, Port: 30009}),
		mk(inbounds.View{ID: 10, Remark: "awg", Protocol: inbounds.AmneziaWG, Port: 30010}),
		mk(inbounds.View{ID: 11, Remark: "mtproto", Protocol: inbounds.MTProto, Port: 30011}),
	}
	u := db.User{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "pw7"}
	users := map[int64][]db.User{}
	for _, ib := range ibs {
		users[ib.ID] = []db.User{u}
	}
	priv, pub, _ := inbounds.GenerateWGKeys()
	_ = priv
	peers := map[int64]map[int64]db.WGPeer{2: {7: {InboundID: 2, UserID: 7, PublicKey: pub, PresharedKey: inbounds.GeneratePSK(), Address: "10.66.0.2/32"}}}
	return &Input{Inbounds: ibs, Users: users, Peers: peers, APIPort: 30099, BridgePort: 30098, DataDir: dir, AssetDir: dir, TunOK: true}
}

func inboundByTag(t *testing.T, b []byte) map[string]map[string]any {
	var cfg struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, ib := range cfg.Inbounds {
		out[ib["tag"].(string)] = ib
	}
	return out
}

func TestBuildAllProtocols(t *testing.T) {
	in := fixtureAll(t)
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Skipped) != 0 {
		t.Fatalf("skipped: %v", in.Skipped)
	}
	ibs := inboundByTag(t, b)
	// api + 8 xray inbounds + sing-box bridge; tuic/awg/mtproto run elsewhere.
	if len(ibs) != 10 {
		t.Fatalf("want 10 inbounds, got %d: %s", len(ibs), b)
	}
	for _, tag := range []string{"inbound-9", "inbound-10", "inbound-11"} {
		if _, ok := ibs[tag]; ok {
			t.Fatalf("%s must not be in the xray config", tag)
		}
	}
	hy := ibs["inbound-1"]
	if hy["protocol"] != "hysteria" {
		t.Fatal("hysteria protocol")
	}
	ss := hy["streamSettings"].(map[string]any)
	if ss["network"] != "hysteria" || ss["security"] != "tls" || ss["finalmask"] == nil {
		t.Fatalf("hysteria stream %v", ss)
	}
	c := hy["settings"].(map[string]any)["clients"].([]any)[0].(map[string]any)
	if c["auth"] != "pw7" || c["email"] != "7.alice" {
		t.Fatalf("hysteria client %v", c)
	}
	wg := ibs["inbound-2"]["settings"].(map[string]any)
	if wg["secretKey"] == "" || len(wg["peers"].([]any)) != 1 {
		t.Fatalf("wireguard %v", wg)
	}
	p := wg["peers"].([]any)[0].(map[string]any)
	if p["email"] != "7.alice" || p["allowedIPs"].([]any)[0] != "10.66.0.2/32" {
		t.Fatalf("wg peer %v", p)
	}
	for _, tag := range []string{"inbound-3", "inbound-4", "inbound-5"} {
		ib := ibs[tag]
		if ib["protocol"] != "mixed" {
			t.Fatalf("%s protocol %v", tag, ib["protocol"])
		}
		acc := ib["settings"].(map[string]any)["accounts"].([]any)[0].(map[string]any)
		if acc["user"] != "7.alice" || acc["pass"] != "pw7" {
			t.Fatalf("%s account %v", tag, acc)
		}
	}
	if ibs["inbound-3"]["settings"].(map[string]any)["udp"] != false || ibs["inbound-5"]["settings"].(map[string]any)["udp"] != true {
		t.Fatal("udp flags")
	}
	if ibs["inbound-4"]["streamSettings"].(map[string]any)["security"] != "tls" {
		t.Fatal("https proxy without tls")
	}
	tn := ibs["inbound-6"]["settings"].(map[string]any)
	if tn["rewriteAddress"] != "1.1.1.1" || tn["rewritePort"].(float64) != 53 || tn["allowedNetwork"] != "tcp,udp" {
		t.Fatalf("tunnel %v", tn)
	}
	tun := ibs["inbound-7"]
	if _, has := tun["port"]; has || tun["settings"].(map[string]any)["autoOutboundsInterface"] != "auto" {
		t.Fatalf("tun %v", tun)
	}
	mq := ibs["inbound-8"]
	if mq["settings"].(map[string]any)["clients"].([]any)[0].(map[string]any)["email"] != "7.alice" ||
		mq["streamSettings"].(map[string]any)["masqueSettings"].(map[string]any)["path"] != inbounds.MasqueDefaultPath {
		t.Fatalf("masque %v", mq)
	}
	br := ibs[BridgeTag]
	if br["listen"] != "127.0.0.1" || br["port"].(float64) != 30098 {
		t.Fatalf("bridge %v", br)
	}

	// TUN is skipped (not fatal) without CAP_NET_ADMIN.
	in.TunOK = false
	b2, _ := Build(in)
	if _, ok := inboundByTag(t, b2)["inbound-7"]; ok || len(in.Skipped) != 1 {
		t.Fatalf("tun not skipped: %v", in.Skipped)
	}

	// Validate with the real core when available: VEILON_TEST_XRAY=/path/to/xray
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(t.TempDir(), "config.json")
		_ = os.WriteFile(f, b, 0o600)
		out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput()
		if err != nil {
			t.Fatalf("xray -test failed: %v\n%s", err, out)
		}
		t.Logf("xray -test: OK")
	}
}
