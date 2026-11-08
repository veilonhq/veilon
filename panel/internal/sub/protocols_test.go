package sub

import (
	"encoding/base64"
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

func extraFixtures(t *testing.T) ([]inbounds.View, Extras) {
	t.Helper()
	mk := func(v inbounds.View) inbounds.View {
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Protocol, err)
		}
		if v.TLS != nil {
			v.TLS.CertID = certID(9)
			v.TLS.AllowInsecure = true
			if v.TLS.SNI == "" {
				v.TLS.SNI = "www.bing.com"
			}
		}
		return v
	}
	ibs := []inbounds.View{
		mk(inbounds.View{ID: 11, Remark: "Hy2", Protocol: inbounds.Hysteria, Port: 443, Settings: inbounds.Settings{ObfsPassword: "ob", UpMbps: 50, DownMbps: 200}}),
		mk(inbounds.View{ID: 12, Remark: "TUIC", Protocol: inbounds.TUIC, Port: 8443}),
		mk(inbounds.View{ID: 13, Remark: "WG", Protocol: inbounds.WireGuard, Port: 51820, Settings: inbounds.Settings{Address: "10.8.0.1/24"}}),
		mk(inbounds.View{ID: 14, Remark: "AWG", Protocol: inbounds.AmneziaWG, Port: 51821, Settings: inbounds.Settings{Address: "10.9.0.1/24"}}),
		mk(inbounds.View{ID: 15, Remark: "HTTP", Protocol: inbounds.HTTP, Port: 3128}),
		mk(inbounds.View{ID: 16, Remark: "Mixed", Protocol: inbounds.Mixed, Port: 1080, Settings: inbounds.Settings{UDP: true}}),
		mk(inbounds.View{ID: 17, Remark: "Fwd", Protocol: inbounds.Tunnel, Port: 53, Settings: inbounds.Settings{TargetAddress: "1.1.1.1", TargetPort: 53}}),
		mk(inbounds.View{ID: 18, Remark: "TUN", Protocol: inbounds.TUN}),
		mk(inbounds.View{ID: 19, Remark: "TG", Protocol: inbounds.MTProto, Port: 993}),
		mk(inbounds.View{ID: 20, Remark: "MASQUE", Protocol: inbounds.Masque, Port: 9443}),
	}
	priv, pub, _ := inbounds.GenerateWGKeys()
	_ = pub
	cp, _, _ := certs.GenerateSelfSigned("www.bing.com")
	pins, _ := certs.PinsOf(cp)
	ex := Extras{
		Pins: map[int64]certs.Pins{9: pins},
		Peers: map[int64]db.WGPeer{
			13: {PrivateKey: priv, PresharedKey: inbounds.GeneratePSK(), Address: "10.8.0.2/32"},
			14: {PrivateKey: priv, PresharedKey: inbounds.GeneratePSK(), Address: "10.9.0.2/32"},
		},
	}
	return ibs, ex
}

func extraProxies(t *testing.T) map[string]Proxy {
	ibs, ex := extraFixtures(t)
	ps := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "srv.example.com"}, "h", ex)
	out := map[string]Proxy{}
	for _, p := range ps {
		out[p.Protocol] = p
	}
	if _, ok := out[inbounds.Tunnel]; ok {
		t.Fatal("tunnel must not produce a client proxy")
	}
	if _, ok := out[inbounds.TUN]; ok {
		t.Fatal("tun must not produce a client proxy")
	}
	if len(out) != 8 {
		t.Fatalf("want 8 proxies, got %d", len(out))
	}
	return out
}

func TestExtraURIs(t *testing.T) {
	ps := extraProxies(t)
	hy := ps[inbounds.Hysteria]
	u, err := url.Parse(URI(&hy))
	if err != nil || u.Scheme != "hysteria2" || u.User.Username() != "trojanpass" || u.Host != "srv.example.com:443" {
		t.Fatalf("hy2 %v %v", u, err)
	}
	q := u.Query()
	if q.Get("obfs") != "salamander" || q.Get("obfs-password") != "ob" || q.Get("sni") != "www.bing.com" || q.Get("insecure") != "1" || len(q.Get("pinSHA256")) != 64 {
		t.Fatalf("hy2 query %v", q)
	}
	tu := ps[inbounds.TUIC]
	u, _ = url.Parse(URI(&tu))
	if u.Scheme != "tuic" || u.User.Username() != user().UUID {
		t.Fatalf("tuic %v", u)
	}
	if pw, _ := u.User.Password(); pw != "trojanpass" || u.Query().Get("congestion_control") != "bbr" || u.Query().Get("alpn") != "h3" {
		t.Fatalf("tuic %v", u)
	}
	wg := ps[inbounds.WireGuard]
	u, _ = url.Parse(URI(&wg))
	if u.Scheme != "wireguard" || u.Query().Get("address") != "10.8.0.2/32" || u.Query().Get("publickey") == "" {
		t.Fatalf("wg %v", u)
	}
	if pk, _ := url.PathUnescape(u.User.Username()); pk != wg.WG.PrivateKey {
		t.Fatal("wg private key")
	}
	h := ps[inbounds.HTTP]
	if got := URI(&h); got != "http://7.alice:trojanpass@srv.example.com:3128#HTTP" {
		t.Fatalf("http %s", got)
	}
	mx := ps[inbounds.Mixed]
	u, _ = url.Parse(URI(&mx))
	ui, _ := base64.RawURLEncoding.DecodeString(u.User.Username())
	if u.Scheme != "socks" || string(ui) != "7.alice:trojanpass" {
		t.Fatalf("socks %v %s", u, ui)
	}
	tg := ps[inbounds.MTProto]
	u, _ = url.Parse(URI(&tg))
	if u.Scheme != "tg" || u.Host != "proxy" || u.Query().Get("server") != "srv.example.com" || u.Query().Get("port") != "993" ||
		u.Query().Get("secret") != inbounds.MTProtoSecret("www.cloudflare.com", "trojanpass") {
		t.Fatalf("tg %v", u)
	}
	for _, p := range []string{inbounds.AmneziaWG, inbounds.Masque} {
		x := ps[p]
		if URI(&x) != "" {
			t.Fatalf("%s should have no share link", p)
		}
	}
}

func TestConfFiles(t *testing.T) {
	ps := extraProxies(t)
	wg := ps[inbounds.WireGuard]
	name, body, ok := ConfFile(&wg)
	if !ok || name != "WG.conf" || !strings.Contains(body, "Endpoint = srv.example.com:51820") || strings.Contains(body, "Jc =") ||
		!strings.Contains(body, "Address = 10.8.0.2/32") || !strings.Contains(body, "PresharedKey = ") {
		t.Fatalf("wg conf %s\n%s", name, body)
	}
	awg := ps[inbounds.AmneziaWG]
	_, body, ok = ConfFile(&awg)
	for _, k := range []string{"Jc = ", "Jmin = ", "Jmax = ", "S1 = ", "S2 = ", "H1 = ", "H2 = ", "H3 = ", "H4 = "} {
		if !ok || !strings.Contains(body, k) {
			t.Fatalf("awg conf missing %q:\n%s", k, body)
		}
	}
	h := ps[inbounds.HTTP]
	if _, _, ok := ConfFile(&h); ok {
		t.Fatal("http has no conf file")
	}
	// Non-ASCII remarks get a safe tunnel name.
	wg.Name = "وایرگارد"
	if n, _, _ := ConfFile(&wg); n != "veilon-13.conf" {
		t.Fatal(n)
	}
}

func TestExtraFormats(t *testing.T) {
	ibs, ex := extraFixtures(t)
	ps := BuildProxiesX(user(), ibs, db.Settings{ServerAddress: "srv.example.com"}, "h", ex)
	// sing-box: hy2, tuic, http, socks + wireguard endpoint.
	b, err := Singbox(ps)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
		Endpoints []map[string]any `json:"endpoints"`
	}
	_ = json.Unmarshal(b, &sb)
	types := map[string]map[string]any{}
	for _, o := range sb.Outbounds {
		types[o["type"].(string)] = o
	}
	for _, want := range []string{"hysteria2", "tuic", "http", "socks"} {
		if types[want] == nil {
			t.Fatalf("sing-box missing %s: %s", want, b)
		}
	}
	if tls := types["hysteria2"]["tls"].(map[string]any); tls["certificate_public_key_sha256"] == nil {
		t.Fatalf("sing-box hy2 pin missing %v", tls)
	}
	if len(sb.Endpoints) != 1 || sb.Endpoints[0]["type"] != "wireguard" {
		t.Fatalf("sing-box endpoints %v", sb.Endpoints)
	}
	if bin := os.Getenv("VEILON_TEST_SINGBOX"); bin != "" {
		f := filepath.Join(t.TempDir(), "c.json")
		_ = os.WriteFile(f, b, 0o600)
		if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("sing-box check: %v\n%s", err, out)
		}
	}
	// Clash Meta: hy2, tuic, http, socks5, wireguard x2 (one with amnezia options).
	y, err := Clash(ps)
	if err != nil {
		t.Fatal(err)
	}
	var cc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	_ = yaml.Unmarshal(y, &cc)
	if len(cc.Proxies) != 6 {
		t.Fatalf("clash proxies %d:\n%s", len(cc.Proxies), y)
	}
	amnezia := 0
	for _, p := range cc.Proxies {
		if p["amnezia-wg-option"] != nil {
			amnezia++
		}
	}
	if amnezia != 1 {
		t.Fatal("amnezia-wg-option")
	}
	if bin := os.Getenv("VEILON_TEST_MIHOMO"); bin != "" {
		d := t.TempDir()
		_ = os.WriteFile(filepath.Join(d, "config.yaml"), y, 0o600)
		if out, err := exec.Command(bin, "-t", "-d", d).CombinedOutput(); err != nil {
			t.Fatalf("mihomo -t: %v\n%s", err, out)
		}
	}
	// Xray JSON: hy2, wireguard, http, socks, masque (no tuic/awg/mtproto).
	xb, err := XrayJSON(ps)
	if err != nil {
		t.Fatal(err)
	}
	var xs []map[string]any
	_ = json.Unmarshal(xb, &xs)
	got := []string{}
	for _, x := range xs {
		got = append(got, x["outbounds"].([]any)[0].(map[string]any)["protocol"].(string))
	}
	if strings.Join(got, ",") != "hysteria,wireguard,http,socks,masque" {
		t.Fatalf("xray outbounds %v", got)
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		for i, x := range xs {
			f := filepath.Join(t.TempDir(), "x.json")
			b, _ := json.Marshal(x)
			_ = os.WriteFile(f, b, 0o600)
			if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
				t.Fatalf("xray -test %s: %v\n%s", got[i], err, out)
			}
		}
	}
	// v2ray base64: only protocols with share links.
	raw, _ := base64.StdEncoding.DecodeString(V2Ray(ps))
	if n := len(strings.Split(string(raw), "\n")); n != 6 {
		t.Fatalf("v2ray links %d:\n%s", n, raw)
	}
}
