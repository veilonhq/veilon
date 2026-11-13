package inbounds

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHopPorts(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "",
		"20000-30000":           "20000-30000",
		" 40000-40100 , 20000 ": "20000,40000-40100",
		"۲۰۰۰۰-۲۰۰۱۰":           "20000-20010",
		"20000:20010":           "20000-20010",
		"20000-20010،30000":     "20000-20010,30000",
	} {
		rs, err := ParseHopPorts(in)
		if err != nil || FormatHopPorts(rs) != want {
			t.Fatalf("%q: got %q %v want %q", in, FormatHopPorts(rs), err, want)
		}
	}
	for _, bad := range []string{"80-90", "30000-20000", "20000-30000,25000-26000", "a-b", "70000", "1-2,3-4,5-6,7-8,9-10"} {
		if _, err := ParseHopPorts(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestNormalizeSingboxProtocols(t *testing.T) {
	v := View{Remark: "st", Protocol: ShadowTLS, Port: 443}
	if err := Normalize(&v); err != nil {
		t.Fatal(err)
	}
	if v.Settings.HandshakeServer != "www.microsoft.com" || v.Settings.HandshakePort != 443 || v.SSMethod != ShadowTLSMethods[0] || v.SSServerKey == "" || v.Security != "none" || v.TLS != nil {
		t.Fatalf("shadowtls defaults %+v", v)
	}
	key := v.SSServerKey
	if err := Normalize(&v); err != nil || v.SSServerKey != key {
		t.Fatal("server key must survive a second normalize (edit)")
	}
	v.SSMethod = "aes-128-gcm"
	if Normalize(&v) == nil {
		t.Fatal("non-2022 inner method accepted")
	}
	a := View{Remark: "a", Protocol: AnyTLS, Port: 443, Settings: Settings{PaddingScheme: []string{"stop=8", "0=30-30", "", "1=100-400,c,500-1000"}}}
	if err := Normalize(&a); err != nil || len(a.Settings.PaddingScheme) != 3 || a.Security != "tls" {
		t.Fatalf("anytls %v %+v", err, a.Settings.PaddingScheme)
	}
	a.Settings.PaddingScheme = []string{"0=30-30"}
	if Normalize(&a) == nil {
		t.Fatal("padding scheme without stop= accepted")
	}
	n := View{Remark: "n", Protocol: Naive, Port: 443}
	if err := Normalize(&n); err != nil || n.Security != "tls" || strings.Join(n.TLS.ALPN, ",") != "h2,http/1.1" {
		t.Fatalf("naive %v %+v", err, n.TLS)
	}
	for _, p := range []string{ShadowTLS, AnyTLS, Naive} {
		if Engine(p) != EngineSingbox || NodeCapable(p) {
			t.Fatalf("%s must be a main-only sing-box protocol", p)
		}
	}
	h := View{Remark: "h", Protocol: Hysteria, Port: 443, Settings: Settings{HopPorts: "20000-30000", HopInterval: 3}}
	if Normalize(&h) == nil {
		t.Fatal("hop interval below 5 s accepted")
	}
	h.Settings.HopInterval = 10
	if err := Normalize(&h); err != nil || h.Settings.HopPorts != "20000-30000" {
		t.Fatal(err)
	}
}

func TestHopConflicts(t *testing.T) {
	s, n1, _ := portSvc(t)
	s.SelfSigned = func(string) (int64, error) { return 0, errors.New("no certs in this test") }
	hy := func(remark string, port int, hop string, main bool, nodes ...int64) *View {
		v := onNodes(remark, Hysteria, port, main, nodes...)
		v.Settings.HopPorts = hop
		return v
	}
	wantHop := func(err error, part string) {
		t.Helper()
		var ce *ConflictError
		if !errors.As(err, &ce) || ce.Field != "hop_ports" || !strings.Contains(ce.Msg, part) {
			t.Fatalf("want hop conflict %q, got %v", part, err)
		}
	}
	// A WireGuard (UDP) inbound on 25000 on the main server.
	wg := mk("wg", WireGuard, 25000)
	if _, err := s.Create(wg); err != nil {
		t.Fatal(err)
	}
	wantHop(s.checkHop(hy("h1", 443, "20000-30000", true)), "covers UDP port 25000")
	// The same range on a node only does not touch the main server.
	if err := s.checkHop(hy("h2", 443, "20000-30000", false, n1)); err != nil {
		t.Fatal(err)
	}
	// A TCP inbound inside the range is fine (only UDP is redirected).
	tcp := mk("vl", VLESS, 21000)
	if _, err := s.Create(tcp); err != nil {
		t.Fatal(err)
	}
	if err := s.checkHop(hy("h3", 443, "21000-21010", true)); err != nil {
		t.Fatal(err)
	}
}
