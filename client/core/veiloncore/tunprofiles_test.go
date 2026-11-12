package veiloncore

// End-to-end TUN tests for AmneziaWG (the SlipNet / SOCKS5 cases were
// removed with those protocols in 1.0.4), through the
// app's real path (startGlobal → Platform.OpenTun → sing-box tun inbound →
// proxy outbound / sidecar) with the Android-like routing model of
// TestTunAndroidRouting (other apps = uid 65534 helper subprocess).
//
// Gated like TestTunAndroidRouting: VEILON_NETTEST=1 and root.
//
//	sudo VEILON_NETTEST=1 ./veiloncore.test -test.run TestTunAndroidProfiles -test.v
//
// Stand-in servers (all on 127.0.0.1, in this process):
//   - "internet": an HTTP server answering "veilon-ok …" and a DNS server
//     (UDP + TCP) answering every A query with ntPubIP.
//   - amneziawg 1.0/1.5/2.0: amneziawg-go + netstack server peer whose
//     netstack hosts the HTTP server at ntPubIP:80 and DNS at ntDNSIP:53.
// Every server routes TCP :53 to the DNS server and any other TCP
// destination to the HTTP server, so the helper's "DNS then HTTP GET to the
// returned public IP" only succeeds when both really went through the tunnel.

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/miekg/dns"
	box "github.com/sagernet/sing-box"
)

// ntWorld is the stand-in internet reached through the proxy.
type ntWorld struct {
	httpAddr   string // 127.0.0.1:port
	dnsUDPAddr string
	dnsTCPAddr string
	echoAddr   string // UDP echo (any other UDP destination)
}

// ntUDPEcho answers every datagram with "echo:" + payload.
func ntUDPEcho(pc net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		pc.WriteTo(append([]byte("echo:"), buf[:n]...), from)
	}
}

func ntDNSHandler() dns.Handler {
	return dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(q)
		if len(q.Question) > 0 && q.Question[0].Qtype == dns.TypeA {
			rr, _ := dns.NewRR(q.Question[0].Name + " 60 IN A " + ntPubIP)
			m.Answer = append(m.Answer, rr)
		}
		w.WriteMsg(m)
	})
}

func ntHTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "veilon-ok "+r.Host)
	})
}

func ntStartWorld(t *testing.T) *ntWorld {
	t.Helper()
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: ntHTTPHandler()}
	go hs.Serve(hl)
	dl, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	du := &dns.Server{PacketConn: dl, Handler: ntDNSHandler()}
	go du.ActivateAndServe()
	tl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dt := &dns.Server{Listener: tl, Handler: ntDNSHandler()}
	go dt.ActivateAndServe()
	el, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go ntUDPEcho(el)
	t.Cleanup(func() { hs.Close(); du.Shutdown(); dt.Shutdown(); el.Close() })
	return &ntWorld{httpAddr: hl.Addr().String(), dnsUDPAddr: dl.LocalAddr().String(), dnsTCPAddr: tl.Addr().String(), echoAddr: el.LocalAddr().String()}
}

// target maps a requested destination to the stand-in service.
func (w *ntWorld) target(network, dst string) string {
	_, port, _ := net.SplitHostPort(dst)
	if port == "53" {
		if network == "udp" {
			return w.dnsUDPAddr
		}
		return w.dnsTCPAddr
	}
	return w.httpAddr
}

func ntPort(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	var n int
	fmt.Sscan(p, &n)
	return n
}

// ntServerBox runs a sing-box server with the given inbounds that sends
// everything to the stand-in world.
func ntServerBox(t *testing.T, w *ntWorld, inbounds []any) {
	t.Helper()
	cfg := obj{
		"log":       obj{"level": firstNonEmpty(os.Getenv("NT_SRVLOG"), "warn")},
		"inbounds":  inbounds,
		"outbounds": []any{obj{"type": "direct", "tag": "direct"}},
		"route": obj{"rules": []any{
			// Mutually exclusive rules (an earlier matching route-options
			// override is not replaced by a later one).
			obj{"network": "tcp", "port_range": []string{"1:52", "54:65535"}, "action": "route-options", "override_address": "127.0.0.1", "override_port": ntPort(w.httpAddr)},
			obj{"network": "tcp", "port": 53, "action": "route-options", "override_address": "127.0.0.1", "override_port": ntPort(w.dnsTCPAddr)},
			obj{"network": "udp", "port": 53, "action": "route-options", "override_address": "127.0.0.1", "override_port": ntPort(w.dnsUDPAddr)},
			obj{"network": "udp", "port": ntEchoPort, "action": "route-options", "override_address": "127.0.0.1", "override_port": ntPort(w.echoAddr)},
		}, "final": "direct"},
	}
	ctx := boxContext(context.Background())
	opts, err := parseBoxOptions(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
}

// ntAWGServer: amneziawg-go server peer whose netstack hosts HTTP at
// ntPubIP:80 and DNS (UDP + TCP) at ntDNSIP:53.
func ntAWGServer(t *testing.T, srv, cli, psk wgKey, params string) int {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	pub, dnsIP := netip.MustParseAddr(ntPubIP), netip.MustParseAddr(ntDNSIP)
	tdev, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.9.0.1"), pub, dnsIP}, nil, 1420)
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
	primeTransportPadding(tnet)
	hl, err := tnet.ListenTCPAddrPort(netip.AddrPortFrom(pub, 80))
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: ntHTTPHandler()}
	go hs.Serve(hl)
	t.Cleanup(func() { hs.Close() })
	ul, err := tnet.ListenUDPAddrPort(netip.AddrPortFrom(dnsIP, 53))
	if err != nil {
		t.Fatal(err)
	}
	du := &dns.Server{PacketConn: ul, Handler: ntDNSHandler()}
	go du.ActivateAndServe()
	ul2, err := tnet.ListenUDPAddrPort(netip.AddrPortFrom(netip.MustParseAddr("10.9.0.1"), 53))
	if err != nil {
		t.Fatal(err)
	}
	du2 := &dns.Server{PacketConn: ul2, Handler: ntDNSHandler()}
	go du2.ActivateAndServe()
	t.Cleanup(func() { du2.Shutdown() })
	tl, err := tnet.ListenTCPAddrPort(netip.AddrPortFrom(dnsIP, 53))
	if err != nil {
		t.Fatal(err)
	}
	dt := &dns.Server{Listener: tl, Handler: ntDNSHandler()}
	go dt.ActivateAndServe()
	el, err := tnet.ListenUDPAddrPort(netip.AddrPortFrom(pub, ntEchoPort))
	if err != nil {
		t.Fatal(err)
	}
	go ntUDPEcho(el)
	t.Cleanup(func() { du.Shutdown(); dt.Shutdown(); el.Close() })
	return port
}

const ntEchoPort = 7

// ntProfileCase builds a profile (and its stand-in server).
type ntProfileCase struct {
	name  string
	build func(t *testing.T, w *ntWorld) *Profile
}

func ntProfileCases() []ntProfileCase {
	var cases []ntProfileCase
	for _, v := range awgVersions {
		v := v
		cases = append(cases, ntProfileCase{"amneziawg/" + v.name, func(t *testing.T, w *ntWorld) *Profile {
			cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
			port := ntAWGServer(t, srv, cli, psk, v.server)
			p, err := ParseWGConf(clientConf(cli, srv, psk, fmt.Sprintf("127.0.0.1:%d", port), v.client), v.name)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}})
	}
	return cases
}

// ntRunSession connects p like the app does and runs the "other app".
func ntRunSession(t *testing.T, p *Profile, stack string) {
	t.Helper()
	st := DefaultSettings()
	if stack != "" {
		st.Stack = stack
		st.ExperimentalStack = true // sidecar protocols must still fall back to gVisor
	}
	st.RemoteDNS = "udp://" + ntDNSIP
	st.DirectDNS = "127.0.0.1"
	st.LogLevel = "debug"
	st.AppPackage = "app.veilon"
	dir := t.TempDir()
	st.DataDir, st.TempDir = dir, dir
	st.normalizePaths()
	// Ping (URLTest), the same isolated path as the app's ping button.
	if d, err := urlTest(p, st, "http://probe.test/generate_204", 12*time.Second); err != nil || d <= 0 {
		t.Errorf("PING %s: delay %d err %v", t.Name(), d, err)
	} else {
		t.Logf("PING %s: %d ms", t.Name(), d)
	}
	plat := &androidLikePlatform{t: t}
	defer plat.cleanup()
	before := ntLogSeq()
	if err := startGlobal(p, st, plat); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopGlobal()
	t.Logf("core %s, effective tun stack: %s", CoreOf(p), runningStack())
	// UDP beyond DNS only where the transport carries UDP (SSH and the
	// DNS tunnels are TCP-only by design: their DNS goes over TCP).
	udp := "0"
	if ntCarriesUDP(p) {
		udp = "1"
	}
	out, err := ntOtherAppEnv(t, "VEILON_NETCLIENT_UDP="+udp)
	t.Logf("other app (uid %d):\n%s", ntOtherUID, strings.TrimSpace(out))
	if err != nil {
		t.Logf("core log:\n%s", ntLogsSince(before))
		t.Errorf("RESULT %s: other apps have NO internet through the tun (%v)", t.Name(), err)
	} else {
		t.Logf("RESULT %s: ok", t.Name())
	}
}

func ntCarriesUDP(p *Profile) bool {
	return true
}

// ntOtherAppEnv is ntOtherApp with extra environment for the helper.
func ntOtherAppEnv(t *testing.T, env ...string) (string, error) {
	cmd := exec.Command(os.Args[0], "-test.run", "^TestNetClientHelper$", "-test.v")
	cmd.Env = append(append(os.Environ(), "VEILON_NETCLIENT=1"), env...)
	cmd.SysProcAttr = ntHelperAttr()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestTunAndroidProfiles(t *testing.T) {
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	stacks := []string{""}
	if s := os.Getenv("VEILON_NETTEST_STACKS"); s != "" {
		stacks = strings.Split(s, ",")
	}
	only := os.Getenv("VEILON_NETTEST_ONLY")
	for _, c := range ntProfileCases() {
		if only != "" && !strings.Contains(c.name, only) {
			continue
		}
		for _, stack := range stacks {
			name := c.name
			if stack != "" {
				name += "/" + stack
			}
			t.Run(name, func(t *testing.T) {
				w := ntStartWorld(t)
				p := c.build(t, w)
				p.normalize()
				ntRunSession(t, p, stack)
			})
		}
	}
}
