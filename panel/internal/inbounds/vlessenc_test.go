package inbounds

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func encView(e *VLESSEnc) View {
	return View{Remark: "v", Protocol: VLESS, Port: 443, VLESSEnc: e}
}

func TestVLESSEncDefaultsOff(t *testing.T) {
	v := norm(t, View{Remark: "v", Protocol: VLESS, Port: 443})
	e := v.VLESSEnc
	if e == nil || e.Enabled || e.Auth != EncAuthX25519 || e.Mode != "native" || e.RTT != "0rtt" || e.TicketSeconds != 600 {
		t.Fatalf("default %+v", e)
	}
	if e.PrivateKey != "" || e.Encryption != "" || e.Decryption() != "none" || e.ClientEncryption() != "none" {
		t.Fatalf("an off setting has no keys and keeps decryption none: %+v", e)
	}
	tr := norm(t, View{Remark: "t", Protocol: Trojan, Port: 443, VLESSEnc: &VLESSEnc{Enabled: true}})
	if tr.VLESSEnc != nil {
		t.Fatal("trojan must not keep a VLESS Encryption setting")
	}
	h := norm(t, View{Remark: "h", Protocol: Hysteria, Port: 443, VLESSEnc: &VLESSEnc{Enabled: true}})
	if h.VLESSEnc != nil {
		t.Fatal("hysteria must not keep a VLESS Encryption setting")
	}
}

func TestVLESSEncX25519(t *testing.T) {
	v := norm(t, encView(&VLESSEnc{Enabled: true}))
	e := v.VLESSEnc
	raw, err := base64.RawURLEncoding.DecodeString(e.PrivateKey)
	if err != nil || len(raw) != 32 {
		t.Fatalf("x25519 private key %q", e.PrivateKey)
	}
	pub, _ := base64.RawURLEncoding.DecodeString(e.PublicKey)
	if len(pub) != 32 {
		t.Fatalf("x25519 public key %q", e.PublicKey)
	}
	if got, want := e.Decryption(), "mlkem768x25519plus.native.600s."+e.PrivateKey; got != want {
		t.Fatalf("decryption %q", got)
	}
	if got, want := e.ClientEncryption(), "mlkem768x25519plus.native.0rtt."+e.PublicKey; got != want || e.Encryption != want {
		t.Fatalf("encryption %q / %q", got, e.Encryption)
	}
	// Same key: same public key, settings changes keep it.
	v2 := norm(t, encView(&VLESSEnc{Enabled: true, PrivateKey: e.PrivateKey, Mode: "xorpub", RTT: "1rtt", TicketSeconds: 900}))
	e2 := v2.VLESSEnc
	if e2.PublicKey != e.PublicKey || e2.TicketSeconds != 0 {
		t.Fatalf("re-normalized %+v", e2)
	}
	if !strings.HasPrefix(e2.Decryption(), "mlkem768x25519plus.xorpub.0s.") || !strings.HasPrefix(e2.ClientEncryption(), "mlkem768x25519plus.xorpub.1rtt.") {
		t.Fatalf("1rtt strings %q %q", e2.Decryption(), e2.ClientEncryption())
	}
	// Off keeps the keys (switching back on gives the same links).
	v3 := norm(t, encView(&VLESSEnc{Enabled: false, PrivateKey: e.PrivateKey}))
	if v3.VLESSEnc.PublicKey != e.PublicKey || v3.VLESSEnc.Encryption != "" || v3.VLESSEnc.Decryption() != "none" {
		t.Fatalf("off %+v", v3.VLESSEnc)
	}
}

func TestVLESSEncMLKEM(t *testing.T) {
	v := norm(t, encView(&VLESSEnc{Enabled: true, Auth: EncAuthMLKEM, Mode: "random", TicketSeconds: 300}))
	e := v.VLESSEnc
	seed, _ := base64.RawURLEncoding.DecodeString(e.PrivateKey)
	pub, _ := base64.RawURLEncoding.DecodeString(e.PublicKey)
	if len(seed) != 64 || len(pub) != 1184 {
		t.Fatalf("mlkem key sizes %d %d", len(seed), len(pub))
	}
	if !strings.HasPrefix(e.Decryption(), "mlkem768x25519plus.random.300s.") {
		t.Fatalf("decryption %q", e.Decryption())
	}
	// An X25519 key is refused for ML-KEM and the other way round.
	x := norm(t, encView(&VLESSEnc{Enabled: true})).VLESSEnc.PrivateKey
	if err := Normalize(&View{Remark: "v", Protocol: VLESS, Port: 443, VLESSEnc: &VLESSEnc{Enabled: true, Auth: EncAuthMLKEM, PrivateKey: x}}); err == nil {
		t.Fatal("x25519 key accepted as ML-KEM seed")
	}
	if err := Normalize(&View{Remark: "v", Protocol: VLESS, Port: 443, VLESSEnc: &VLESSEnc{Enabled: true, Auth: EncAuthX25519, PrivateKey: e.PrivateKey}}); err == nil {
		t.Fatal("ML-KEM seed accepted as x25519 key")
	}
}

func TestVLESSEncImportAndErrors(t *testing.T) {
	k, err := GenerateVLESSEncKeys("", EncAuthMLKEM)
	if err != nil {
		t.Fatal(err)
	}
	v := norm(t, encView(&VLESSEnc{Enabled: true, PrivateKey: "mlkem768x25519plus.xorpub.300-600s.100-111-1111." + k.PrivateKey}))
	e := v.VLESSEnc
	if e.Auth != EncAuthMLKEM || e.Mode != "xorpub" || e.RTT != "0rtt" || e.TicketSeconds != 600 || e.PrivateKey != k.PrivateKey || e.PublicKey != k.PublicKey {
		t.Fatalf("import %+v", e)
	}
	v = norm(t, encView(&VLESSEnc{Enabled: true, PrivateKey: "mlkem768x25519plus.native.0s." + norm(t, encView(&VLESSEnc{Enabled: true})).VLESSEnc.PrivateKey}))
	if v.VLESSEnc.RTT != "1rtt" || v.VLESSEnc.Auth != EncAuthX25519 {
		t.Fatalf("import 0s %+v", v.VLESSEnc)
	}
	for name, e := range map[string]*VLESSEnc{
		"mode":      {Enabled: true, Mode: "plain"},
		"rtt":       {Enabled: true, RTT: "2rtt"},
		"auth":      {Enabled: true, Auth: "rsa"},
		"ticket":    {Enabled: true, TicketSeconds: MaxTicketSeconds + 1},
		"negative":  {Enabled: true, TicketSeconds: -5},
		"key":       {Enabled: true, PrivateKey: "not-a-key"},
		"two keys":  {Enabled: true, PrivateKey: "mlkem768x25519plus.native.600s." + k.PrivateKey + "." + k.PrivateKey},
		"bad block": {Enabled: true, PrivateKey: "mlkem768x25519plus.native.0rtt." + k.PrivateKey},
	} {
		if err := Normalize(&View{Remark: "v", Protocol: VLESS, Port: 443, VLESSEnc: e}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Vision needs tcp + TLS/Reality, or VLESS Encryption (any transport).
func TestVLESSEncVisionRule(t *testing.T) {
	for _, net := range []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"} {
		v := View{Remark: "v", Protocol: VLESS, Port: 443, Network: net, Flow: "xtls-rprx-vision", VLESSEnc: &VLESSEnc{Enabled: true}}
		if err := Normalize(&v); err != nil {
			t.Errorf("vision + encryption over %s refused: %v", net, err)
		}
	}
	if err := Normalize(&View{Remark: "v", Protocol: VLESS, Port: 443, Flow: "xtls-rprx-vision"}); err == nil {
		t.Fatal("vision without tls/reality/encryption accepted")
	}
	if err := Normalize(&View{Remark: "v", Protocol: VLESS, Port: 443, Network: "ws", Flow: "xtls-rprx-vision", VLESSEnc: &VLESSEnc{}}); err == nil {
		t.Fatal("vision over ws with encryption off accepted")
	}
	// Flow off with encryption on is fine (the per-inbound switch).
	v := norm(t, View{Remark: "v", Protocol: VLESS, Port: 443, Network: "ws", VLESSEnc: &VLESSEnc{Enabled: true}})
	if v.Flow != "" || !v.VLESSEnc.On() {
		t.Fatalf("no flow %+v", v)
	}
}

// The setting is stored in the settings column and read back the same.
func TestVLESSEncStoredRoundTrip(t *testing.T) {
	v := norm(t, encView(&VLESSEnc{Enabled: true, Auth: EncAuthMLKEM, Mode: "xorpub"}))
	var m db.Inbound
	toModel(&v, &m)
	if !strings.Contains(m.Settings, `"vless_encryption"`) {
		t.Fatalf("settings column %s", m.Settings)
	}
	back := FromModel(&m)
	if *back.VLESSEnc != *v.VLESSEnc {
		t.Fatalf("round trip\n%+v\n%+v", back.VLESSEnc, v.VLESSEnc)
	}
	// Rows written before the feature: off, no keys.
	old := FromModel(&db.Inbound{Protocol: VLESS, Settings: `{"dns":[]}`})
	if old.VLESSEnc == nil || old.VLESSEnc.Enabled || old.VLESSEnc.Decryption() != "none" {
		t.Fatalf("old row %+v", old.VLESSEnc)
	}
	// Other protocols never store it.
	tr := norm(t, View{Remark: "t", Protocol: VMess, Port: 80, Network: "ws"})
	var m2 db.Inbound
	toModel(&tr, &m2)
	if strings.Contains(m2.Settings, "vless_encryption") {
		t.Fatalf("vmess settings %s", m2.Settings)
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		v    string
		want bool
	}{
		{"26.10.31", true}, {"25.9.5", true}, {"25.9.4", false}, {"25.8.31", false}, {"v25.10.1", true},
		{"1.8.24", false}, {"", true}, {"dev", true}, {"25.9", false}, {"25.12.8", true},
	} {
		if got := VersionAtLeast(c.v, MinVLESSEncXray); got != c.want {
			t.Errorf("%q: %v", c.v, got)
		}
	}
}

// writeFakeXray writes a script that prints a fixed key pair the way
// `xray x25519` / `xray mlkem768` do.
func writeFakeXray(t *testing.T, x25519Priv, x25519Pub, seed, client string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "xray")
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"x25519) printf 'PrivateKey: %s\\nPassword (PublicKey): %s\\nHash32: zz\\n' '" + x25519Priv + "' '" + x25519Pub + "';;\n" +
		"mlkem768) printf 'Seed: %s\\nClient: %s\\nHash32: zz\\n' '" + seed + "' '" + client + "';;\n" +
		"*) exit 1;;\nesac\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGenerateVLESSEncKeysFromBinary(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	x, _ := GenerateVLESSEncKeys("", EncAuthX25519)
	m, _ := GenerateVLESSEncKeys("", EncAuthMLKEM)
	bin := writeFakeXray(t, x.PrivateKey, x.PublicKey, m.PrivateKey, m.PublicKey)
	for _, want := range []VLESSEncKeys{x, m} {
		got, err := GenerateVLESSEncKeys(bin, want.Auth)
		if err != nil || got.Source != "xray" || got.PrivateKey != want.PrivateKey || got.PublicKey != want.PublicKey {
			t.Fatalf("%s from binary: %+v %v", want.Auth, got, err)
		}
	}
	// A binary whose pair does not match is not trusted.
	bad := writeFakeXray(t, x.PrivateKey, m.PublicKey[:43], m.PrivateKey, x.PublicKey)
	for _, auth := range EncAuths {
		got, err := GenerateVLESSEncKeys(bad, auth)
		if err != nil || got.Source != "go" || got.PrivateKey == x.PrivateKey || got.PrivateKey == m.PrivateKey {
			t.Fatalf("%s mismatched binary: %+v %v", auth, got, err)
		}
	}
	// No binary: Go.
	got, err := GenerateVLESSEncKeys(filepath.Join(t.TempDir(), "missing"), EncAuthMLKEM)
	if err != nil || got.Source != "go" {
		t.Fatalf("missing binary %+v %v", got, err)
	}
	if _, err := GenerateVLESSEncKeys("", "rsa"); err == nil {
		t.Fatal("unknown auth accepted")
	}
}

// With a real Xray-core (VEILON_TEST_XRAY=/path/to/xray): its keys pass
// through and the panel derives the same client keys from its seeds.
func TestGenerateVLESSEncKeysRealXray(t *testing.T) {
	bin := os.Getenv("VEILON_TEST_XRAY")
	if bin == "" {
		t.Skip("VEILON_TEST_XRAY not set")
	}
	for _, auth := range EncAuths {
		k, err := GenerateVLESSEncKeys(bin, auth)
		if err != nil || k.Source != "xray" {
			t.Fatalf("%s: %+v %v", auth, k, err)
		}
	}
	out, err := exec.Command(bin, "mlkem768").Output()
	if err != nil {
		t.Fatal(err)
	}
	var seed, client string
	for _, l := range strings.Split(string(out), "\n") {
		if s, ok := strings.CutPrefix(l, "Seed: "); ok {
			seed = s
		}
		if c, ok := strings.CutPrefix(l, "Client: "); ok {
			client = c
		}
	}
	if got, err := VLESSEncClientKey(EncAuthMLKEM, seed); err != nil || got != client {
		t.Fatalf("ML-KEM client key differs from xray: %v", err)
	}
}
