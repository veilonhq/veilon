package veiloncore

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/curve25519"
)

const tUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

func vmessLinkOf(m map[string]string) string {
	b, _ := json.Marshal(m)
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

// vlessEnc returns a syntactically valid client VLESS encryption string.
func vlessEnc(t *testing.T) string {
	priv := make([]byte, 32)
	_, _ = rand.Read(priv)
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return "mlkem768x25519plus.native.0rtt." + base64.RawURLEncoding.EncodeToString(pub)
}

// xrayCheck builds the Xray config for p and validates it with core.New.
func xrayCheck(t *testing.T, p *Profile) obj {
	t.Helper()
	cfg, err := xrayConfig(p, 10808, "u", "p", DefaultSettings())
	if err != nil {
		t.Fatalf("%s: xrayConfig: %v", p.Name, err)
	}
	if err := checkXrayConfig(cfg); err != nil {
		b, _ := json.Marshal(cfg["outbounds"].([]any)[0])
		t.Fatalf("%s: core.New: %v\n%s", p.Name, err, b)
	}
	return cfg["outbounds"].([]any)[0].(obj)
}

func streamOf(o obj) map[string]any {
	b, _ := json.Marshal(o["streamSettings"])
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func TestXrayTransportsFromLinks(t *testing.T) {
	fmTCP := `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}}]}`
	fmUDP := `{"udp":[{"type":"mkcp-legacy","settings":{"header":"srtp"}}]}`
	cases := []struct {
		name, link string
		core       Core
		check      func(t *testing.T, p *Profile, ss map[string]any)
	}{
		{"raw-http", "vless://" + tUUID + "@raw.test:80?type=raw&headerType=http&path=%2Fa%2C%2Fb&host=x.example.com,y.example.com&security=none#raw-http", CoreSingbox,
			func(t *testing.T, p *Profile, ss map[string]any) {
				h := ss["tcpSettings"].(map[string]any)["header"].(map[string]any)
				req := h["request"].(map[string]any)
				if h["type"] != "http" || len(req["path"].([]any)) != 2 || len(req["headers"].(map[string]any)["Host"].([]any)) != 2 {
					t.Errorf("tcp header: %v", h)
				}
			}},
		{"kcp-legacy", "vless://" + tUUID + "@kcp.test:443?type=kcp&headerType=wechat-video&seed=s3cr3t&mtu=1350&tti=20&security=none#kcp", CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				k := ss["kcpSettings"].(map[string]any)
				if k["mtu"].(float64) != 1350 || k["tti"].(float64) != 20 {
					t.Errorf("kcp: %v", k)
				}
				m := ss["finalmask"].(map[string]any)["udp"].([]any)[0].(map[string]any)
				if m["type"] != "mkcp-legacy" || m["settings"].(map[string]any)["header"] != "wechat" {
					t.Errorf("mask: %v", m)
				}
			}},
		{"kcp-seed-only", "vless://" + tUUID + "@kcp.test:443?type=mkcp&seed=s3cr3t#kcp2", CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				m := ss["finalmask"].(map[string]any)["udp"].([]any)[0].(map[string]any)
				if m["settings"].(map[string]any)["value"] != "s3cr3t" {
					t.Errorf("seed mask: %v", m)
				}
			}},
		{"kcp-fm", "vless://" + tUUID + "@kcp.test:443?type=kcp&fm=" + url.QueryEscape(fmUDP) + "#kcp3", CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				if n := len(ss["finalmask"].(map[string]any)["udp"].([]any)); n != 1 {
					t.Errorf("udp masks %d", n)
				}
			}},
		{"ws-tls", "trojan://pw@198.51.100.2:443?type=ws&path=%2Fws&host=cdn.example.com&security=tls&sni=cdn.example.com&fp=chrome&alpn=http%2F1.1#ws", CoreSingbox, nil},
		{"grpc-authority", "vless://" + tUUID + "@198.51.100.3:443?type=grpc&serviceName=svc&authority=auth.example.com&mode=multi&security=tls&sni=g.example.com#grpc", CoreSingbox,
			func(t *testing.T, p *Profile, ss map[string]any) {
				g := ss["grpcSettings"].(map[string]any)
				if g["authority"] != "auth.example.com" || g["multiMode"] != true {
					t.Errorf("grpc: %v", g)
				}
			}},
		{"httpupgrade", "vless://" + tUUID + "@hu.test:80?type=httpupgrade&path=%2Fhu&host=h.example.com#hu", CoreSingbox, nil},
		{"xhttp-auto-reality", "vless://" + tUUID + "@198.51.100.5:443?type=xhttp&path=%2Fx&mode=auto&security=reality&sni=www.microsoft.com&fp=chrome&pbk=nOgeQ91pGXuAOrULdAGVkC0sBrgLuKWXGq7tj0gqFEA&sid=6ba85179#xh-auto", CoreXray, nil},
		{"xhttp-packet-up", "vless://" + tUUID + "@198.51.100.5:443?type=xhttp&path=%2Fx&mode=packet-up&x_padding_bytes=100-1000&extra=" +
			url.QueryEscape(`{"mode":"packet-up","xPaddingBytes":"100-1000","scMaxEachPostBytes":"500000","headers":{"X-A":"1"}}`) + "&security=tls&sni=x.example.com#xh-pu", CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				x := ss["xhttpSettings"].(map[string]any)
				if x["mode"] != "packet-up" || x["xPaddingBytes"] != "100-1000" || x["extra"] == nil {
					t.Errorf("xhttp: %v", x)
				}
			}},
		{"xhttp-stream-up", "vless://" + tUUID + "@198.51.100.5:443?type=xhttp&path=%2Fx&mode=stream-up&security=tls&sni=x.example.com#xh-su", CoreXray, nil},
		{"xhttp-stream-one", "vless://" + tUUID + "@198.51.100.5:443?type=splithttp&path=%2Fx&mode=stream-one&security=tls&sni=x.example.com#xh-so", CoreXray, nil},
		{"tcp-mask-trojan", "trojan://pw@198.51.100.6:443?type=tcp&security=tls&sni=t.example.com&fm=" + url.QueryEscape(fmTCP) + "#fm", CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				m := ss["finalmask"].(map[string]any)["tcp"].([]any)[0].(map[string]any)
				if m["type"] != "fragment" {
					t.Errorf("tcp mask: %v", m)
				}
			}},
		{"ss-ws-fm", "ss://YWVzLTI1Ni1nY206cGFzcw@198.51.100.7:443/?type=ws&path=%2Fss&host=s.example.com&security=tls&sni=s.example.com&fm=" + url.QueryEscape(fmTCP) + "#ss-ws", CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				if ss["network"] != "ws" || ss["security"] != "tls" {
					t.Errorf("ss stream: %v", ss)
				}
			}},
		{"vmess-kcp", vmessLinkOf(map[string]string{"v": "2", "ps": "vm-kcp", "add": "198.51.100.8", "port": "443", "id": tUUID, "aid": "0",
			"net": "kcp", "type": "dtls", "path": "seed1", "mtu": "1200", "tti": "30", "tls": ""}), CoreXray,
			func(t *testing.T, p *Profile, ss map[string]any) {
				if p.Seed != "seed1" || p.HeaderType != "dtls" || p.KCP == nil || p.KCP.MTU != 1200 {
					t.Errorf("vmess kcp parse: %+v %+v", p, p.KCP)
				}
			}},
		{"vmess-grpc-fm", vmessLinkOf(map[string]string{"v": "2", "ps": "vm-grpc", "add": "198.51.100.8", "port": "443", "id": tUUID, "aid": "0",
			"net": "grpc", "path": "svc", "authority": "a.example.com", "type": "multi", "tls": "tls", "sni": "v.example.com", "fm": fmTCP}), CoreXray, nil},
		{"vless-ech-vcn", "vless://" + tUUID + "@198.51.100.9:443?type=tcp&security=tls&sni=e.example.com&vcn=e.example.com&fp=chrome#ech", CoreXray, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseLink(c.link)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := CoreOf(p); got != c.core {
				t.Errorf("core %s, want %s", got, c.core)
			}
			// Every case must also be valid for Xray (core.New) ...
			o := xrayCheck(t, p)
			if c.check != nil {
				c.check(t, p, streamOf(o))
			}
			// ... and for the engine that will actually carry it.
			if _, err := checkProfile(p, DefaultSettings(), false); err != nil {
				t.Errorf("checkProfile: %v", err)
			}
			// Share-link round trip.
			roundTrip(t, c.link)
		})
	}
}

func TestXrayJSONSubscriptionTransports(t *testing.T) {
	enc := vlessEnc(t)
	sub := `{"outbounds":[
 {"tag":"xdrive-local","protocol":"vless","settings":{"vnext":[{"address":"storage.example.com","port":443,"users":[{"id":"` + tUUID + `","encryption":"` + enc + `"}]}]},
  "streamSettings":{"network":"xdrive","xdriveSettings":{"service":"local","remoteFolder":"/tmp/veilon-xdrive-test"},"security":"none",
   "sockopt":{"tcpFastOpen":true,"domainStrategy":"UseIPv4"}},"mux":{"enabled":true,"concurrency":4}},
 {"tag":"kcp-json","protocol":"vmess","settings":{"vnext":[{"address":"198.51.100.20","port":443,"users":[{"id":"` + tUUID + `","security":"auto"}]}]},
  "streamSettings":{"network":"kcp","kcpSettings":{"mtu":1350,"tti":50,"uplinkCapacity":12,"downlinkCapacity":100,"cwndMultiplier":2,"maxSendingWindow":2097152},
   "finalmask":{"udp":[{"type":"mkcp-legacy","settings":{"header":"utp"}}]},"security":"none"}},
 {"tag":"raw-http-json","protocol":"trojan","settings":{"servers":[{"address":"raw.test","port":80,"password":"pw"}]},
  "streamSettings":{"network":"raw","rawSettings":{"header":{"type":"http","request":{"path":["/p1","/p2"],"headers":{"Host":["h1.example.com"]}}}},"security":"none"}},
 {"tag":"xhttp-json","protocol":"vless","settings":{"vnext":[{"address":"198.51.100.22","port":443,"users":[{"id":"` + tUUID + `","encryption":"none"}]}]},
  "streamSettings":{"network":"xhttp","xhttpSettings":{"path":"/xh","host":"x.example.com","mode":"stream-one","xPaddingBytes":"200-800","extra":{"noGRPCHeader":true}},
   "security":"tls","tlsSettings":{"serverName":"x.example.com","fingerprint":"chrome","alpn":["h2"]},
   "finalmask":{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"50-100","delay":"5-10"}}]},
   "sockopt":{"tcpCongestion":"bbr","mark":255}}},
 {"tag":"grpc-json","protocol":"shadowsocks","settings":{"servers":[{"address":"198.51.100.23","port":443,"method":"aes-256-gcm","password":"pw"}]},
  "streamSettings":{"network":"grpc","grpcSettings":{"serviceName":"g","authority":"ga.example.com","multiMode":true},"security":"tls","tlsSettings":{"serverName":"g.example.com"}}}
]}`
	res := ParseContent(sub)
	if len(res.Profiles) != 5 {
		t.Fatalf("profiles %d, errors %v", len(res.Profiles), res.Errors)
	}
	by := map[string]*Profile{}
	for _, p := range res.Profiles {
		by[p.Name] = p
	}
	xd := by["xdrive-local"]
	if xd == nil || xd.Network != "xdrive" || !strings.Contains(xd.XDrive, `"service":"local"`) || xd.Sockopt == "" || CoreOf(xd) != CoreXray {
		t.Fatalf("xdrive parse: %+v", xd)
	}
	o := xrayCheck(t, xd)
	if m, _ := o["mux"].(obj); m == nil || m["enabled"] != true {
		t.Errorf("xdrive mux: %v", o["mux"])
	}
	if _, err := ToLink(xd); err == nil {
		t.Error("xdrive must not export a link")
	}
	// xdrive with a flow / without encryption is rejected by Xray (not a panic).
	bad := *xd
	bad.Encryption = ""
	if _, err := checkProfile(&bad, DefaultSettings(), false); err == nil {
		t.Error("xdrive without VLESS encryption should fail validation")
	}

	k := by["kcp-json"]
	if k.KCP == nil || k.KCP.UplinkCapacity != 12 || k.KCP.CwndMultiplier != 2 || !strings.Contains(k.FinalMask, "mkcp-legacy") {
		t.Errorf("kcp json: %+v %+v", k, k.KCP)
	}
	ss := streamOf(xrayCheck(t, k))
	if n := len(ss["finalmask"].(map[string]any)["udp"].([]any)); n != 1 {
		t.Errorf("kcp masks duplicated: %d", n)
	}
	r := by["raw-http-json"]
	if r.Network != "tcp" || r.HeaderType != "http" || r.Path != "/p1,/p2" || r.Host != "h1.example.com" {
		t.Errorf("raw http json: %+v", r)
	}
	xrayCheck(t, r)
	x := by["xhttp-json"]
	if x.Mode != "stream-one" || x.XPaddingBytes != "200-800" || x.FinalMask == "" || x.Sockopt == "" {
		t.Errorf("xhttp json: %+v", x)
	}
	xs := streamOf(xrayCheck(t, x))
	if xs["sockopt"].(map[string]any)["mark"].(float64) != 255 {
		t.Errorf("sockopt: %v", xs["sockopt"])
	}
	g := by["grpc-json"]
	if g.Authority != "ga.example.com" || CoreOf(g) != CoreXray {
		t.Errorf("ss grpc json: %+v core %s", g, CoreOf(g))
	}
	xrayCheck(t, g)
	for _, p := range res.Profiles {
		if _, err := checkProfile(p, DefaultSettings(), false); err != nil {
			t.Errorf("%s: checkProfile: %v", p.Name, err)
		}
	}
}

func TestXrayBadRawJSONIsAnError(t *testing.T) {
	p := &Profile{Type: TypeVLESS, Server: "198.51.100.1", Port: 443, UUID: tUUID, Network: "tcp", FinalMask: "{not json"}
	if _, err := xrayOutbound(p); err == nil {
		t.Error("bad finalmask JSON must fail")
	}
	p.FinalMask = `{"tcp":[{"type":"no-such-mask"}]}`
	cfg, err := xrayConfig(p, 1, "u", "p", DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := checkXrayConfig(cfg); err == nil {
		t.Error("unknown mask type must fail core.New")
	}
}

// Xray refuses cleartext VLESS/Trojan to a public IP or public domain (only
// private IPs and private names such as *.test are allowed). Such profiles go to
// sing-box unless they need an Xray-only feature; then a clear, translatable
// error is returned instead of an Xray build error or a panic.
func TestCleartextPublicIPRouting(t *testing.T) {
	cases := []struct {
		name, link string
		core       Core
		wantErr    error
	}{
		{"vless-raw-http-ip", "vless://" + tUUID + "@198.51.100.1:80?type=raw&headerType=http&path=%2Fa&host=x.example.com&security=none#a", CoreSingbox, nil},
		{"vless-tcp-ip", "vless://" + tUUID + "@198.51.100.1:80?type=tcp&security=none#b", CoreSingbox, nil},
		{"vless-ws-ip", "vless://" + tUUID + "@198.51.100.1:80?type=ws&path=%2Fws&host=w.example.com#c", CoreSingbox, nil},
		{"vless-hu-ip", "vless://" + tUUID + "@198.51.100.4:80?type=httpupgrade&path=%2Fhu&host=h.example.com#d", CoreSingbox, nil},
		{"trojan-grpc-ip", "trojan://pw@198.51.100.2:80?type=grpc&serviceName=g&security=none#e", CoreSingbox, nil},
		{"trojan-ip6", "trojan://pw@[2001:4860::1]:80?type=tcp&security=none#f", CoreSingbox, nil},
		{"vless-kcp-ip", "vless://" + tUUID + "@198.51.100.1:443?type=kcp&seed=s#g", CoreXray, ErrXrayCleartextPublicIP},
		{"vless-xhttp-ip", "vless://" + tUUID + "@198.51.100.1:80?type=xhttp&path=%2Fx#h", CoreXray, ErrXrayCleartextPublicIP},
		{"trojan-fm-ip", "trojan://pw@198.51.100.6:80?type=tcp&security=none&fm=" + url.QueryEscape(`{"tcp":[{"type":"fragment","settings":{"packets":"1-3","length":"10-20","delay":"1-2"}}]}`) + "#i", CoreXray, ErrXrayCleartextPublicIP},
		{"vless-kcp-private", "vless://" + tUUID + "@10.0.0.1:443?type=kcp#j", CoreXray, nil},
		{"vless-kcp-public-domain", "vless://" + tUUID + "@kcp.example.com:443?type=kcp#k", CoreXray, ErrXrayCleartextPublicIP},
		{"vless-kcp-private-domain", "vless://" + tUUID + "@kcp.test:443?type=kcp#k2", CoreXray, nil},
		{"vless-tcp-public-domain", "vless://" + tUUID + "@v.example.com:80?type=tcp#k3", CoreSingbox, nil},
		{"vless-kcp-ip-tls", "vless://" + tUUID + "@198.51.100.1:443?type=xhttp&path=%2Fx&security=tls&sni=x.example.com#l", CoreXray, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseLink(c.link)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := CoreOf(p); got != c.core {
				t.Errorf("core %s, want %s (%+v)", got, c.core, p)
			}
			_, err = checkProfile(p, DefaultSettings(), false)
			if c.wantErr == nil && err != nil {
				t.Errorf("checkProfile: %v", err)
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Errorf("checkProfile: %v, want %v", err, c.wantErr)
			}
		})
	}
	// The address override is what the engine dials: a domain profile whose
	// override is a bare public IP must hit the same rule.
	p, _ := ParseLink("vless://" + tUUID + "@kcp.test:443?type=kcp#ov")
	p.AddressOverride = "198.51.100.30"
	if _, err := checkProfile(p, DefaultSettings(), false); !errors.Is(err, ErrXrayCleartextPublicIP) {
		t.Errorf("override: %v", err)
	}
	// The JSON API returns the bare key so the app can translate it.
	b, _ := json.Marshal(p)
	if got := CheckProfile(string(b), ""); got != "err.xrayCleartextPublicIP" {
		t.Errorf("CheckProfile = %q", got)
	}
}
