package inbounds

import (
	"strings"
	"testing"
)

func TestXrayNeeds(t *testing.T) {
	base := func() View {
		v := Defaults()
		v.Protocol, v.Network, v.Security = "vless", "tcp", "none"
		return v
	}
	if n := XrayNeeds(&View{Protocol: "tuic"}); n != nil {
		t.Fatalf("tuic (sing-box) %v", n)
	}
	v := base()
	if n := XrayNeeds(&v); len(n) != 0 {
		t.Fatalf("plain %v", n)
	}
	cases := []struct {
		name string
		set  func(v *View)
		want string
	}{
		{"mldsa", func(v *View) {
			v.Security = "reality"
			v.Advanced = &Advanced{Reality: &RealityExtra{MLDSA65Seed: "x"}}
		}, "Reality ML-DSA-65:25.7.26"},
		{"limit", func(v *View) {
			v.Security = "reality"
			v.Advanced = &Advanced{Reality: &RealityExtra{LimitDownload: &LimitFallback{BytesPerSec: 1}}}
		}, "Reality fallback rate limit:25.6.8"},
		{"ech", func(v *View) {
			v.Security = "tls"
			v.Advanced = &Advanced{TLS: &TLSExtra{ECHServerKeys: "k"}}
		}, "TLS ECH:25.7.26"},
		{"ech ignored without tls", func(v *View) { v.Advanced = &Advanced{TLS: &TLSExtra{ECHServerKeys: "k"}} }, ""},
		{"masks", func(v *View) {
			v.Advanced = &Advanced{Masks: &Masks{Fragment: &FragmentMask{Packets: "tlshello", Length: "100-200"}}}
		}, "fragment / noise masks:26.3.27"},
		{"reverse", func(v *View) { v.Advanced = &Advanced{Reverse: &Reverse{Enabled: true}} }, "VLESS reverse proxy:25.9.11"},
		{"kcp", func(v *View) { v.Network = "kcp" }, "mKCP:26.6.1"},
		{"stream-up", func(v *View) {
			v.Network = "xhttp"
			v.Advanced = &Advanced{XHTTP: &XHTTPExtra{ScStreamUpServerSecs: "20-80"}}
		}, "XHTTP stream-up seconds:25.1.30"},
		{"heartbeat", func(v *View) {
			v.Network = "ws"
			v.Advanced = &Advanced{WS: &WSExtra{HeartbeatPeriod: 10}}
		}, "WebSocket heartbeat:24.11.30"},
		{"hysteria", func(v *View) { v.Protocol, v.Network = "hysteria", "udp" }, "Hysteria 2:26.3.27"},
		{"vless enc", func(v *View) { v.VLESSEnc = &VLESSEnc{Enabled: true} }, "VLESS Encryption:25.9.5"},
		{"sorted, highest first", func(v *View) {
			v.Network = "kcp"
			v.Advanced = &Advanced{Reverse: &Reverse{Enabled: true}}
		}, "mKCP:26.6.1,VLESS reverse proxy:25.9.11"},
	}
	for _, c := range cases {
		v := base()
		c.set(&v)
		var got []string
		for _, n := range XrayNeeds(&v) {
			got = append(got, n.Feature+":"+n.Min)
		}
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: got %q want %q", c.name, strings.Join(got, ","), c.want)
		}
	}
	v = base()
	v.Network = "kcp"
	v.Advanced = &Advanced{Reverse: &Reverse{Enabled: true}}
	if m := XrayMissing(&v, "25.10.15"); len(m) != 1 || m[0].Feature != "mKCP" {
		t.Fatalf("missing %v", m)
	}
	if m := XrayMissing(&v, ""); m != nil {
		t.Fatalf("unknown version %v", m)
	}
	if m := XrayMissing(&v, "v26.10.31"); m != nil {
		t.Fatalf("new version %v", m)
	}
	if s := XrayMissingText(XrayMissing(&v, "25.8.3")); s != "mKCP (26.6.1+), VLESS reverse proxy (25.9.11+)" {
		t.Fatal(s)
	}
}
