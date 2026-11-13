package xray

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// goldenInbounds are stored inbounds as an older panel wrote them (no
// advanced settings). Their server config must never change: an upgrade
// may not touch running inbounds. Values are fixed (no Normalize, which
// would generate keys).
func goldenInbounds() []inbounds.View {
	cid := int64(1)
	tlsOf := func(alpn ...string) *inbounds.TLS {
		if alpn == nil {
			alpn = []string{}
		}
		return &inbounds.TLS{CertID: &cid, SNI: "g.example.com", ALPN: alpn, Fingerprint: "chrome"}
	}
	re := &inbounds.Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com"},
		PrivateKey: "SDmCRAdbBnOV4lqtd25MsJfY5rR2e_AJrJs-nEBeFkU", PublicKey: "pub", ShortIDs: []string{"0123456789abcdef"}, Fingerprint: "chrome", SpiderX: "/"}
	vOff := &inbounds.VLESSEnc{Auth: "x25519", Mode: "native", RTT: "0rtt", TicketSeconds: 600}
	ibs := []inbounds.View{
		{ID: 1, Remark: "vless-reality", Protocol: "vless", Port: 41001, Network: "tcp", Security: "reality", Reality: re, Flow: "xtls-rprx-vision", VLESSEnc: vOff, Sniffing: true},
		{ID: 2, Remark: "vless-tls-ws", Protocol: "vless", Port: 41002, Network: "ws", Security: "tls", TLS: tlsOf("http/1.1"), Transport: inbounds.Transport{Path: "/ws", Host: "h.example.com"}, VLESSEnc: vOff, Sniffing: true},
		{ID: 3, Remark: "vless-xhttp", Protocol: "vless", Port: 41003, Network: "xhttp", Security: "tls", TLS: tlsOf("h2", "http/1.1"), Transport: inbounds.Transport{Path: "/x", Mode: "auto"}, VLESSEnc: vOff},
		{ID: 4, Remark: "vmess-grpc", Protocol: "vmess", Port: 41004, Network: "grpc", Security: "tls", TLS: tlsOf(), Transport: inbounds.Transport{ServiceName: "gs", Mode: "multi"}, Sniffing: true},
		{ID: 5, Remark: "trojan-tcp", Protocol: "trojan", Port: 41005, Network: "tcp", Security: "tls", TLS: tlsOf("h2"), Sniffing: true},
		{ID: 6, Remark: "ss-2022", Protocol: "shadowsocks", Port: 41006, Network: "tcp", Security: "none", SSMethod: "2022-blake3-aes-128-gcm", SSServerKey: "AAAAAAAAAAAAAAAAAAAAAA==", Sniffing: true},
		{ID: 7, Remark: "ss-aead", Protocol: "shadowsocks", Port: 41007, Network: "tcp", Security: "none", SSMethod: "chacha20-ietf-poly1305"},
		{ID: 8, Remark: "vmess-hu", Protocol: "vmess", Port: 41008, Network: "httpupgrade", Security: "none", Transport: inbounds.Transport{Path: "/hu"}},
		{ID: 9, Remark: "trojan-reality-grpc", Protocol: "trojan", Port: 41009, Network: "grpc", Security: "reality", Reality: re, Transport: inbounds.Transport{ServiceName: "grpc"}},
		{ID: 10, Remark: "hy2", Protocol: "hysteria", Port: 41010, Network: "udp", Security: "tls", TLS: tlsOf("h3"), Settings: inbounds.Settings{ObfsPassword: "ob", UpMbps: 50, DownMbps: 100}, Sniffing: true},
		{ID: 11, Remark: "mixed", Protocol: "mixed", Port: 41011, Network: "tcp", Security: "none", Settings: inbounds.Settings{UDP: true}, Sniffing: true},
		{ID: 12, Remark: "socks", Protocol: "socks", Port: 41012, Network: "tcp", Security: "none", Settings: inbounds.Settings{UDP: true}},
		{ID: 13, Remark: "https", Protocol: "http", Port: 41013, Network: "tcp", Security: "tls", TLS: tlsOf()},
	}
	return ibs
}

func goldenBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cf, kf := certs.Paths(dir, 1)
	_ = os.MkdirAll(filepath.Dir(cf), 0o700)
	_ = os.WriteFile(cf, []byte("cert"), 0o600)
	_ = os.WriteFile(kf, []byte("key"), 0o600)
	ibs := goldenInbounds()
	u := db.User{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "pw7"}
	users := map[int64][]db.User{}
	for _, ib := range ibs {
		users[ib.ID] = []db.User{u}
	}
	b, err := Build(&Input{Inbounds: ibs, Users: users, APIPort: 41099, DataDir: dir, AssetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), dir, "@DIR@")
}

// TestGoldenOldInbounds: inbounds stored before the advanced settings
// existed render the same Xray config byte for byte (testdata written by
// the panel before that change; VEILON_WRITE_GOLDEN=1 rewrites it).
func TestGoldenOldInbounds(t *testing.T) {
	got := goldenBuild(t)
	f := filepath.Join("testdata", "golden_pre_advanced.json")
	if os.Getenv("VEILON_WRITE_GOLDEN") == "1" {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(f, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("config of old inbounds changed:\n%s", got)
	}
	var x any
	if json.Unmarshal([]byte(got), &x) != nil {
		t.Fatal("not JSON")
	}
}
