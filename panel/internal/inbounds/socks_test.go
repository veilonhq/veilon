package inbounds

import "testing"

func TestSocksNormalize(t *testing.T) {
	s := norm(t, View{Remark: "s", Protocol: Socks, Port: 1080, Sniffing: true,
		Settings: Settings{UDP: true, NoAuth: true, ObfsPassword: "x", TargetPort: 9}})
	if s.Security != "none" || s.Network != "tcp" || !s.Settings.UDP || !s.Settings.NoAuth || s.TLS != nil {
		t.Fatalf("socks %+v", s)
	}
	if s.Settings.ObfsPassword != "" || s.Settings.TargetPort != 0 {
		t.Fatal("foreign settings kept")
	}
	if err := Normalize(&View{Remark: "s", Protocol: Socks, Port: 1080, Security: "tls"}); err == nil {
		t.Fatal("socks with tls accepted")
	}
	tcp, udp := Transports(&s)
	if !tcp || !udp {
		t.Fatal("socks transports with udp")
	}
	s2 := norm(t, View{Remark: "s2", Protocol: Socks, Port: 1081})
	if tcp, udp := Transports(&s2); !tcp || udp || s2.Settings.NoAuth {
		t.Fatal("socks defaults: tcp only, password auth")
	}
	if !in(Socks, Protocols) || Engine(Socks) != EngineXray || !NodeCapable(Socks) || NodeCap(Socks) != "" || !HasUsers(Socks) {
		t.Fatal("socks must be an Xray, node-capable protocol with users")
	}
	for _, p := range []string{HTTP, Mixed, Socks} {
		if !IsProxyLogin(p) {
			t.Fatalf("%s uses the proxy login", p)
		}
	}
	if IsProxyLogin(Masque) || IsProxyLogin(VLESS) {
		t.Fatal("only http, mixed and socks use the proxy login")
	}
}
