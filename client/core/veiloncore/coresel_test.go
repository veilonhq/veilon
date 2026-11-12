package veiloncore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestResolveCoreOverride(t *testing.T) {
	const u = "b831381d-6324-4d53-ad4f-8cda48b30811"
	cases := []struct {
		name, link, global, pref string
		want                     Core
		err                      error
	}{
		{"auto-ws", vlessTLSWS, "auto", "", CoreSingbox, nil},
		{"global-xray-ws", vlessTLSWS, "xray", "", CoreXray, nil},
		{"pref-xray-wins", vlessTLSWS, "sing-box", "xray", CoreXray, nil},
		{"pref-singbox-wins", vlessTLSWS, "xray", "sing-box", CoreSingbox, nil},
		{"pref-auto-uses-global", vlessTLSWS, "xray", "auto", CoreXray, nil},
		{"auto-xhttp", "vless://" + u + "@198.51.100.2:443?security=tls&sni=a.example.com&type=xhttp&path=%2Fx#x", "auto", "", CoreXray, nil},
		{"singbox-xhttp-refused", "vless://" + u + "@198.51.100.2:443?security=tls&sni=a.example.com&type=xhttp&path=%2Fx#x", "sing-box", "", CoreSingbox, ErrSingboxNeedsXray},
		{"singbox-kcp-refused", "vless://" + u + "@10.0.0.1:443?type=kcp&seed=s#k", "auto", "sing-box", CoreSingbox, ErrSingboxNeedsXray},
		{"xray-cleartext-public", "vless://" + u + "@198.51.100.1:443?type=ws&path=%2F#c", "xray", "", CoreXray, ErrXrayCleartextPublicIP},
		{"auto-cleartext-public", "vless://" + u + "@198.51.100.1:443?type=ws&path=%2F#c", "auto", "", CoreSingbox, nil},
		{"trojan-xray", "trojan://pw@198.51.100.4:443?security=tls&sni=t.example.com&type=grpc&serviceName=g#t", "xray", "", CoreXray, nil},
		{"ss-xray", "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpmYWtlcGFzcw==@198.51.100.5:8388#ss", "auto", "xray", CoreXray, nil},
		{"vmess-singbox", "vmess://eyJ2IjogIjIiLCAicHMiOiAidm1lc3Mtd3MiLCAiYWRkIjogIjE5OC41MS4xMDAuMyIsICJwb3J0IjogIjQ0MyIsICJpZCI6ICJiODMxMzgxZC02MzI0LTRkNTMtYWQ0Zi04Y2RhNDhiMzA4MTEiLCAiYWlkIjogIjAiLCAic2N5IjogImF1dG8iLCAibmV0IjogIndzIiwgInR5cGUiOiAibm9uZSIsICJob3N0IjogImNkbi5leGFtcGxlLmNvbSIsICJwYXRoIjogIi93cyIsICJ0bHMiOiAidGxzIiwgInNuaSI6ICJjZG4uZXhhbXBsZS5jb20ifQ==", "xray", "sing-box", CoreSingbox, nil},
		// Non-V2Ray types ignore the choice.
		{"hy2-ignores", "hysteria2://pw@198.51.100.6:443?sni=h.example.com#h", "xray", "xray", CoreSingbox, nil},
		{"wg-ignores", "wireguard://0mske3O80qVSmGm2nHiR2us1ZgH8GQwWFCbDgZJmNt4%3D@198.51.100.8:51820?publickey=uoiOdrOiF%2FSKQmzpqsctzB7H4J6tS%2BDT5XXeSeSWVEY%3D&address=10.7.0.2%2F32#wg", "xray", "", CoreSingbox, nil},
	}
	for _, c := range cases {
		p, err := ParseLink(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		p.CorePref = c.pref
		p.normalize()
		st, _ := parseSettings(`{"core":"` + c.global + `"}`)
		got, err := ResolveCore(p, st)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("%s: got %s/%v want %s/%v", c.name, got, err, c.want, c.err)
		}
		// prepare (connect and URLTest path) applies the same choice.
		plan, _, perr := prepare(context.Background(), p, st, true, nil)
		if !errors.Is(perr, c.err) {
			t.Errorf("%s: prepare err %v want %v", c.name, perr, c.err)
		}
		if perr == nil && c.want == CoreXray {
			b, _ := json.Marshal(plan.outbounds)
			if !strings.Contains(string(b), `"socks"`) {
				t.Errorf("%s: xray plan should use the local socks outbound: %s", c.name, b)
			}
		}
		// JSON API.
		pj, _ := json.Marshal(p)
		var r map[string]string
		_ = json.Unmarshal([]byte(ResolveCoreFor(string(pj), `{"core":"`+c.global+`"}`)), &r)
		if r["core"] != string(c.want) || (c.err != nil) != (r["error"] != "") {
			t.Errorf("%s: ResolveCoreFor %v", c.name, r)
		}
	}
	// The choice does not change the profile id.
	p1, _ := ParseLink(vlessTLSWS)
	p2, _ := ParseLink(vlessTLSWS)
	p2.CorePref = "xray"
	if computeID(p1) != computeID(p2) {
		t.Error("core choice changed the profile id")
	}
	// Round-trip through the profile JSON.
	var back Profile
	b, _ := json.Marshal(p2)
	_ = json.Unmarshal(b, &back)
	if back.CorePref != "xray" || !strings.Contains(string(b), `"core":"xray"`) {
		t.Errorf("json: %s", b)
	}
}
