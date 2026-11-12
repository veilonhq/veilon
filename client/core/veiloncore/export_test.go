package veiloncore

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var extraLinks = []string{
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@[2001:db8::1]:443?security=tls&sni=a.example.com&type=grpc&serviceName=g1&alpn=h2,http/1.1&fp=chrome&allowInsecure=1#v6 grpc",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@198.51.100.1:443?type=xhttp&path=%2Fx&mode=packet-up&extra=%7B%22a%22%3A1%7D&security=tls&encryption=mlkem768x25519plus.native.0rtt.abc#xhttp enc",
	"trojan://p%40ss@198.51.100.4:443?type=ws&path=%2Fws%3Fed%3D2048&host=cdn.example.com&sni=cdn.example.com#tr ws",
	"ss://YWVzLTI1Ni1nY206cGFzcw@198.51.100.5:8388/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com#ss plugin",
	"hysteria2://user:pass@198.51.100.6:20000-30000?sni=h.example.com&insecure=1&alpn=h3&upmbps=50&downmbps=100#hy2 hop",
	"tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@198.51.100.7:443?congestion_control=bbr&udp_relay_mode=quic&sni=t.example.com&alpn=h3&allow_insecure=1#tuic",
	"wireguard://0mske3O80qVSmGm2nHiR2us1ZgH8GQwWFCbDgZJmNt4%3D@198.51.100.8:51820?publickey=uoiOdrOiF%2FSKQmzpqsctzB7H4J6tS%2BDT5XXeSeSWVEY%3D&address=10.7.0.2%2F32,fd00::2%2F128&mtu=1280&reserved=1,2,3&jc=4&jmin=40&jmax=70&s1=52&s2=49&h1=1&h2=2&h3=3&h4=4#awg",
	"https://u:p@198.51.100.11:443?sni=h.example.com#https proxy",
	"tg://proxy?server=198.51.100.13&port=443&secret=ee00112233445566778899aabbccddeeff",
}

// conn returns the connection-relevant view of a profile.
func connView(p *Profile) Profile {
	c := *p
	// Warning is an import-time hint (privateHost), not a connection field.
	c.ID, c.Link, c.SubID, c.Warning = "", "", "", ""
	if c.Type == TypeMTProto {
		c.Name = ""
	}
	return c
}

func roundTrip(t *testing.T, link string) {
	t.Helper()
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	// Export from the profile JSON as the app would.
	q := *p
	l, err := ToLink(&q)
	if err != nil {
		t.Fatalf("%s: ToLink: %v", p.Name, err)
	}
	p2, err := ParseLink(l)
	if err != nil {
		t.Fatalf("%s: re-parse %q: %v", p.Name, l, err)
	}
	a, b := connView(p), connView(p2)
	if !reflect.DeepEqual(a, b) {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		t.Errorf("%s: round trip differs\n  in:  %s\n  out: %s\n  link: %s", p.Name, ja, jb, l)
	}
}

func TestToLinkRoundTrip(t *testing.T) {
	for _, c := range configCases {
		t.Run(c.name, func(t *testing.T) { roundTrip(t, c.link) })
	}
	for _, l := range extraLinks {
		roundTrip(t, l)
	}
}

func TestProfileJSONAPI(t *testing.T) {
	js := ParseProfileLink(configCases[0].link)
	if strings.Contains(js, `"error"`) {
		t.Fatal(js)
	}
	var p Profile
	_ = json.Unmarshal([]byte(js), &p)
	p.Name, p.Port, p.SNI = "edited", 8443, "edited.example.com"
	p.Type = " VLESS "
	b, _ := json.Marshal(p)
	norm := NormalizeProfile(string(b))
	var q Profile
	if err := json.Unmarshal([]byte(norm), &q); err != nil || q.Type != TypeVLESS || q.ID != p.ID {
		t.Fatalf("normalize: %s", norm)
	}
	// "id":"" -> recomputed from connection fields.
	q.ID = ""
	b, _ = json.Marshal(q)
	var r Profile
	_ = json.Unmarshal([]byte(NormalizeProfile(string(b))), &r)
	if r.ID == "" || r.ID == p.ID {
		t.Errorf("id not recomputed: %q", r.ID)
	}
	var lk map[string]string
	_ = json.Unmarshal([]byte(ProfileToLink(norm)), &lk)
	if !strings.HasPrefix(lk["link"], "vless://") || !strings.Contains(lk["link"], ":8443") || !strings.Contains(lk["link"], "sni=edited.example.com") {
		t.Errorf("ProfileToLink: %v", lk)
	}
	if out := ProfileToLink(`{"type":"masque","server":"m.example.com","port":443}`); !strings.Contains(out, `"error"`) {
		t.Errorf("masque should not export: %s", out)
	}
	if out := NormalizeProfile("{bad"); !strings.Contains(out, `"error"`) {
		t.Errorf("bad json: %s", out)
	}
	if out := ParseProfileLink("nope://x"); !strings.Contains(out, `"error"`) {
		t.Errorf("bad link: %s", out)
	}
}

func TestAddressOverrideAllTypes(t *testing.T) {
	links := append([]string{}, extraLinks...)
	for _, c := range configCases {
		links = append(links, c.link)
	}
	for _, l := range links {
		p, err := ParseLink(l)
		if err != nil || p.Type == TypeMTProto {
			continue
		}
		origSNI, origHost, origServer := p.SNI, p.Host, p.Server
		p.AddressOverride = "203.0.113.7"
		q := p.withAddressOverride()
		if q.Server != "203.0.113.7" {
			t.Errorf("%s: server %q", p.Name, q.Server)
		}
		if p.Server != origServer {
			t.Errorf("%s: original mutated", p.Name)
		}
		if origSNI != "" && q.SNI != origSNI {
			t.Errorf("%s: user SNI changed %q -> %q", p.Name, origSNI, q.SNI)
		}
		if origHost != "" && q.Host != origHost {
			t.Errorf("%s: user Host changed", p.Name)
		}
		// Generated configs carry the override.
		b, _ := json.Marshal(p)
		if cfg := BuildConfig(string(b), ""); CoreOf(p) == CoreSingbox && !strings.Contains(cfg, "not supported in this build") && !strings.Contains(cfg, "203.0.113.7") {
			t.Errorf("%s: BuildConfig lacks override: %.300s", p.Name, cfg)
		}
	}

	// Hostname server: SNI / Host header keep the hostname.
	p, _ := ParseLink("vless://b831381d-6324-4d53-ad4f-8cda48b30811@cdn.example.com:443?security=tls&type=ws&path=%2Fws#x")
	p.AddressOverride = "[2001:db8::7]:8443"
	q := p.withAddressOverride()
	if q.Server != "2001:db8::7" || q.Port != 8443 || q.SNI != "cdn.example.com" || q.Host != "cdn.example.com" {
		t.Errorf("hostname override: %+v", q)
	}
	// No override: same pointer.
	if d2 := (&Profile{Type: TypeHTTP, Server: "a"}); d2.withAddressOverride() != d2 {
		t.Error("no-op override copied")
	}
	// Not part of the id.
	a := &Profile{Type: TypeHTTP, Server: "10.0.0.1", Port: 8080}
	bb := *a
	bb.AddressOverride = "1.2.3.4"
	if computeID(a) != computeID(&bb) {
		t.Fatal("override changed id")
	}
}
