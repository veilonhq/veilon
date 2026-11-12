package veiloncore

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/miekg/dns"
	"golang.org/x/crypto/curve25519"
)

type wgKey struct{ priv, pub [32]byte }

func newWGKey(t *testing.T) wgKey {
	t.Helper()
	var k wgKey
	if _, err := rand.Read(k.priv[:]); err != nil {
		t.Fatal(err)
	}
	k.priv[0] &= 248
	k.priv[31] = (k.priv[31] & 127) | 64
	pub, err := curve25519.X25519(k.priv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	copy(k.pub[:], pub)
	return k
}

func b64(b [32]byte) string { return base64.StdEncoding.EncodeToString(b[:]) }

// Amnezia parameter sets as the Amnezia app exports them.
var awgVersions = []struct {
	name string
	// conf lines for the client [Interface]
	client string
	// UAPI lines for the server device
	server string
}{
	{"awg1.0",
		"Jc = 4\nJmin = 40\nJmax = 70\nS1 = 52\nS2 = 49\nH1 = 1213945451\nH2 = 1513475432\nH3 = 1784535361\nH4 = 1904584353\n",
		"jc=4\njmin=40\njmax=70\ns1=52\ns2=49\nh1=1213945451\nh2=1513475432\nh3=1784535361\nh4=1904584353\n"},
	{"awg1.5",
		// I1 (QUIC-like signature packet), plus the 1.5-beta keys J1-J3 / Itime
		// that newer clients ignore.
		"Jc = 3\nJmin = 10\nJmax = 50\nS1 = 98\nS2 = 120\nH1 = 1437062130\nH2 = 1966447713\nH3 = 2060304813\nH4 = 1281498765\n" +
			"I1 = <b 0xc70000000108ce1bf31eec7d93360000449e227e4596ed7f75c4d35ce31880b4133107c822c6355b51f0d7c1bba96d5c210a48aca01885fed0871cfc37d59137d73b506dc013bb4a13c060ca5b04b7ae215af71e37d6e8ff1db235f9fe0c25cb8b492471054a7c8d0d6077d430d07f6e87a8699287f6e69f54263c7334a8e144a29851429bf2e350e519445172d36953e96085110ce1fb641e5efad42c0feb4711ece959b72cc4d6f3c1e83251adb572b921534f6ac4b10927167f41fe50040a75acef62f45bded67c0b45b9d655ce374589cad6f6b3c9a63ec3ad2e7a1e2c3de2a5a8f6>\n" +
			"I2 = <r 32>\nJ1 = <r 16>\nJ2 = <t>\nItime = 120\n",
		"jc=3\njmin=10\njmax=50\ns1=98\ns2=120\nh1=1437062130\nh2=1966447713\nh3=2060304813\nh4=1281498765\n" +
			"i1=<b 0xc70000000108ce1bf31eec7d93360000449e227e4596ed7f75c4d35ce31880b4133107c822c6355b51f0d7c1bba96d5c210a48aca01885fed0871cfc37d59137d73b506dc013bb4a13c060ca5b04b7ae215af71e37d6e8ff1db235f9fe0c25cb8b492471054a7c8d0d6077d430d07f6e87a8699287f6e69f54263c7334a8e144a29851429bf2e350e519445172d36953e96085110ce1fb641e5efad42c0feb4711ece959b72cc4d6f3c1e83251adb572b921534f6ac4b10927167f41fe50040a75acef62f45bded67c0b45b9d655ce374589cad6f6b3c9a63ec3ad2e7a1e2c3de2a5a8f6>\ni2=<r 32>\n"},
	{"awg2.0",
		"Jc = 5\nJmin = 64\nJmax = 256\nS1 = 33\nS2 = 71\nS3 = 18\nS4 = 9\nH1 = 100000-200000\nH2 = 300000 - 400000\nH3 = 500000-600000\nH4 = 700000-800000\nI1 = <r 2><b 0x858000010001000000000669636c6f756403636f6d0000010001c00c000100010000105a00044d583737>\n",
		"jc=5\njmin=64\njmax=256\ns1=33\ns2=71\ns3=18\ns4=9\nh1=100000-200000\nh2=300000-400000\nh3=500000-600000\nh4=700000-800000\ni1=<r 2><b 0x858000010001000000000669636c6f756403636f6d0000010001c00c000100010000105a00044d583737>\n"}, // AWG 3.1 (amneziawg-go v3, Run 105): header protection + random
	// trailers must match the server, so a client that drops them never
	// completes a handshake (the 1.0.3 "connects but no traffic" bug).
	{"awg3.1",
		"Jc = 4\nJmin = 49\nJmax = 297\nS1 = 16\nS2 = 16\nS3 = 16\nS4 = 16\nH1 = 100000-200000\nH2 = 300000-400000\nH3 = 500000-600000\nH4 = 700000-800000\n" +
			"HeaderProtectionKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nContentPaddingAddition = 24-64\nRekeyAfterTime = 104-131\nRekeyTimeout = 4-7\nRejectAfterTime = 165-244\n" +
			"KeepaliveTimeout = 8-11\nMaxHandshakeAttempts = 18-34\nRandomTrailers = on\nDisableCookies = on\n",
		"jc=4\njmin=49\njmax=297\ns1=16\ns2=16\ns3=16\ns4=16\nh1=100000-200000\nh2=300000-400000\nh3=500000-600000\nh4=700000-800000\n" +
			"header_protection_key=0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20\ncontent_padding_addition=24-64\nrandom_trailers=true\n"},
}

func clientConf(cli, srv, psk wgKey, endpoint, params string) string {
	return "[Interface]\nPrivateKey = " + b64(cli.priv) + "\nAddress = 10.9.0.2/32\nDNS = 10.9.0.1\n" + params +
		"\n[Peer]\nPublicKey = " + b64(srv.pub) + "\nPresharedKey = " + b64(psk.priv) +
		"\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = " + endpoint + "\nPersistentKeepalive = 25\n"
}

func TestParseAmneziaConfVersions(t *testing.T) {
	cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
	for _, v := range awgVersions {
		p, err := ParseWGConf(clientConf(cli, srv, psk, "203.0.113.7:51820", v.client), v.name)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		if p.Type != TypeAmneziaWG || p.WG.AWG == nil || CoreOf(p) != CoreAWG {
			t.Fatalf("%s: type %s", v.name, p.Type)
		}
		uapi, err := awgUAPI(p, "203.0.113.7:51820")
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(v.server), "\n") {
			if !strings.Contains(uapi, line+"\n") {
				t.Errorf("%s: UAPI lacks %q", v.name, line)
			}
		}
		for _, bad := range []string{"j1=", "itime=", " "} {
			for _, l := range strings.Split(uapi, "\n") {
				if strings.HasPrefix(l, bad) || (bad == " " && strings.Contains(l, " ") && !strings.HasPrefix(l, "i")) {
					t.Errorf("%s: bad UAPI line %q", v.name, l)
				}
			}
		}
		// amneziawg-go must accept the whole device configuration.
		tdev, _, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.9.0.2")}, nil, 1280)
		if err != nil {
			t.Fatal(err)
		}
		dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
		if err := dev.IpcSet(uapi); err != nil {
			t.Errorf("%s: IpcSet: %v\n%s", v.name, err, uapi)
		}
		dev.Close()
	}
}

func qCompress(t *testing.T, b []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(b)))
	zw := zlib.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func TestParseAmneziaVPNLink(t *testing.T) {
	cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
	for _, v := range awgVersions {
		conf := strings.Replace(clientConf(cli, srv, psk, "203.0.113.7:51820", v.client), "DNS = 10.9.0.1", "DNS = $PRIMARY_DNS, $SECONDARY_DNS", 1)
		last, _ := json.Marshal(map[string]any{"config": conf, "hostName": "203.0.113.7", "port": 51820, "mtu": "1376",
			"client_priv_key": b64(cli.priv), "server_pub_key": b64(srv.pub)})
		root, _ := json.Marshal(map[string]any{
			"containers":       []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(last), "port": "51820", "transport_proto": "udp"}}},
			"defaultContainer": "amnezia-awg", "description": "My Amnezia " + v.name, "dns1": "1.1.1.1", "dns2": "1.0.0.1", "hostName": "203.0.113.7",
		})
		for _, enc := range []string{
			base64.RawURLEncoding.EncodeToString(qCompress(t, root)),
			base64.URLEncoding.EncodeToString(qCompress(t, root)),
		} {
			p, err := ParseLink("vpn://" + enc)
			if err != nil {
				t.Fatalf("%s: %v", v.name, err)
			}
			if p.Type != TypeAmneziaWG || p.Server != "203.0.113.7" || p.Port != 51820 || p.WG.MTU != 1376 {
				t.Fatalf("%s: %+v", v.name, p)
			}
			if strings.Join(p.WG.DNS, ",") != "1.1.1.1,1.0.0.1" || p.Name != "My Amnezia "+v.name {
				t.Errorf("%s: dns %v name %q", v.name, p.WG.DNS, p.Name)
			}
			if _, err := awgUAPI(p, "203.0.113.7:51820"); err != nil {
				t.Error(err)
			}
		}
		// Whole-text import (ParseText / .vpn file).
		res := ParseContent("vpn://" + base64.RawURLEncoding.EncodeToString(qCompress(t, root)))
		if len(res.Profiles) != 1 {
			t.Fatalf("%s: ParseContent %+v", v.name, res)
		}
	}
	// Field-only last_config (no "config" text).
	last, _ := json.Marshal(map[string]any{"client_priv_key": b64(cli.priv), "server_pub_key": b64(srv.pub), "psk_key": b64(psk.priv),
		"client_ip": "10.8.1.3", "hostName": "203.0.113.9", "port": "40000", "Jc": "4", "Jmin": "40", "Jmax": "70", "S1": "52", "S2": "49",
		"H1": "1213945451", "H2": "1513475432", "H3": "1784535361", "H4": "1904584353"})
	root, _ := json.Marshal(map[string]any{"containers": []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(last)}}}, "hostName": "203.0.113.9"})
	p, err := ParseLink("vpn://" + base64.RawURLEncoding.EncodeToString(qCompress(t, root)))
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != TypeAmneziaWG || p.Port != 40000 || p.WG.AWG.Jc != 4 || p.WG.Address[0] != "10.8.1.3/32" {
		t.Fatalf("fields: %+v %+v", p, p.WG)
	}
}

// startAWGServer runs an in-process AmneziaWG server (amneziawg-go +
// netstack) on 127.0.0.1 with an HTTP 204 server and a DNS server inside
// the tunnel at 10.9.0.1.
func startAWGServer(t *testing.T, srv, cli, psk wgKey, params string) int {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	tdev, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.9.0.1")}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), awgServerLogger())
	uapi := fmt.Sprintf("private_key=%s\nlisten_port=%d\n%spublic_key=%s\npreshared_key=%s\nallowed_ip=10.9.0.2/32\n",
		hex.EncodeToString(srv.priv[:]), port, params, hex.EncodeToString(cli.pub[:]), hex.EncodeToString(psk.priv[:]))
	if err := dev.IpcSet(uapi); err != nil {
		t.Fatalf("server IpcSet: %v", err)
	}
	if err := dev.Up(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.Close)
	// The server is an amneziawg-go device too: same S4 first-packet race.
	primeTransportPadding(tnet)
	ln, err := tnet.ListenTCP(&net.TCPAddr{IP: net.IPv4(10, 9, 0, 1), Port: 80})
	if err != nil {
		t.Fatal(err)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	upc, err := tnet.ListenUDP(&net.UDPAddr{IP: net.IPv4(10, 9, 0, 1), Port: 53})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := upc.ReadFrom(buf)
			if err != nil {
				return
			}
			var q dns.Msg
			if q.Unpack(buf[:n]) != nil || len(q.Question) == 0 {
				continue
			}
			r := new(dns.Msg)
			r.SetReply(&q)
			if q.Question[0].Qtype == dns.TypeA {
				r.Answer = append(r.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(10, 9, 0, 1)})
			}
			b, _ := r.Pack()
			upc.WriteTo(b, from)
		}
	}()
	return port
}

// End to end: the same path as the app's ping (URLTest → sing-box socks
// outbound → AWG SOCKS sidecar → amneziawg-go → server), for 1.0/1.5/2.0.
func TestAWGURLTestEndToEnd(t *testing.T) {
	for _, v := range awgVersions {
		t.Run(v.name, func(t *testing.T) {
			cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
			port := startAWGServer(t, srv, cli, psk, v.server)
			p, err := ParseWGConf(clientConf(cli, srv, psk, fmt.Sprintf("127.0.0.1:%d", port), v.client), v.name)
			if err != nil {
				t.Fatal(err)
			}
			st, _ := parseSettings(`{"dataDir":"` + t.TempDir() + `"}`)
			for i, link := range []string{"http://10.9.0.1/generate_204", "http://probe.test/generate_204"} {
				if i > 0 {
					// Same keys, new device: wireguard-go rounds the TAI64N
					// handshake timestamp to ~16 ms, and a second initiation
					// inside that window is dropped as a replay (one 5 s
					// REKEY_TIMEOUT retry). Not an app path; keep the test fast.
					time.Sleep(50 * time.Millisecond)
				}
				start := time.Now()
				d, err := urlTest(p, st, link, awgMinTimeout)
				if err != nil || d <= 0 {
					t.Fatalf("%s %s: delay %d err %v (after %v)", v.name, link, d, err, time.Since(start))
				}
				t.Logf("%s %s: %d ms", v.name, link, d)
			}
		})
	}
}

// A server with different obfuscation params must fail with a readable
// handshake error, not hang or report a bogus delay.
func TestAWGURLTestHandshakeError(t *testing.T) {
	cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
	port := startAWGServer(t, srv, cli, psk, awgVersions[0].server)
	p, _ := ParseWGConf(clientConf(cli, srv, psk, fmt.Sprintf("127.0.0.1:%d", port), awgVersions[2].client), "mismatch")
	st, _ := parseSettings(`{"dataDir":"` + t.TempDir() + `"}`)
	d, err := urlTest(p, st, "http://10.9.0.1/generate_204", 6*time.Second)
	if err == nil || d > 0 {
		t.Fatalf("want failure, got %d", d)
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Errorf("error should name the handshake: %v", err)
	}
	t.Logf("error: %v", err)
}

func awgServerLogger() *device.Logger {
	if os.Getenv("AWG_VERBOSE") != "" {
		l := log.New(os.Stderr, "awg-server: ", log.Lmicroseconds)
		return &device.Logger{Verbosef: l.Printf, Errorf: l.Printf}
	}
	return device.NewLogger(device.LogLevelError, "awg-server: ")
}
