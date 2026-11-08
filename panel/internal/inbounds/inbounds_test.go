package inbounds

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestX25519KnownVector(t *testing.T) {
	// RFC 7748 section 6.1 test vector.
	priv, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	want, _ := hex.DecodeString("8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
	pub, err := PublicKeyFromPrivate(base64.RawURLEncoding.EncodeToString(priv))
	if err != nil {
		t.Fatal(err)
	}
	if pub != base64.RawURLEncoding.EncodeToString(want) {
		t.Fatalf("got %s", pub)
	}
}

func TestGenerateRealityKeys(t *testing.T) {
	k, err := GenerateRealityKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(k.PrivateKey) != 43 || len(k.PublicKey) != 43 || len(k.ShortID) != 16 {
		t.Fatalf("unexpected lengths %+v", k)
	}
	pub, _ := PublicKeyFromPrivate(k.PrivateKey)
	if pub != k.PublicKey {
		t.Fatal("public key mismatch")
	}
}

func TestNormalize(t *testing.T) {
	v := View{Remark: "r", Protocol: "vless", Port: 443, Security: "reality", Flow: "xtls-rprx-vision",
		Reality: &Reality{Dest: "www.example.com"}}
	if err := Normalize(&v); err != nil {
		t.Fatal(err)
	}
	if v.Reality.Dest != "www.example.com:443" || v.Reality.ServerNames[0] != "www.example.com" || v.Reality.PublicKey == "" || len(v.Reality.ShortIDs) != 1 {
		t.Fatalf("defaults not applied %+v", v.Reality)
	}
	bad := []View{
		{Remark: "", Protocol: "vless", Port: 1},
		{Remark: "r", Protocol: "foo", Port: 1},
		{Remark: "r", Protocol: "vless", Port: 70000},
		{Remark: "r", Protocol: "vmess", Port: 1, Security: "reality", Reality: &Reality{Dest: "a.com"}},
		{Remark: "r", Protocol: "vless", Port: 1, Network: "ws", Security: "reality", Reality: &Reality{Dest: "a.com"}},
		{Remark: "r", Protocol: "vless", Port: 1, Network: "ws", Flow: "xtls-rprx-vision", Security: "none"},
		{Remark: "r", Protocol: "shadowsocks", Port: 1, Security: "tls"},
		{Remark: "r", Protocol: "trojan", Port: 1, Security: "tls"},
		{Remark: "r", Protocol: "vless", Port: 1, Security: "reality", Reality: &Reality{Dest: "a.com", ShortIDs: []string{"xyz"}}},
	}
	for i, b := range bad {
		if err := Normalize(&b); err == nil {
			t.Errorf("case %d accepted: %+v", i, b)
		}
	}
	ss := View{Remark: "s", Protocol: "shadowsocks", Port: 1, SSMethod: "2022-blake3-aes-256-gcm"}
	if err := Normalize(&ss); err != nil {
		t.Fatal(err)
	}
	if k, _ := base64.StdEncoding.DecodeString(ss.SSServerKey); len(k) != 32 {
		t.Fatal("ss2022 server key not generated")
	}
	ws := View{Remark: "w", Protocol: "vmess", Port: 1, Network: "ws", Transport: Transport{Path: "ws"}}
	_ = Normalize(&ws)
	if ws.Transport.Path != "/ws" {
		t.Fatal("path not normalised")
	}
}
