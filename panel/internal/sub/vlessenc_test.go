package sub

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// VLESS Encryption reaches clients as the "encryption" string: share link
// parameter, Clash (mihomo) "encryption", Xray JSON user "encryption";
// sing-box (no support) leaves the inbound out.
func TestVLESSEncryptionOutputs(t *testing.T) {
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Remark, err)
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 41, Remark: "ENC", Protocol: inbounds.VLESS, Port: 8443, Flow: "xtls-rprx-vision",
			VLESSEnc: &inbounds.VLESSEnc{Enabled: true, Auth: inbounds.EncAuthMLKEM, Mode: "xorpub"}}),
		mk(inbounds.View{ID: 42, Remark: "PLAIN", Protocol: inbounds.VLESS, Port: 8444}),
	}
	enc := ibs[0].VLESSEnc.ClientEncryption()
	if !strings.HasPrefix(enc, "mlkem768x25519plus.xorpub.0rtt.") || strings.Contains(enc, ibs[0].VLESSEnc.PrivateKey) {
		t.Fatalf("client string %q", enc)
	}
	ps := byName(BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "srv.example.com"}, "h", Extras{}))
	if ps["ENC"].Encryption != enc || ps["PLAIN"].Encryption != "" {
		t.Fatalf("proxies %+v", ps)
	}
	p := ps["ENC"]
	u, err := url.Parse(URI(&p))
	if err != nil || u.Query().Get("encryption") != enc || u.Query().Get("flow") != "xtls-rprx-vision" || u.Query().Get("security") != "none" {
		t.Fatalf("vless link %s", URI(&p))
	}
	plain := ps["PLAIN"]
	if u, _ := url.Parse(URI(&plain)); u.Query().Get("encryption") != "none" {
		t.Fatalf("plain link %s", URI(&plain))
	}
	all := []Proxy{ps["ENC"], ps["PLAIN"]}

	y, err := Clash(all)
	if err != nil {
		t.Fatal(err)
	}
	var cc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	_ = yaml.Unmarshal(y, &cc)
	if len(cc.Proxies) != 2 || cc.Proxies[0]["encryption"] != enc || cc.Proxies[1]["encryption"] != nil {
		t.Fatalf("clash %v", cc.Proxies)
	}

	sb, err := Singbox(all)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sb), `"ENC"`) || !strings.Contains(string(sb), `"PLAIN"`) {
		t.Fatalf("sing-box must leave the encrypted inbound out:\n%s", sb)
	}

	xj, err := XrayJSON(all)
	if err != nil {
		t.Fatal(err)
	}
	var xs []map[string]any
	_ = json.Unmarshal(xj, &xs)
	user0 := xs[0]["outbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
	if user0["encryption"] != enc || user0["flow"] != "xtls-rprx-vision" {
		t.Fatalf("xray json user %v", user0)
	}
	user1 := xs[1]["outbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
	if user1["encryption"] != "none" {
		t.Fatalf("plain xray json user %v", user1)
	}
}
