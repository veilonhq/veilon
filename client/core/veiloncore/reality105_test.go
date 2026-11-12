package veiloncore

import (
	"encoding/json"
	"strings"
	"testing"
)

// Run 105: VLESS REALITY link params → profile → Xray realitySettings /
// sing-box tls.reality, one assertion per parameter, and the auto core.
func TestRealityLinkParams(t *testing.T) {
	const pbk = "nOgeQ91pGXuAOrULdAGVkC0sBrgLuKWXGq7tj0gqFEA" // base64url, 43 chars, no padding
	base := "vless://" + ntUUID + "@198.51.100.1:443?encryption=none&type=tcp&headerType=none&security=reality" +
		"&pbk=" + pbk + "&sid=6ba85179e30d4fc2&sni=www.example.com&spx=%2F"
	cases := []struct {
		name, extra string
		check       func(t *testing.T, p *Profile, x, s obj)
	}{
		{"pbk", "&fp=chrome", func(t *testing.T, p *Profile, x, s obj) {
			if p.PublicKey != pbk || x["publicKey"] != pbk || s["public_key"] != pbk {
				t.Errorf("pbk: profile %q xray %v sing-box %v", p.PublicKey, x["publicKey"], s["public_key"])
			}
		}},
		{"sid", "&fp=chrome", func(t *testing.T, p *Profile, x, s obj) {
			if p.ShortID != "6ba85179e30d4fc2" || x["shortId"] != p.ShortID || s["short_id"] != p.ShortID {
				t.Errorf("sid: %q %v %v", p.ShortID, x["shortId"], s["short_id"])
			}
		}},
		{"sni", "&fp=chrome", func(t *testing.T, p *Profile, x, s obj) {
			if p.SNI != "www.example.com" || x["serverName"] != p.SNI {
				t.Errorf("sni: %q %v", p.SNI, x["serverName"])
			}
		}},
		{"spx", "&fp=chrome", func(t *testing.T, p *Profile, x, s obj) {
			if p.SpiderX != "/" || x["spiderX"] != "/" {
				t.Errorf("spx: %q %v", p.SpiderX, x["spiderX"])
			}
		}},
		{"fp-given", "&fp=firefox", func(t *testing.T, p *Profile, x, s obj) {
			if x["fingerprint"] != "firefox" || s["utls_fp"] != "firefox" {
				t.Errorf("fp: xray %v sing-box %v", x["fingerprint"], s["utls_fp"])
			}
		}},
		{"fp-default", "", func(t *testing.T, p *Profile, x, s obj) {
			if x["fingerprint"] != "chrome" || s["utls_fp"] != "chrome" {
				t.Errorf("missing fp must default to chrome: xray %v sing-box %v", x["fingerprint"], s["utls_fp"])
			}
		}},
		{"pqv", "&fp=chrome&pqv=QUJD", func(t *testing.T, p *Profile, x, s obj) {
			if p.PQV != "QUJD" || x["mldsa65Verify"] != "QUJD" {
				t.Errorf("pqv: %q %v", p.PQV, x["mldsa65Verify"])
			}
		}},
		{"flow", "&fp=chrome&flow=xtls-rprx-vision", func(t *testing.T, p *Profile, x, s obj) {
			if p.Flow != "xtls-rprx-vision" || !strings.Contains(s["xray_json"].(string), `"flow":"xtls-rprx-vision"`) {
				t.Errorf("flow: %q not in the Xray outbound", p.Flow)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseLink(base + c.extra + "#r")
			if err != nil {
				t.Fatal(err)
			}
			// Auto core is Xray for REALITY (sing-box 1.14 fails against
			// current Xray servers, see needsXray).
			if core, err := ResolveCore(p, DefaultSettings()); err != nil || core != CoreXray {
				t.Errorf("auto core %s (%v), want xray", core, err)
			}
			xo, err := xrayOutbound(p)
			if err != nil {
				t.Fatal(err)
			}
			xj, _ := json.Marshal(xo)
			var xm map[string]any
			_ = json.Unmarshal(xj, &xm)
			x := obj(xm["streamSettings"].(map[string]any)["realitySettings"].(map[string]any))
			so, _, err := singboxOutbound(p, tagProxy)
			if err != nil {
				t.Fatal(err)
			}
			sj, _ := json.Marshal(so)
			var sm map[string]any
			_ = json.Unmarshal(sj, &sm)
			tl := sm["tls"].(map[string]any)
			r := tl["reality"].(map[string]any)
			s := obj{"public_key": r["public_key"], "short_id": r["short_id"], "server_name": tl["server_name"], "xray_json": string(xj)}
			if u, ok := tl["utls"].(map[string]any); ok {
				s["utls_fp"] = u["fingerprint"]
			}
			if s["server_name"] != p.SNI {
				t.Errorf("sing-box server_name %v, want the sni", s["server_name"])
			}
			c.check(t, p, x, s)
		})
	}
}
