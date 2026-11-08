package xray

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func TestStatsCodec(t *testing.T) {
	req := EncodeQueryStatsRequest("user>>>", true)
	if len(req) == 0 {
		t.Fatal("empty request")
	}
	stat := func(name string, v int64) []byte {
		var m []byte
		m = protowire.AppendTag(m, 1, protowire.BytesType)
		m = protowire.AppendString(m, name)
		m = protowire.AppendTag(m, 2, protowire.VarintType)
		m = protowire.AppendVarint(m, uint64(v))
		var out []byte
		out = protowire.AppendTag(out, 1, protowire.BytesType)
		return protowire.AppendBytes(out, m)
	}
	var resp []byte
	resp = append(resp, stat("user>>>3.alice>>>traffic>>>uplink", 100)...)
	resp = append(resp, stat("user>>>3.alice>>>traffic>>>downlink", 2000)...)
	resp = append(resp, stat("inbound>>>inbound-1>>>traffic>>>downlink", 5000)...)
	resp = append(resp, stat("inbound>>>api>>>traffic>>>downlink", 9)...)
	resp = append(resp, stat("outbound>>>direct>>>traffic>>>downlink", 7)...)
	stats, err := DecodeQueryStatsResponse(resp)
	if err != nil || len(stats) != 5 {
		t.Fatalf("%v %v", err, stats)
	}
	tr := ParseStats(stats)
	if tr.Users[3] != [2]int64{100, 2000} || tr.SysDown != 5000 || tr.SysUp != 0 {
		t.Fatalf("parsed %+v", tr)
	}
}

func TestEmailRoundTrip(t *testing.T) {
	id, ok := ParseEmail(Email(&db.User{ID: 42, Username: "a.b"}))
	if !ok || id != 42 {
		t.Fatal("email roundtrip failed")
	}
}

func TestBuildConfig(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "certs", "1", "fullchain.pem")
	_ = os.MkdirAll(filepath.Dir(certFile), 0o700)
	_ = os.WriteFile(certFile, []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "certs", "1", "privkey.pem"), []byte("x"), 0o600)
	one := int64(1)
	two := int64(2)
	ibs := []inbounds.View{
		{ID: 1, Remark: "a", Protocol: "vless", Port: 443, Network: "tcp", Security: "reality", Flow: "xtls-rprx-vision",
			Reality: &inbounds.Reality{Dest: "x.com:443", ServerNames: []string{"x.com"}, PrivateKey: "k", ShortIDs: []string{"ab"}}},
		{ID: 2, Remark: "b", Protocol: "vmess", Port: 80, Network: "ws", Security: "tls", Transport: inbounds.Transport{Path: "/p"},
			TLS: &inbounds.TLS{CertID: &one, ALPN: []string{"h2"}}},
		{ID: 3, Remark: "c", Protocol: "trojan", Port: 81, Network: "grpc", Security: "tls", Transport: inbounds.Transport{ServiceName: "g"},
			TLS: &inbounds.TLS{CertID: &two}}, // missing cert files -> skipped
		{ID: 4, Remark: "d", Protocol: "shadowsocks", Port: 82, Network: "tcp", Security: "none", SSMethod: "aes-128-gcm"}, // no users -> skipped
	}
	users := map[int64][]db.User{
		1: {{ID: 1, Username: "u", UUID: "uuid-1"}},
		2: {{ID: 1, Username: "u", UUID: "uuid-1"}},
	}
	b, err := Build(&Input{Inbounds: ibs, Users: users, APIPort: 10085, DataDir: dir, AssetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []map[string]any `json:"inbounds"`
		Stats    map[string]any   `json:"stats"`
		API      map[string]any   `json:"api"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 3 { // api + 2
		t.Fatalf("want 3 inbounds, got %d", len(cfg.Inbounds))
	}
	vl := cfg.Inbounds[1]
	clients := vl["settings"].(map[string]any)["clients"].([]any)
	c0 := clients[0].(map[string]any)
	if c0["email"] != "1.u" || c0["flow"] != "xtls-rprx-vision" || c0["id"] != "uuid-1" {
		t.Fatalf("bad client %v", c0)
	}
	rs := vl["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
	if rs["privateKey"] != "k" || rs["dest"] != "x.com:443" {
		t.Fatalf("bad reality %v", rs)
	}
	ws := cfg.Inbounds[2]["streamSettings"].(map[string]any)
	if ws["security"] != "tls" || ws["wsSettings"].(map[string]any)["path"] != "/p" {
		t.Fatalf("bad ws/tls %v", ws)
	}
}

func TestAssetName(t *testing.T) {
	if n, _ := AssetName("linux", "arm64"); n != "Xray-linux-arm64-v8a.zip" {
		t.Fatal(n)
	}
	if n, _ := AssetName("linux", "amd64"); n != "Xray-linux-64.zip" {
		t.Fatal(n)
	}
	if _, err := AssetName("windows", "amd64"); err == nil {
		t.Fatal("windows should be unsupported")
	}
}
