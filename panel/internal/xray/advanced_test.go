package xray

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// advancedInbounds covers every advanced setting at once.
func advancedInbounds(t *testing.T, certID int64) []inbounds.View {
	t.Helper()
	mk := func(v inbounds.View) inbounds.View {
		v.Enabled = true
		if err := inbounds.Normalize(&v); err != nil {
			t.Fatalf("%s: %v", v.Remark, err)
		}
		return v
	}
	ml, err := inbounds.GenerateMLDSA65("")
	if err != nil {
		t.Fatal(err)
	}
	ech, err := inbounds.GenerateECH("", "outer.example.com")
	if err != nil {
		t.Fatal(err)
	}
	tlsOf := func() *inbounds.TLS {
		return &inbounds.TLS{CertID: &certID, SNI: "adv.example.com", ALPN: []string{"h2", "http/1.1"}}
	}
	return []inbounds.View{
		mk(inbounds.View{ID: 1, Remark: "fb-tls", Protocol: "vless", Port: 42001, Security: "tls", TLS: tlsOf(), Flow: "xtls-rprx-vision", Sniffing: true,
			Advanced: &inbounds.Advanced{
				Fallbacks: []inbounds.Fallback{{Dest: "8080"}, {Path: "/ws", Dest: "127.0.0.1:8081", Xver: 1}, {Name: "a.example.com", ALPN: "h2", Dest: "/run/x.sock"}},
				TLS:       &inbounds.TLSExtra{MinVersion: "1.2", MaxVersion: "1.3", CipherSuites: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", RejectUnknownSNI: true, ECHServerKeys: ech.Server, PinCert: true},
				Sockopt:   &inbounds.Sockopt{AcceptProxyProtocol: true, TCPFastOpen: true, KeepAliveInterval: 30, KeepAliveIdle: 300, Mark: 255, Congestion: "bbr"},
				Sniffing:  &inbounds.SniffOpts{DestOverride: []string{"tls", "http"}, RouteOnly: true, DomainsExcluded: []string{"courier.push.apple.com"}},
				Masks:     &inbounds.Masks{Fragment: &inbounds.FragmentMask{Packets: "tlshello", Length: "100-200", Delay: "10-20"}},
			}}),
		mk(inbounds.View{ID: 2, Remark: "reality-pq", Protocol: "vless", Port: 42002, Security: "reality", Flow: "xtls-rprx-vision",
			Reality: &inbounds.Reality{Dest: "www.example.com"},
			Advanced: &inbounds.Advanced{
				Fallbacks: []inbounds.Fallback{{Dest: "8080"}},
				Reality: &inbounds.RealityExtra{MLDSA65Seed: ml.Server, MinClientVer: "25.9.5", MaxClientVer: "99.0.0", MaxTimeDiff: 60000,
					LimitUpload: &inbounds.LimitFallback{AfterBytes: 1 << 20, BytesPerSec: 100000, BurstBytesPerSec: 200000}, LimitDownload: &inbounds.LimitFallback{BytesPerSec: 500000}},
				Reverse: &inbounds.Reverse{Enabled: true, RouteSelf: true, RouteInboundIDs: []int64{3}},
			}}),
		mk(inbounds.View{ID: 3, Remark: "http-obfs", Protocol: "vmess", Port: 42003, Sniffing: true,
			Advanced: &inbounds.Advanced{TCP: &inbounds.TCPHeader{Type: "http", Host: []string{"a.com", "b.com"}, Path: []string{"/x"}},
				Sniffing: &inbounds.SniffOpts{DestOverride: []string{"fakedns"}, MetadataOnly: true}}}),
		mk(inbounds.View{ID: 4, Remark: "trojan-fb", Protocol: "trojan", Port: 42004, Security: "tls", TLS: tlsOf(),
			Advanced: &inbounds.Advanced{Fallbacks: []inbounds.Fallback{{Dest: "80"}}}}),
		mk(inbounds.View{ID: 5, Remark: "ss-noudp", Protocol: "shadowsocks", Port: 42005, Advanced: &inbounds.Advanced{SS: &inbounds.SSExtra{NoUDP: true}}}),
		mk(inbounds.View{ID: 6, Remark: "ss-ws-tls", Protocol: "shadowsocks", Port: 42006, Network: "ws", Security: "tls", TLS: tlsOf(), SSMethod: "2022-blake3-aes-256-gcm",
			Transport: inbounds.Transport{Path: "/ss"}}),
		mk(inbounds.View{ID: 7, Remark: "xhttp", Protocol: "vless", Port: 42007, Network: "xhttp", Security: "tls", TLS: tlsOf(), Transport: inbounds.Transport{Path: "/xh", Mode: "packet-up"},
			Advanced: &inbounds.Advanced{XHTTP: &inbounds.XHTTPExtra{XPaddingBytes: "100-1000", NoSSEHeader: true, ScMaxEachPostBytes: "500000-1000000", ScMaxBufferedPosts: 30, ScStreamUpServerSecs: "20-80",
				Xmux:     &inbounds.Xmux{MaxConcurrency: "16-32", HMaxRequestTimes: "600-900", HKeepAlivePeriod: 10},
				Download: &inbounds.XHTTPDownload{Address: "cdn.example.com", Port: 443, SNI: "cdn.example.com"}}}}),
		mk(inbounds.View{ID: 8, Remark: "kcp", Protocol: "vless", Port: 42008, Network: "kcp",
			Advanced: &inbounds.Advanced{KCP: &inbounds.KCPExtra{MTU: 1350, TTI: 20, UplinkCapacity: 5, DownlinkCapacity: 20, CwndMultiplier: 2, MaxSendingWindow: 2 << 20, Header: "wechat-video", Seed: "s33d"},
				Masks: &inbounds.Masks{Noise: &inbounds.NoiseMask{Rand: "10-20", Delay: "5-10"}}}}),
		mk(inbounds.View{ID: 9, Remark: "ws", Protocol: "vless", Port: 42009, Network: "ws", Transport: inbounds.Transport{Path: "/w"},
			Advanced: &inbounds.Advanced{WS: &inbounds.WSExtra{Headers: map[string]string{"X-A": "1"}, HeartbeatPeriod: 30}, Sockopt: &inbounds.Sockopt{AcceptProxyProtocol: true}}}),
		mk(inbounds.View{ID: 10, Remark: "grpc", Protocol: "trojan", Port: 42010, Network: "grpc", Security: "tls", TLS: tlsOf(), Transport: inbounds.Transport{ServiceName: "g"},
			Advanced: &inbounds.Advanced{GRPC: &inbounds.GRPCExtra{UserAgent: "ua", IdleTimeout: 60, HealthCheckTimeout: 20, PermitWithoutStream: true, InitialWindowsSize: 65536}}}),
		mk(inbounds.View{ID: 11, Remark: "hy2", Protocol: "hysteria", Port: 42011, TLS: &inbounds.TLS{CertID: &certID}, Settings: inbounds.Settings{ObfsPassword: "ob"},
			Advanced: &inbounds.Advanced{Hysteria: &inbounds.HysteriaExtra{Congestion: "brutal", BrutalUpMbps: 100, BrutalDownMbps: 200, UDPIdleTimeout: 120,
				Masquerade: &inbounds.Masquerade{Type: "string", Content: "hello", StatusCode: 200, Headers: map[string]string{"Server": "nginx"}}},
				Masks: &inbounds.Masks{Noise: &inbounds.NoiseMask{Rand: "20-30"}}}}),
		mk(inbounds.View{ID: 12, Remark: "hy2-proxy", Protocol: "hysteria", Port: 42012, TLS: &inbounds.TLS{CertID: &certID},
			Advanced: &inbounds.Advanced{Hysteria: &inbounds.HysteriaExtra{Congestion: "bbr", Masquerade: &inbounds.Masquerade{Type: "proxy", URL: "https://www.example.com", RewriteHost: true}}}}),
		mk(inbounds.View{ID: 13, Remark: "hu", Protocol: "vmess", Port: 42013, Network: "httpupgrade", Transport: inbounds.Transport{Path: "/hu"},
			Advanced: &inbounds.Advanced{WS: &inbounds.WSExtra{Headers: map[string]string{"X-B": "2"}}, Sockopt: &inbounds.Sockopt{V6Only: true}}}),
		mk(inbounds.View{ID: 14, Remark: "ss-kcp", Protocol: "shadowsocks", Port: 42014, Network: "kcp", Advanced: &inbounds.Advanced{KCP: &inbounds.KCPExtra{Header: "dns", HeaderDomain: "a.example.com"}}}),
		mk(inbounds.View{ID: 15, Remark: "socks-sniff", Protocol: "socks", Port: 42015, Sniffing: true, Advanced: &inbounds.Advanced{Sniffing: &inbounds.SniffOpts{RouteOnly: true}}}),
	}
}

// TestBuildAdvanced renders every advanced setting; with VEILON_TEST_XRAY
// the real Xray must accept the config (xray run -test).
func TestBuildAdvanced(t *testing.T) {
	g := testutil.DB(t)
	dir := t.TempDir()
	cs := &certs.Service{DB: g, DataDir: dir}
	certID, err := cs.SelfSigned("adv.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ibs := advancedInbounds(t, certID)
	u := db.User{ID: 7, Username: "alice", UUID: "11111111-2222-4333-8444-555555555555", Password: "pw7pw7pw7"}
	users := map[int64][]db.User{}
	for _, ib := range ibs {
		users[ib.ID] = []db.User{u}
	}
	b, err := Build(&Input{Inbounds: ibs, Users: users, APIPort: 42099, DataDir: dir, AssetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []map[string]any `json:"inbounds"`
		Routing  struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	by := map[string]map[string]any{}
	for _, x := range cfg.Inbounds {
		by[x["tag"].(string)] = x
	}
	if len(by) != len(ibs)+1 {
		t.Fatalf("inbounds rendered %d, want %d (skipped?)", len(by), len(ibs)+1)
	}
	js := func(v any) string { x, _ := json.Marshal(v); return string(x) }
	check := func(tag, want string) {
		t.Helper()
		if s := js(by[tag]); !strings.Contains(s, want) {
			t.Errorf("%s: missing %s in %s", tag, want, s)
		}
	}
	check("inbound-1", `"fallbacks":[{"dest":8080},{"dest":"127.0.0.1:8081","path":"/ws","xver":1},{"alpn":"h2","dest":"/run/x.sock","name":"a.example.com"}]`)
	check("inbound-1", `"echServerKeys":`)
	check("inbound-1", `"minVersion":"1.2"`)
	check("inbound-1", `"rejectUnknownSni":true`)
	check("inbound-1", `"acceptProxyProtocol":true`)
	check("inbound-1", `"tcpCongestion":"bbr"`)
	check("inbound-1", `"finalmask":{"tcp":[{"settings":{"delay":"10-20","length":"100-200","packets":"tlshello"},"type":"fragment"}]}`)
	check("inbound-1", `"sniffing":{"destOverride":["http","tls"],"domainsExcluded":["courier.push.apple.com"],"enabled":true,"routeOnly":true}`)
	check("inbound-2", `"mldsa65Seed":"`+ibs[1].Advanced.Reality.MLDSA65Seed+`"`)
	check("inbound-2", `"limitFallbackUpload":{"afterBytes":1048576,"burstBytesPerSec":200000,"bytesPerSec":100000}`)
	check("inbound-2", `"reverse":{"tag":"reverse-2"}`)
	check("inbound-3", `"rawSettings":{"header":{`)
	check("inbound-3", `"metadataOnly":true`)
	check("inbound-5", `"network":"tcp"`)
	check("inbound-7", `"xPaddingBytes":"100-1000"`)
	check("inbound-7", `"noSSEHeader":true`)
	check("inbound-8", `"network":"kcp"`)
	check("inbound-8", `"udp":[{"settings":{"noise":[{"delay":"5-10","rand":"10-20"}]},"type":"noise"},{"settings":{"header":"wechat"},"type":"mkcp-legacy"},{"settings":{"value":"s33d"},"type":"mkcp-legacy"}]`)
	check("inbound-9", `"heartbeatPeriod":30`)
	check("inbound-10", `"permit_without_stream":true`)
	check("inbound-11", `"quicParams":{"brutalDown":"200 mbps","brutalUp":"100 mbps","congestion":"brutal"}`)
	check("inbound-11", `"masquerade":{"content":"hello","headers":{"Server":"nginx"},"statusCode":200,"type":"string"}`)
	check("inbound-11", `"udp":[{"settings":{"noise":[{"rand":"20-30"}]},"type":"noise"},{"settings":{"password":"ob"},"type":"salamander"}]`)
	check("inbound-14", `"settings":{"header":"dns","value":"a.example.com"}`)
	var rev bool
	for _, r := range cfg.Routing.Rules {
		if r["outboundTag"] == "reverse-2" && js(r["inboundTag"]) == `["inbound-2","inbound-3"]` {
			rev = true
		}
	}
	if !rev {
		t.Fatalf("reverse rule missing: %s", js(cfg.Routing.Rules))
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(dir, "adv.json")
		_ = os.WriteFile(f, b, 0o600)
		out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput()
		if err != nil {
			t.Fatalf("xray -test: %v\n%s", err, out)
		}
		t.Logf("xray -test: %s", out)
		if d := os.Getenv("VEILON_DUMP"); d != "" {
			_ = os.WriteFile(d, b, 0o600)
		}
	}
}

// Nodes run the same advanced settings: they arrive with the config push.
func TestNodeConfigAdvanced(t *testing.T) {
	gg, dir, rid, wid := nodeFixture(t)
	g := &inbounds.Service{DB: gg}
	n := db.Node{Name: "edge", Address: "10.0.0.2", APIPort: 62050, Enabled: true}
	gg.Create(&n)
	ml, _ := inbounds.GenerateMLDSA65("")
	v, _ := g.Get(rid)
	v.NodeIDs = []int64{n.ID}
	v.Advanced = &inbounds.Advanced{Fallbacks: []inbounds.Fallback{{Dest: "8080"}}, Reality: &inbounds.RealityExtra{MLDSA65Seed: ml.Server},
		Sockopt: &inbounds.Sockopt{AcceptProxyProtocol: true}, Reverse: &inbounds.Reverse{Enabled: true, RouteSelf: true}}
	if _, err := g.Update(v); err != nil {
		t.Fatal(err)
	}
	w, _ := g.Get(wid)
	w.NodeIDs = []int64{n.ID}
	w.Advanced = &inbounds.Advanced{WS: &inbounds.WSExtra{HeartbeatPeriod: 15}, Sniffing: &inbounds.SniffOpts{RouteOnly: true}}
	w.Sniffing = true
	if _, err := g.Update(w); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := NodeConfig(gg, dir, []int64{rid, wid}, 62051)
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	for _, want := range []string{`"fallbacks"`, `"mldsa65Seed": "` + ml.Server + `"`, `"acceptProxyProtocol": true`, `"reverse-` + strconv.FormatInt(rid, 10) + `"`, `"heartbeatPeriod": 15`, `"routeOnly": true`} {
		if !strings.Contains(s, want) {
			t.Errorf("node config misses %s", want)
		}
	}
	if bin := os.Getenv("VEILON_TEST_XRAY"); bin != "" {
		f := filepath.Join(dir, "node.json")
		_ = os.WriteFile(f, cfg, 0o600)
		if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
			t.Fatalf("xray -test: %v\n%s", err, out)
		}
	}
}
