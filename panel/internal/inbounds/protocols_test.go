package inbounds

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func norm(t *testing.T, v View) View {
	t.Helper()
	if err := Normalize(&v); err != nil {
		t.Fatalf("%s: %v", v.Protocol, err)
	}
	return v
}

func TestNormalizeDefaults(t *testing.T) {
	h := norm(t, View{Remark: "h", Protocol: Hysteria, Port: 443})
	if h.Security != "tls" || h.TLS == nil || h.TLS.ALPN[0] != "h3" || h.Network != "udp" {
		t.Fatalf("hysteria defaults %+v", h)
	}
	tu := norm(t, View{Remark: "t", Protocol: TUIC, Port: 443})
	if tu.Settings.Congestion != "bbr" {
		t.Fatalf("tuic congestion %q", tu.Settings.Congestion)
	}
	wg := norm(t, View{Remark: "w", Protocol: WireGuard, Port: 51820})
	if wg.Settings.PrivateKey == "" || wg.Settings.PublicKey == "" || wg.Settings.MTU != 1420 || len(wg.Settings.DNS) != 2 {
		t.Fatalf("wireguard defaults %+v", wg.Settings)
	}
	if pub, _ := WGPublicKey(wg.Settings.PrivateKey); pub != wg.Settings.PublicKey {
		t.Fatal("public key mismatch")
	}
	if p, err := netip.ParsePrefix(wg.Settings.Address); err != nil || p.Bits() != 24 {
		t.Fatalf("address %q", wg.Settings.Address)
	}
	if wg.Settings.AWG != nil {
		t.Fatal("plain wireguard must not carry awg params")
	}
	awg := norm(t, View{Remark: "a", Protocol: AmneziaWG, Port: 51821})
	a := awg.Settings.AWG
	if a == nil || a.Jc < 3 || a.Jmin > a.Jmax || a.S1+56 == a.S2 || a.H1 < 5 || a.H1 == a.H2 {
		t.Fatalf("awg defaults %+v", a)
	}
	mx := norm(t, View{Remark: "m", Protocol: Mixed, Port: 1080, Settings: Settings{UDP: true}})
	if !mx.Settings.UDP || mx.Security != "none" {
		t.Fatalf("mixed %+v", mx)
	}
	tun := norm(t, View{Remark: "tun", Protocol: TUN})
	if tun.Port != 0 || tun.Settings.TunName != "veilon-tun" || tun.Settings.Gateway[0] != "10.77.0.1/24" {
		t.Fatalf("tun %+v", tun.Settings)
	}
	mt := norm(t, View{Remark: "mt", Protocol: MTProto, Port: 443})
	if mt.Settings.FakeTLSDomain != "www.cloudflare.com" || mt.Network != "tcp" {
		t.Fatalf("mtproto %+v", mt.Settings)
	}
	mq := norm(t, View{Remark: "mq", Protocol: Masque, Port: 443})
	if len(mq.Settings.Pool) != 2 || mq.Settings.Path != MasqueDefaultPath || mq.Settings.MTU != 1280 || mq.TLS.ALPN[0] != "h3" {
		t.Fatalf("masque %+v", mq.Settings)
	}
	// Irrelevant fields are cleared.
	h2 := norm(t, View{Remark: "x", Protocol: Hysteria, Port: 1, Flow: "xtls-rprx-vision", SSMethod: "aes-128-gcm", Settings: Settings{TunName: "x"}})
	if h2.Flow != "" || h2.SSMethod != "" || h2.Settings.TunName != "" {
		t.Fatalf("not cleared %+v", h2)
	}
}

func TestNormalizeErrors(t *testing.T) {
	bad := []View{
		{Remark: "t", Protocol: Tunnel, Port: 1},                                               // no target
		{Remark: "t", Protocol: Tunnel, Port: 1, Settings: Settings{TargetAddress: "1.1.1.1"}}, // no port
		{Remark: "t", Protocol: Tunnel, Port: 1, Settings: Settings{TargetAddress: "bad host!", TargetPort: 1}},
		{Remark: "w", Protocol: WireGuard, Port: 1, Settings: Settings{Address: "10.0.0.0/24"}}, // network address
		{Remark: "w", Protocol: WireGuard, Port: 1, Settings: Settings{Address: "10.0.0.1/8"}},  // too big
		{Remark: "w", Protocol: WireGuard, Port: 1, Settings: Settings{PrivateKey: "nope"}},
		{Remark: "a", Protocol: AmneziaWG, Port: 1, Settings: Settings{AWG: &AWG{Jc: 3, Jmin: 50, Jmax: 10, H1: 5, H2: 6, H3: 7, H4: 8}}},
		{Remark: "a", Protocol: AmneziaWG, Port: 1, Settings: Settings{AWG: &AWG{Jc: 3, Jmin: 1, Jmax: 10, H1: 5, H2: 5, H3: 7, H4: 8}}},
		{Remark: "a", Protocol: AmneziaWG, Port: 1, Settings: Settings{AWG: &AWG{S1: 10, S2: 66, H1: 5, H2: 6, H3: 7, H4: 8}}},
		{Remark: "tun", Protocol: TUN, Settings: Settings{Routes: []string{"0.0.0.0/0"}}},
		{Remark: "tun", Protocol: TUN, Settings: Settings{TunName: "this-name-is-way-too-long"}},
		{Remark: "m", Protocol: MTProto, Port: 1, Settings: Settings{FakeTLSDomain: "not a domain"}},
		{Remark: "q", Protocol: Masque, Port: 1, Settings: Settings{Pool: []string{"10.0.0.1/24", "10.1.0.1/24"}}},
		{Remark: "q", Protocol: TUIC, Port: 1, Settings: Settings{Congestion: "reno"}},
		{Remark: "h", Protocol: HTTP, Port: 1, Security: "tls"}, // HTTPS needs a cert
		{Remark: "m", Protocol: Mixed, Port: 1, Security: "tls"},
		{Remark: "h", Protocol: Hysteria, Port: 0},
		{Remark: "r", Protocol: Hysteria, Port: 1, Security: "reality"},
	}
	for i, v := range bad {
		if err := Normalize(&v); err == nil {
			t.Errorf("case %d (%s) should fail", i, v.Protocol)
		}
	}
}

func TestTransports(t *testing.T) {
	cases := []struct {
		v        View
		tcp, udp bool
	}{
		{View{Protocol: VLESS, Network: "tcp"}, true, false},
		{View{Protocol: Shadowsocks}, true, true},
		{View{Protocol: Hysteria}, false, true},
		{View{Protocol: TUIC}, false, true},
		{View{Protocol: WireGuard}, false, true},
		{View{Protocol: AmneziaWG}, false, true},
		{View{Protocol: HTTP}, true, false},
		{View{Protocol: Mixed, Settings: Settings{UDP: true}}, true, true},
		{View{Protocol: Mixed}, true, false},
		{View{Protocol: Tunnel, Settings: Settings{TunnelNetwork: "udp"}}, false, true},
		{View{Protocol: Tunnel, Settings: Settings{TunnelNetwork: "tcp,udp"}}, true, true},
		{View{Protocol: MTProto}, true, false},
		{View{Protocol: Masque, TLS: &TLS{ALPN: []string{"h3", "h2"}}}, true, true},
		{View{Protocol: Masque}, false, true},
		{View{Protocol: TUN}, false, false},
	}
	for _, c := range cases {
		tcp, udp := Transports(&c.v)
		if tcp != c.tcp || udp != c.udp {
			t.Errorf("%s: got %v/%v want %v/%v", c.v.Protocol, tcp, udp, c.tcp, c.udp)
		}
	}
}

func TestPortConflictsAndSelfSigned(t *testing.T) {
	gdb := testutil.DB(t)
	certs := 0
	svc := &Service{DB: gdb, ReservedPorts: func() []int { return []int{2053} }, SelfSigned: func(d string) (int64, error) {
		certs++
		c := db.Cert{Domain: d, Source: "self", CertPEM: "x", KeyPEM: "y"}
		gdb.Create(&c)
		return c.ID, nil
	}}
	mk := func(v View) (*View, error) { v.Enabled = true; return svc.Create(&v) }
	if _, err := mk(View{Remark: "vless", Protocol: VLESS, Port: 443}); err != nil {
		t.Fatal(err)
	}
	// Ports are unique across all inbounds, even a UDP protocol on a TCP port.
	if _, err := mk(View{Remark: "hy0", Protocol: Hysteria, Port: 443}); err == nil {
		t.Fatal("shared port accepted")
	}
	h, err := mk(View{Remark: "hy", Protocol: Hysteria, Port: 4443})
	if err != nil {
		t.Fatal(err)
	}
	if h.TLS == nil || h.TLS.CertID == nil || !h.TLS.AllowInsecure || h.TLS.SNI != "www.bing.com" {
		t.Fatalf("self-signed tls %+v", h.TLS)
	}
	// Second QUIC inbound reuses the self-signed certificate.
	if _, err := mk(View{Remark: "mq", Protocol: Masque, Port: 8443}); err != nil {
		t.Fatal(err)
	}
	if certs != 1 {
		t.Fatalf("self-signed certificates created: %d", certs)
	}
	if _, err := mk(View{Remark: "tuic", Protocol: TUIC, Port: 4443}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "port 4443") {
		t.Fatalf("udp conflict not detected: %v", err)
	}
	if _, err := mk(View{Remark: "http", Protocol: HTTP, Port: 443}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "port 443") {
		t.Fatalf("tcp conflict not detected: %v", err)
	}
	if _, err := mk(View{Remark: "panel", Protocol: Mixed, Port: 2053}); err == nil {
		t.Fatal("reserved port accepted")
	}
	// A port stays unique even on different listen addresses.
	if _, err := mk(View{Remark: "l1", Protocol: HTTP, Port: 9000, Listen: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mk(View{Remark: "l2", Protocol: HTTP, Port: 9000, Listen: "127.0.0.2"}); err == nil {
		t.Fatal("shared port on another listen address accepted")
	}
	// Overlapping WireGuard subnets are rejected.
	if _, err := mk(View{Remark: "wg1", Protocol: WireGuard, Port: 51820, Settings: Settings{Address: "10.9.0.1/24"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mk(View{Remark: "wg2", Protocol: AmneziaWG, Port: 51821, Settings: Settings{Address: "10.9.0.5/16"}}); err == nil {
		t.Fatal("overlapping subnet accepted")
	}
	// Two TUN inbounds cannot share an interface name.
	if _, err := mk(View{Remark: "tun1", Protocol: TUN}); err != nil {
		t.Fatal(err)
	}
	if _, err := mk(View{Remark: "tun2", Protocol: TUN}); err == nil {
		t.Fatal("duplicate tun name accepted")
	}
}

func TestEnsurePeers(t *testing.T) {
	gdb := testutil.DB(t)
	ib := norm(t, View{ID: 5, Remark: "wg", Protocol: WireGuard, Port: 1, Settings: Settings{Address: "10.5.0.1/29"}})
	ps, err := EnsurePeers(gdb, &ib, []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for uid, p := range ps {
		if p.PrivateKey == "" || p.PresharedKey == "" || p.Address == "10.5.0.1/32" || seen[p.Address] {
			t.Fatalf("bad peer %d %+v", uid, p)
		}
		seen[p.Address] = true
		if pub, _ := WGPublicKey(p.PrivateKey); pub != p.PublicKey {
			t.Fatal("peer key mismatch")
		}
	}
	// Stable on the second call.
	ps2, _ := EnsurePeers(gdb, &ib, []int64{1, 2, 3})
	if ps2[1].PrivateKey != ps[1].PrivateKey || ps2[1].Address != ps[1].Address {
		t.Fatal("peers not stable")
	}
	// /29 has 6 hosts; server takes one, broadcast excluded -> 5 peers max.
	if _, err := EnsurePeers(gdb, &ib, []int64{1, 2, 3, 4, 5, 6}); err == nil {
		t.Fatal("full subnet not detected")
	}
	// Subnet change keeps keys, moves addresses.
	ib.Settings.Address = "10.6.0.1/24"
	ps3, err := EnsurePeers(gdb, &ib, []int64{1})
	if err != nil || ps3[1].PrivateKey != ps[1].PrivateKey || !strings.HasPrefix(ps3[1].Address, "10.6.0.") {
		t.Fatalf("subnet move %+v %v", ps3[1], err)
	}
}

func TestMTProtoSecret(t *testing.T) {
	s := MTProtoSecret("www.cloudflare.com", "pw")
	if !strings.HasPrefix(s, "ee") || len(s) != 2+32+2*len("www.cloudflare.com") || !strings.HasSuffix(s, "7777772e636c6f7564666c6172652e636f6d") {
		t.Fatal(s)
	}
	if MTProtoSecret("www.cloudflare.com", "other") == s {
		t.Fatal("secrets must differ per user")
	}
}

// The web UI shows this mapping on the wizard tiles (web/src/lib/protocols.ts
// engineOf); keep both in step.
func TestEngineMapping(t *testing.T) {
	want := map[string]string{
		VLESS: EngineXray, VMess: EngineXray, Trojan: EngineXray, Shadowsocks: EngineXray,
		Hysteria: EngineXray, Masque: EngineXray, WireGuard: EngineXray, TUN: EngineXray,
		HTTP: EngineXray, Mixed: EngineXray, Socks: EngineXray, Tunnel: EngineXray,
		TUIC: EngineSingbox, AmneziaWG: EngineAWG, MTProto: EngineMTG,
	}
	for p, e := range want {
		if got := Engine(p); got != e {
			t.Errorf("Engine(%s) = %s, want %s", p, got, e)
		}
	}
}
