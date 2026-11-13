package inbounds

import (
	"encoding/json"
	"os"
	"testing"
)

// TestWizardRulesGolden keeps testdata/wizard_rules.json (read by the web
// test tests/wizardRules.test.ts) equal to the Go table. Regenerate with
// VEILON_UPDATE_GOLDEN=1 go test ./panel/internal/inbounds -run WizardRules.
func TestWizardRulesGolden(t *testing.T) {
	b, err := json.MarshalIndent(Rules(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	const f = "testdata/wizard_rules.json"
	if os.Getenv("VEILON_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(f, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(b) {
		t.Fatalf("%s is out of date (VEILON_UPDATE_GOLDEN=1 to rewrite it, then make the web side agree):\n%s", f, b)
	}
}

// Normalize accepts every combination the table offers and refuses the
// ones it leaves out.
func TestWizardRulesMatchNormalize(t *testing.T) {
	cert := int64(1)
	for _, p := range []string{VLESS, VMess, Trojan, Shadowsocks} {
		for _, n := range Networks {
			for _, s := range Securities {
				for _, flow := range Flows {
					v := View{Remark: "x", Protocol: p, Port: 1000, Network: n, Security: s, Flow: flow}
					if s == "tls" {
						v.TLS = &TLS{CertID: &cert}
					}
					if s == "reality" {
						v.Reality = &Reality{Dest: "www.example.com:443"}
					}
					want := in(s, SecuritiesFor(p, n)) && (flow == "" || FlowAllowed(p, n, s, false))
					err := Normalize(&v)
					if (err == nil) != want {
						t.Errorf("%s/%s/%s flow %q: err %v, want ok=%v", p, n, s, flow, err, want)
					}
				}
			}
		}
	}
	for _, p := range []string{HTTP, Mixed, Socks} {
		for _, s := range Securities {
			v := View{Remark: "x", Protocol: p, Port: 1000, Security: s}
			if s == "tls" {
				v.TLS = &TLS{CertID: &cert}
			}
			if s == "reality" {
				v.Reality = &Reality{Dest: "www.example.com:443"}
			}
			err := Normalize(&v)
			if (err == nil) != in(s, SecuritiesFor(p, "")) {
				t.Errorf("%s/%s: err %v", p, s, err)
			}
		}
	}
}
