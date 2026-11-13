package sub

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// ShadowTLS, AnyTLS, NaiveProxy and Hysteria 2 port hopping in every
// output that can express them.
func sbProtoProxies(t *testing.T, ex Extras) map[string]Proxy {
	t.Helper()
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Protocol, err)
		}
		if v.TLS != nil {
			v.TLS.CertID = certID(9)
			v.TLS.SNI = "srv.example.com"
			v.TLS.AllowInsecure = v.Protocol != inbounds.Naive // naive needs a CA certificate
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 31, Remark: "STLS", Protocol: inbounds.ShadowTLS, Port: 8443, Settings: inbounds.Settings{HandshakeServer: "www.microsoft.com"}}),
		mk(inbounds.View{ID: 32, Remark: "AnyTLS", Protocol: inbounds.AnyTLS, Port: 8444}),
		mk(inbounds.View{ID: 33, Remark: "Naive", Protocol: inbounds.Naive, Port: 8445}),
		mk(inbounds.View{ID: 34, Remark: "Hop", Protocol: inbounds.Hysteria, Port: 8446, Settings: inbounds.Settings{HopPorts: "20000-30000,40000", HopInterval: 15}}),
	}
	ps := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "srv.example.com"}, "h", ex)
	out := map[string]Proxy{}
	for _, p := range ps {
		out[p.Name] = p
	}
	return out
}

func TestSingboxProtocolOutputs(t *testing.T) {
	cp, _, _ := certs.GenerateSelfSigned("srv.example.com")
	pins, _ := certs.PinsOf(cp)
	ps := sbProtoProxies(t, Extras{Pins: map[int64]certs.Pins{9: pins}})
	u := user()
	st, an, nv, hop := ps["STLS"], ps["AnyTLS"], ps["Naive"], ps["Hop"]

	// Share links.
	if URI(&st) != "" {
		t.Fatal("ShadowTLS has no share-link standard")
	}
	if l := URI(&an); !strings.HasPrefix(l, "anytls://"+u.Password+"@srv.example.com:8444/?") || !strings.Contains(l, "sni=srv.example.com") {
		t.Fatalf("anytls link %s", l)
	}
	if l := URI(&nv); l != "naive+https://"+url.UserPassword(xrayEmail(u), u.Password).String()+"@srv.example.com:8445#Naive" {
		t.Fatalf("naive link %s", l)
	}
	hl, _ := url.Parse(URI(&hop))
	if hl.Port() != "8446" || hl.Query().Get("mport") != "20000-30000,40000" {
		t.Fatalf("hysteria hop link %s", URI(&hop))
	}

	all := []Proxy{st, an, nv, hop}
	// sing-box: ShadowTLS = shadowsocks with a detour to a shadowtls outbound.
	b, err := Singbox(all)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	_ = json.Unmarshal(b, &sb)
	by := map[string]map[string]any{}
	for _, o := range sb.Outbounds {
		by[o["tag"].(string)] = o
	}
	if o := by["STLS"]; o["type"] != "shadowsocks" || o["detour"] != "STLS · shadowtls" || !strings.Contains(o["password"].(string), ":") {
		t.Fatalf("sing-box ss %v", o)
	}
	if o := by["STLS · shadowtls"]; o["type"] != "shadowtls" || o["password"] != u.Password || o["version"].(float64) != 3 {
		t.Fatalf("sing-box shadowtls %v", o)
	}
	if strings.Contains(string(b), `"STLS · shadowtls",`) && strings.Contains(string(b), `"auto"`) {
		// the detour must not be offered in the selector / urltest
		for _, o := range sb.Outbounds {
			if o["type"] == "selector" || o["type"] == "urltest" {
				for _, x := range o["outbounds"].([]any) {
					if x == "STLS · shadowtls" {
						t.Fatal("detour listed in a group")
					}
				}
			}
		}
	}
	if o := by["AnyTLS"]; o["type"] != "anytls" || o["password"] != u.Password {
		t.Fatalf("sing-box anytls %v", o)
	}
	if o := by["Naive"]; o["type"] != "naive" || o["username"] != xrayEmail(u) {
		t.Fatalf("sing-box naive %v", o)
	}
	if o := by["Hop"]; o["server_port"] != nil || o["hop_interval"] != "15s" || len(o["server_ports"].([]any)) != 2 || o["server_ports"].([]any)[1] != "40000:40000" {
		t.Fatalf("sing-box hop %v", o)
	}

	// Clash (mihomo): no NaiveProxy.
	yb, err := Clash(all)
	if err != nil {
		t.Fatal(err)
	}
	var cy struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	_ = yaml.Unmarshal(yb, &cy)
	cb := map[string]map[string]any{}
	for _, p := range cy.Proxies {
		cb[p["name"].(string)] = p
	}
	if _, ok := cb["Naive"]; ok || len(cb) != 3 {
		t.Fatalf("clash proxies %v", cb)
	}
	if p := cb["STLS"]; p["type"] != "ss" || p["plugin"] != "shadow-tls" || p["plugin-opts"].(map[string]any)["host"] != "www.microsoft.com" {
		t.Fatalf("clash shadowtls %v", p)
	}
	if p := cb["AnyTLS"]; p["type"] != "anytls" || p["fingerprint"] == nil {
		t.Fatalf("clash anytls %v", p)
	}
	if p := cb["Hop"]; p["ports"] != "20000-30000,40000" || p["hop-interval"] != 15 {
		t.Fatalf("clash hop %v", p)
	}

	// Xray JSON: only the Hysteria 2 one, with the udphop mask.
	if XraySupported(&st) || XraySupported(&an) || XraySupported(&nv) {
		t.Fatal("Xray has no client for the sing-box protocols")
	}
	xo := xrayExtraOutbound(&hop)
	fm := xo["streamSettings"].(obj)["finalmask"].(obj)["udp"].([]any)
	if h := fm[0].(obj); h["type"] != "udphop" || h["settings"].(obj)["remotePorts"] != "20000-30000,40000" {
		t.Fatalf("xray udphop %v", fm)
	}

	// Real clients accept the configs when given.
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" {
		f := filepath.Join(t.TempDir(), "sb.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil && !strings.Contains(string(out), "cronet") {
			t.Fatalf("sing-box check: %v\n%s", err, out)
		}
	}
	if bin := os.Getenv("VEILON_TEST_MIHOMO"); bin != "" {
		d := t.TempDir()
		_ = os.WriteFile(filepath.Join(d, "config.yaml"), yb, 0o600)
		if out, err := exec.Command(bin, "-t", "-d", d).CombinedOutput(); err != nil {
			t.Fatalf("mihomo -t: %v\n%s", err, out)
		}
	}
}

// A node whose agent lacks port hopping gets links without hop ports.
func TestHopLinksPerNode(t *testing.T) {
	ex := Extras{Nodes: map[int64][]inbounds.NodeTarget{34: {{ID: 1, Name: "new", Address: "1.1.1.1"}, {ID: 2, Name: "old", Address: "2.2.2.2", NoHop: true}}},
		Placed: map[int64]bool{34: true}, NodeOnly: map[int64]bool{34: true}}
	ps := sbProtoProxies(t, ex)
	if ps["Hop · new"].HopPorts == "" || ps["Hop · old"].HopPorts != "" {
		t.Fatalf("new %q old %q", ps["Hop · new"].HopPorts, ps["Hop · old"].HopPorts)
	}
}
