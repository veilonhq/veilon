package sub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// goldenSubInbounds are inbounds as stored before the advanced settings
// existed (fixed values, no Normalize).
func goldenSubInbounds() []inbounds.View {
	cid := int64(1)
	tlsOf := func(insecure bool, alpn ...string) *inbounds.TLS {
		if alpn == nil {
			alpn = []string{}
		}
		return &inbounds.TLS{CertID: &cid, SNI: "g.example.com", ALPN: alpn, Fingerprint: "chrome", AllowInsecure: insecure}
	}
	re := &inbounds.Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com"},
		PrivateKey: "priv", PublicKey: "PUBKEY", ShortIDs: []string{"0123456789abcdef"}, Fingerprint: "chrome", SpiderX: "/"}
	vOff := &inbounds.VLESSEnc{Auth: "x25519", Mode: "native", RTT: "0rtt", TicketSeconds: 600}
	return []inbounds.View{
		{ID: 1, Remark: "vless-reality", Protocol: "vless", Port: 41001, Network: "tcp", Security: "reality", Reality: re, Flow: "xtls-rprx-vision", VLESSEnc: vOff},
		{ID: 2, Remark: "vless-tls-ws", Protocol: "vless", Port: 41002, Network: "ws", Security: "tls", TLS: tlsOf(false, "http/1.1"), Transport: inbounds.Transport{Path: "/ws", Host: "h.example.com"}, VLESSEnc: vOff},
		{ID: 3, Remark: "vless-xhttp", Protocol: "vless", Port: 41003, Network: "xhttp", Security: "tls", TLS: tlsOf(true, "h2"), Transport: inbounds.Transport{Path: "/x", Mode: "auto"}, VLESSEnc: vOff},
		{ID: 4, Remark: "vmess-grpc", Protocol: "vmess", Port: 41004, Network: "grpc", Security: "tls", TLS: tlsOf(true), Transport: inbounds.Transport{ServiceName: "gs", Mode: "multi"}},
		{ID: 5, Remark: "trojan-tcp", Protocol: "trojan", Port: 41005, Network: "tcp", Security: "tls", TLS: tlsOf(false, "h2")},
		{ID: 6, Remark: "ss-2022", Protocol: "shadowsocks", Port: 41006, Network: "tcp", Security: "none", SSMethod: "2022-blake3-aes-128-gcm", SSServerKey: "AAAAAAAAAAAAAAAAAAAAAA=="},
		{ID: 7, Remark: "ss-aead", Protocol: "shadowsocks", Port: 41007, Network: "tcp", Security: "none", SSMethod: "chacha20-ietf-poly1305"},
		{ID: 8, Remark: "vmess-hu", Protocol: "vmess", Port: 41008, Network: "httpupgrade", Security: "none", Transport: inbounds.Transport{Path: "/hu", Host: "hu.example.com"}},
		{ID: 9, Remark: "trojan-reality-grpc", Protocol: "trojan", Port: 41009, Network: "grpc", Security: "reality", Reality: re, Transport: inbounds.Transport{ServiceName: "grpc"}},
		{ID: 10, Remark: "hy2", Protocol: "hysteria", Port: 41010, Network: "udp", Security: "tls", TLS: tlsOf(true, "h3"), Settings: inbounds.Settings{ObfsPassword: "ob", UpMbps: 50, DownMbps: 100}},
		{ID: 11, Remark: "tuic", Protocol: "tuic", Port: 41011, Network: "udp", Security: "tls", TLS: tlsOf(false, "h3"), Settings: inbounds.Settings{Congestion: "bbr"}},
		{ID: 12, Remark: "socks", Protocol: "socks", Port: 41012, Network: "tcp", Security: "none", Settings: inbounds.Settings{UDP: true}},
	}
}

func goldenSub(t *testing.T) string {
	t.Helper()
	ps := BuildProxiesX(user(), goldenSubInbounds(), db.Settings{ServerAddress: "srv.example.com"}, "h", Extras{Pins: map[int64]certs.Pins{1: {CertSHA256: "ab", SPKISHA256: "cd"}}})
	var b strings.Builder
	for i := range ps {
		b.WriteString(URI(&ps[i]) + "\n")
	}
	c, err := Clash(ps)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Singbox(ps)
	if err != nil {
		t.Fatal(err)
	}
	x, err := XrayJSON(ps)
	if err != nil {
		t.Fatal(err)
	}
	return b.String() + "----\n" + string(c) + "----\n" + string(s) + "\n----\n" + string(x) + "\n"
}

// TestGoldenOldSubscription: subscriptions of inbounds stored before the
// advanced settings existed stay byte for byte the same.
func TestGoldenOldSubscription(t *testing.T) {
	got := goldenSub(t)
	f := filepath.Join("testdata", "golden_pre_advanced.txt")
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
		t.Fatalf("subscription of old inbounds changed:\n%s", got)
	}
}
