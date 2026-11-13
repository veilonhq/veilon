package inbounds

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func certp(i int64) *int64 { return &i }

// The panel's ML-DSA-65 and ECH derivations match the real xray binary
// (VEILON_TEST_XRAY), and the Go fallbacks produce what xray accepts.
func TestAdvancedKeysMatchXray(t *testing.T) {
	bin := os.Getenv("VEILON_TEST_XRAY")
	// Go fallbacks always work.
	m, err := GenerateMLDSA65("")
	if err != nil || m.Source != "go" || len(m.Server) != 43 || len(m.Client) < 2600 {
		t.Fatalf("go mldsa65 %+v %v", m, err)
	}
	e, err := GenerateECH("", "outer.example.com")
	if err != nil || e.Source != "go" {
		t.Fatalf("go ech %+v %v", e, err)
	}
	if l, _ := ECHConfigListFromKeys(e.Server); l != e.Client {
		t.Fatal("ech list mismatch")
	}
	if _, err := ECHConfigListFromKeys("bm90IGtleXM="); err == nil {
		t.Fatal("garbage ech keys accepted")
	}
	if _, err := MLDSA65Verify("short"); err == nil {
		t.Fatal("bad seed accepted")
	}
	if bin == "" {
		t.Skip("VEILON_TEST_XRAY not set")
	}
	x, err := GenerateMLDSA65(bin)
	if err != nil || x.Source != "xray" {
		t.Fatalf("xray mldsa65 %+v %v", x, err)
	}
	out, err := exec.Command(bin, "mldsa65", "-i", m.Server).Output()
	if err != nil || !strings.Contains(string(out), "Verify: "+m.Client) {
		t.Fatalf("xray disagrees with go mldsa65: %s", out)
	}
	xe, err := GenerateECH(bin, "outer.example.com")
	if err != nil || xe.Source != "xray" {
		t.Fatalf("xray ech %+v %v", xe, err)
	}
	// xray restores the config list from the Go-made keys identically.
	out, err = exec.Command(bin, "tls", "ech", "-i", e.Server).Output()
	if err != nil || !strings.Contains(string(out), e.Client) {
		t.Fatalf("xray disagrees with go ech: %s", out)
	}
}

func TestNormalizeAdvanced(t *testing.T) {
	cid := certp(1)
	base := func() View {
		return View{Remark: "a", Protocol: VLESS, Port: 443, Security: "tls", TLS: &TLS{CertID: cid}, Sniffing: true}
	}
	bad := []struct {
		name string
		v    func() View
	}{
		{"fallback on ws", func() View {
			v := base()
			v.Network, v.Advanced = "ws", &Advanced{Fallbacks: []Fallback{{Dest: "80"}}}
			return v
		}},
		{"fallback without tls", func() View {
			v := base()
			v.Security, v.TLS, v.Advanced = "none", nil, &Advanced{Fallbacks: []Fallback{{Dest: "80"}}}
			return v
		}},
		{"fallback with vless encryption", func() View {
			v := base()
			v.VLESSEnc = &VLESSEnc{Enabled: true}
			v.Advanced = &Advanced{Fallbacks: []Fallback{{Dest: "80"}}}
			return v
		}},
		{"fallback bad dest", func() View {
			v := base()
			v.Advanced = &Advanced{Fallbacks: []Fallback{{Dest: "nope"}}}
			return v
		}},
		{"fallback xver", func() View {
			v := base()
			v.Advanced = &Advanced{Fallbacks: []Fallback{{Dest: "80", Xver: 3}}}
			return v
		}},
		{"fallback path", func() View {
			v := base()
			v.Advanced = &Advanced{Fallbacks: []Fallback{{Dest: "80", Path: "x"}}}
			return v
		}},
		{"tls version", func() View {
			v := base()
			v.Advanced = &Advanced{TLS: &TLSExtra{MinVersion: "1.4"}}
			return v
		}},
		{"tls min>max", func() View {
			v := base()
			v.Advanced = &Advanced{TLS: &TLSExtra{MinVersion: "1.3", MaxVersion: "1.2"}}
			return v
		}},
		{"ciphers", func() View {
			v := base()
			v.Advanced = &Advanced{TLS: &TLSExtra{CipherSuites: "rm -rf"}}
			return v
		}},
		{"ech keys", func() View {
			v := base()
			v.Advanced = &Advanced{TLS: &TLSExtra{ECHServerKeys: "AAAA"}}
			return v
		}},
		{"http header with tls", func() View {
			v := base()
			v.Advanced = &Advanced{TCP: &TCPHeader{Type: "http"}}
			return v
		}},
		{"proxy protocol on kcp", func() View {
			v := base()
			v.Network, v.Security, v.TLS = "kcp", "none", nil
			v.Advanced = &Advanced{Sockopt: &Sockopt{AcceptProxyProtocol: true}}
			return v
		}},
		{"xmux both", func() View {
			v := base()
			v.Network = "xhttp"
			v.Advanced = &Advanced{XHTTP: &XHTTPExtra{Xmux: &Xmux{MaxConcurrency: "1", MaxConnections: "2"}}}
			return v
		}},
		{"xhttp range", func() View {
			v := base()
			v.Network = "xhttp"
			v.Advanced = &Advanced{XHTTP: &XHTTPExtra{XPaddingBytes: "9-1"}}
			return v
		}},
		{"download stream-one", func() View {
			v := base()
			v.Network, v.Transport.Mode = "xhttp", "stream-one"
			v.Advanced = &Advanced{XHTTP: &XHTTPExtra{Download: &XHTTPDownload{Address: "cdn.example.com"}}}
			return v
		}},
		{"kcp header", func() View {
			v := base()
			v.Network, v.Security, v.TLS = "kcp", "none", nil
			v.Advanced = &Advanced{KCP: &KCPExtra{Header: "bogus"}}
			return v
		}},
		{"sniff dest", func() View {
			v := base()
			v.Advanced = &Advanced{Sniffing: &SniffOpts{DestOverride: []string{"ftp"}}}
			return v
		}},
		{"metadata only with http", func() View {
			v := base()
			v.Advanced = &Advanced{Sniffing: &SniffOpts{DestOverride: []string{"http"}, MetadataOnly: true}}
			return v
		}},
		{"ws header name", func() View {
			v := base()
			v.Network = "ws"
			v.Advanced = &Advanced{WS: &WSExtra{Headers: map[string]string{"Bad Name": "x"}}}
			return v
		}},
		{"ws header value", func() View {
			v := base()
			v.Network = "ws"
			v.Advanced = &Advanced{WS: &WSExtra{Headers: map[string]string{"X": "a\r\nb"}}}
			return v
		}},
		{"reverse uuid", func() View {
			v := base()
			v.Advanced = &Advanced{Reverse: &Reverse{Enabled: true, UUID: "x"}}
			return v
		}},
		{"noise on tcp", func() View {
			v := base()
			v.Advanced = &Advanced{Masks: &Masks{Noise: &NoiseMask{Rand: "10"}}}
			return v
		}},
		{"fragment without length", func() View {
			v := base()
			v.Advanced = &Advanced{Masks: &Masks{Fragment: &FragmentMask{Packets: "tlshello"}}}
			return v
		}},
		{"reality client ver", func() View {
			v := base()
			v.Security, v.TLS, v.Reality = "reality", nil, &Reality{Dest: "www.example.com"}
			v.Advanced = &Advanced{Reality: &RealityExtra{MinClientVer: "300.1.1"}}
			return v
		}},
		{"reality seed", func() View {
			v := base()
			v.Security, v.TLS, v.Reality = "reality", nil, &Reality{Dest: "www.example.com"}
			v.Advanced = &Advanced{Reality: &RealityExtra{MLDSA65Seed: "abc"}}
			return v
		}},
		{"hy force-brutal", func() View {
			return View{Remark: "h", Protocol: Hysteria, Port: 443, Advanced: &Advanced{Hysteria: &HysteriaExtra{Congestion: "force-brutal"}}}
		}},
		{"hy masquerade url", func() View {
			return View{Remark: "h", Protocol: Hysteria, Port: 443, Advanced: &Advanced{Hysteria: &HysteriaExtra{Masquerade: &Masquerade{Type: "proxy", URL: "ftp://x"}}}}
		}},
		{"tuic relay", func() View {
			return View{Remark: "t", Protocol: TUIC, Port: 443, Advanced: &Advanced{TUIC: &TUICExtra{UDPRelayMode: "x"}}}
		}},
		{"ss reality", func() View {
			return View{Remark: "s", Protocol: Shadowsocks, Port: 443, Security: "reality", Reality: &Reality{Dest: "a.com"}}
		}},
	}
	for _, c := range bad {
		v := c.v()
		if err := Normalize(&v); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}

	// Settings that do not apply are dropped; all-default means nil.
	v := base()
	v.Advanced = &Advanced{Sniffing: &SniffOpts{DestOverride: []string{"quic", "http", "tls"}}, WS: &WSExtra{HeartbeatPeriod: 5},
		Reality: &RealityExtra{MaxTimeDiff: 5}, Hysteria: &HysteriaExtra{Congestion: "bbr"}, SS: &SSExtra{NoUDP: true}, TCP: &TCPHeader{Type: "none"}}
	if err := Normalize(&v); err != nil || v.Advanced != nil {
		t.Fatalf("not applicable kept: %v %+v", err, v.Advanced)
	}
	// Sniffing off drops sniffing details.
	v = base()
	v.Sniffing = false
	v.Advanced = &Advanced{Sniffing: &SniffOpts{RouteOnly: true}}
	if err := Normalize(&v); err != nil || v.Advanced != nil {
		t.Fatalf("sniffing details kept while off")
	}
	// Derived values: ECH list, ML-DSA verify, reverse UUID.
	e, _ := GenerateECH("", "outer.example.com")
	m, _ := GenerateMLDSA65("")
	v = base()
	v.Advanced = &Advanced{TLS: &TLSExtra{ECHServerKeys: e.Server, ECHConfigList: "stale"}, Reverse: &Reverse{Enabled: true, RouteInboundIDs: []int64{5, 5, 0, 3}}}
	if err := Normalize(&v); err != nil {
		t.Fatal(err)
	}
	if v.Advanced.TLS.ECHConfigList != e.Client || len(v.Advanced.Reverse.UUID) != 36 || len(v.Advanced.Reverse.RouteInboundIDs) != 2 || v.Advanced.Reverse.RouteInboundIDs[0] != 3 {
		t.Fatalf("derived %+v %+v", v.Advanced.TLS, v.Advanced.Reverse)
	}
	r := View{Remark: "r", Protocol: VLESS, Port: 443, Security: "reality", Reality: &Reality{Dest: "www.example.com"}, Advanced: &Advanced{Reality: &RealityExtra{MLDSA65Seed: m.Server}}}
	if err := Normalize(&r); err != nil || r.Advanced.Reality.MLDSA65Verify != m.Client {
		t.Fatalf("mldsa verify %v", err)
	}
	// Shadowsocks over ws with TLS is allowed now, and UDP follows the transport.
	ss := View{Remark: "s", Protocol: Shadowsocks, Port: 443, Network: "ws", Security: "tls", TLS: &TLS{CertID: cid}}
	if err := Normalize(&ss); err != nil {
		t.Fatal(err)
	}
	if tcp, udp := Transports(&ss); !tcp || udp {
		t.Fatalf("ss ws transports %v %v", tcp, udp)
	}
	k := View{Remark: "k", Protocol: VMess, Port: 443, Network: "kcp"}
	if err := Normalize(&k); err != nil {
		t.Fatal(err)
	}
	if tcp, udp := Transports(&k); tcp || !udp {
		t.Fatalf("kcp transports %v %v", tcp, udp)
	}

	// Stored and read back unchanged; old rows read as nil.
	v = base()
	v.Advanced = &Advanced{Fallbacks: []Fallback{{Dest: "80"}}, Sockopt: &Sockopt{AcceptProxyProtocol: true}}
	if err := Normalize(&v); err != nil {
		t.Fatal(err)
	}
	var row db.Inbound
	toModel(&v, &row)
	back := FromModel(&row)
	a, _ := json.Marshal(v.Advanced)
	b, _ := json.Marshal(back.Advanced)
	if string(a) != string(b) {
		t.Fatalf("round trip %s != %s", a, b)
	}
	old := View{Remark: "o", Protocol: VLESS, Port: 443, Security: "tls", TLS: &TLS{CertID: cid}}
	_ = Normalize(&old)
	toModel(&old, &row)
	if strings.Contains(row.Settings, "advanced") {
		t.Fatalf("empty advanced stored: %s", row.Settings)
	}
	if FromModel(&row).Advanced != nil {
		t.Fatal("old row has advanced")
	}
}
