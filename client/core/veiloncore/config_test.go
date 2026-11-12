package veiloncore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Representative links (fake servers/keys) for config generation checks.
var configCases = []struct{ name, link, core string }{
	{"vless-reality", "vless://b831381d-6324-4d53-ad4f-8cda48b30811@198.51.100.1:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=nOgeQ91pGXuAOrULdAGVkC0sBrgLuKWXGq7tj0gqFEA&sid=6ba85179&type=tcp&flow=xtls-rprx-vision#vless-reality", "xray"},
	{"vless-xhttp", "vless://b831381d-6324-4d53-ad4f-8cda48b30811@198.51.100.2:443?security=tls&sni=cdn.example.com&fp=chrome&type=xhttp&path=%2Fxh&mode=auto&host=cdn.example.com#vless-xhttp", "xray"},
	{"vless-ws-tls", "vless://b831381d-6324-4d53-ad4f-8cda48b30811@198.51.100.2:443?security=tls&sni=cdn.example.com&type=ws&path=%2Fws&host=cdn.example.com#vless-ws", "sing-box"},
	{"vmess-ws", "vmess://eyJ2IjogIjIiLCAicHMiOiAidm1lc3Mtd3MiLCAiYWRkIjogIjE5OC41MS4xMDAuMyIsICJwb3J0IjogIjQ0MyIsICJpZCI6ICJiODMxMzgxZC02MzI0LTRkNTMtYWQ0Zi04Y2RhNDhiMzA4MTEiLCAiYWlkIjogIjAiLCAic2N5IjogImF1dG8iLCAibmV0IjogIndzIiwgInR5cGUiOiAibm9uZSIsICJob3N0IjogImNkbi5leGFtcGxlLmNvbSIsICJwYXRoIjogIi93cyIsICJ0bHMiOiAidGxzIiwgInNuaSI6ICJjZG4uZXhhbXBsZS5jb20ifQ==", "sing-box"},
	{"trojan-grpc", "trojan://fakepass@198.51.100.4:443?security=tls&sni=t.example.com&type=grpc&serviceName=gsvc#trojan", "sing-box"},
	{"ss", "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpmYWtlcGFzcw==@198.51.100.5:8388#ss", "sing-box"},
	{"ss2022", "ss://2022-blake3-aes-128-gcm:AAECAwQFBgcICQoLDA0ODw%3D%3D@198.51.100.5:8388#ss2022", "sing-box"},
	{"hy2", "hysteria2://fakepass@198.51.100.6:443?sni=h.example.com&obfs=salamander&obfs-password=fakeobfs#hy2", "sing-box"},
	{"tuic", "tuic://b831381d-6324-4d53-ad4f-8cda48b30811:fakepass@198.51.100.7:443?congestion_control=bbr&sni=tu.example.com&alpn=h3#tuic", "sing-box"},
	{"wg", "wireguard://0mske3O80qVSmGm2nHiR2us1ZgH8GQwWFCbDgZJmNt4%3D@198.51.100.8:51820?publickey=uoiOdrOiF%2FSKQmzpqsctzB7H4J6tS%2BDT5XXeSeSWVEY%3D&address=10.7.0.2%2F32&mtu=1280#wg", "sing-box"},
	{"http", "http://testuser:fakepass@198.51.100.11:8080#http", "sing-box"},
}

const awgConf = `[Interface]
PrivateKey = 0mske3O80qVSmGm2nHiR2us1ZgH8GQwWFCbDgZJmNt4=
Address = 10.8.0.2/32
DNS = 1.1.1.1
Jc = 4
Jmin = 40
Jmax = 70
S1 = 52
S2 = 49
H1 = 1213945451
H2 = 1513475432
H3 = 1784535361
H4 = 1904584353

[Peer]
PublicKey = uoiOdrOiF/SKQmzpqsctzB7H4J6tS+DT5XXeSeSWVEY=
AllowedIPs = 0.0.0.0/0
Endpoint = 198.51.100.12:51820
PersistentKeepalive = 25
`

// dumpConfig writes generated configs to $VEILON_DUMP_DIR (for running
// the sing-box / xray CLIs against them).
func dumpConfig(t *testing.T, name string, cfg any) {
	dir := os.Getenv("VEILON_DUMP_DIR")
	if dir == "" || cfg == nil {
		return
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
		t.Error(err)
	}
}

func checkGenerated(t *testing.T, name string, p *Profile, wantCore string) {
	t.Helper()
	if c := CoreOf(p); string(c) != wantCore {
		t.Errorf("core = %s, want %s", c, wantCore)
	}
	st := DefaultSettings()
	st.MixedPort = 10808
	for _, tun := range []bool{true, false} {
		cfg, err := checkProfile(p, st, tun)
		if err != nil {
			t.Fatalf("checkProfile(tun=%v): %v", tun, err)
		}
		if tun {
			dumpConfig(t, "singbox-"+name, cfg)
		}
	}
	if CoreOf(p) == CoreXray {
		x, err := xrayConfig(p, 10809, "u", "p", st)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkXrayConfig(x); err != nil {
			t.Fatalf("xray: %v", err)
		}
		dumpConfig(t, "xray-"+name, x)
	}
	// Public API path.
	if msg := CheckProfile(profileJSON(p), ""); msg != "" {
		t.Errorf("CheckProfile: %s", msg)
	}
	if out := BuildConfig(profileJSON(p), ""); strings.Contains(out, `"error"`) && !strings.Contains(out, `"outbounds"`) {
		t.Errorf("BuildConfig: %s", out)
	}
}

func TestGeneratedConfigs(t *testing.T) {
	for _, c := range configCases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseLink(c.link)
			if err != nil {
				t.Fatal(err)
			}
			checkGenerated(t, c.name, p, c.core)
		})
	}
	t.Run("awg", func(t *testing.T) {
		p, err := ParseWGConf(awgConf, "awg")
		if err != nil {
			t.Fatal(err)
		}
		if p.Type != TypeAmneziaWG || p.WG.AWG == nil || p.WG.AWG.Jc != 4 {
			t.Fatalf("awg parse: %+v", p)
		}
		checkGenerated(t, "awg", p, string(CoreAWG))
	})
}

// profileJSON is a test helper.
func profileJSON(p *Profile) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// TestRemovedTypesRejected: SlipNet-family (SSH, StunTLS, DNS tunnels)
// and Naive links are rejected with an app string key, and stored
// profiles of those types fail cleanly instead of starting.
func TestRemovedTypesRejected(t *testing.T) {
	for _, l := range []string{
		"slipnet://MjJ8c3NofHN0dW50bHN8MTkyLjAuMi4xMA==",
		"slipgate://abc",
		"ssh://u:p@198.51.100.9:22#ssh",
		"ssh://u:p@198.51.100.9:8443?ws=1&wstls=1&path=%2F#stuntls",
	} {
		if _, err := ParseLink(l); !errors.Is(err, ErrSlipnetRemoved) {
			t.Errorf("%s: err = %v", l, err)
		}
	}
	for _, l := range []string{"naive+https://u:p@n.example.com:443#n", "naive+quic://u:p@n.example.com:443#n"} {
		if _, err := ParseLink(l); !errors.Is(err, ErrTypeRemoved) {
			t.Errorf("%s: err = %v", l, err)
		}
	}
	r := ParseContent("slipnet://MjJ8c3No\nvless://b831381d-6324-4d53-ad4f-8cda48b30811@198.51.100.1:443?security=none#ok")
	if len(r.Profiles) != 1 || len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "err.slipnetRemoved") {
		t.Errorf("mixed: %d profiles, errors %v", len(r.Profiles), r.Errors)
	}
	for _, typ := range []string{"ssh", "dnstt", "noizdns", "vaydns", "slipstream", "soar", "naive"} {
		js := `{"type":"` + typ + `","server":"198.51.100.9","port":22}`
		if msg := CheckProfile(js, ""); msg != "err.typeRemoved" {
			t.Errorf("%s: CheckProfile = %q", typ, msg)
		}
		if c := CoreFor(js); c != "" {
			t.Errorf("%s: CoreFor = %q", typ, c)
		}
	}
	// SOCKS5 is a standard protocol and stays: Clash entries are kept.
	c := ParseContent("proxies:\n  - {name: s, type: socks5, server: 198.51.100.1, port: 1080}\n  - {name: h, type: http, server: 198.51.100.2, port: 8080}\n")
	if len(c.Profiles) != 2 || c.Profiles[0].Type != TypeSOCKS || c.Profiles[1].Type != TypeHTTP {
		t.Errorf("clash: %+v", c.Profiles)
	}
}

// TestStackSettingIgnored: stored stack / experimentalStack are ignored.
func TestStackSettingIgnored(t *testing.T) {
	for _, js := range []string{`{"stack":"system","experimentalStack":true}`, `{"stack":"mixed"}`, ``} {
		st, err := parseSettings(js)
		if err != nil {
			t.Fatal(err)
		}
		if st.Stack != "gvisor" || st.ExperimentalStack || tunStack(st) != "gvisor" {
			t.Errorf("%s: %+v", js, st.Stack)
		}
	}
	if tunStack(Settings{Stack: "system", ExperimentalStack: true}) != "gvisor" {
		t.Error("tunStack must force gvisor")
	}
}
