package veiloncore

// End-to-end TUN test that models Android's VpnService routing on Linux.
//
// Gated: VEILON_NETTEST=1 and root (needs /dev/net/tun, ip(8), policy rules).
// Build with `go test -c`, run the binary with sudo:
//
//	sudo VEILON_NETTEST=1 ./veiloncore.test -test.run TestTunAndroidRouting -test.v
//
// Model of Android (per-UID policy routing):
//   - the "VPN app" is this test process (uid 0). Like Veilon on a phone it is
//     excluded from the VPN: no rule sends uid 0 to the tun table.
//   - "other apps" are a helper subprocess running as uid 65534 (nobody). An
//     ip rule (uidrange 65534) sends all its traffic to table 100, which holds
//     the VpnService routes (from TunRequest) and the tun's connected subnet,
//     exactly as Android puts them into the VPN network's own table.
//   - the tun's connected route is removed from the main table (Android never
//     puts it there).
//
// Path under test: Platform.OpenTun -> boxPlatform.OpenInterface (dup, name) ->
// sing-box tun inbound (stack from Settings) -> VLESS+WS+TLS outbound (sing-box
// or Xray) -> local VLESS server (sing-box) -> local DNS / HTTP servers.
// The helper does what a browser does: a DNS query to the tun DNS 172.19.0.2,
// then an HTTP GET to the returned public IP.

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/miekg/dns"
	box "github.com/sagernet/sing-box"
	"golang.org/x/sys/unix"
)

const (
	ntTunName  = "vtun0"
	ntTable    = "100"
	ntOtherUID = 65534
	ntPubIP    = "198.51.100.10" // what the fake DNS returns (global unicast)
	ntDNSIP    = "198.51.100.53" // remote DNS (reached through the proxy)
	ntUUID     = "b831381d-6324-4d53-ad4f-8cda48b30811"
)

func ntRun(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		t.Logf("cmd %v: %v: %s", args, err, out)
	}
}

// androidLikePlatform implements Platform like VeilonVpnService, on Linux.
type androidLikePlatform struct {
	t       *testing.T
	mu      sync.Mutex
	files   []*os.File
	req     TunRequest
	protect int
	selfIn  bool // the app itself is routed into the VPN
	// noProtect: Protect is a no-op (negative control).
	noProtect bool
}

func (p *androidLikePlatform) OpenTun(optionsJSON string) (int32, error) {
	if err := json.Unmarshal([]byte(optionsJSON), &p.req); err != nil {
		return -1, err
	}
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return -1, err
	}
	var ifr [unix.IFNAMSIZ + 64]byte
	copy(ifr[:], ntTunName)
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = unix.IFF_TUN | unix.IFF_NO_PI
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.TUNSETIFF, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		f.Close()
		return -1, errno
	}
	p.mu.Lock()
	p.files = append(p.files, f)
	p.mu.Unlock()
	t := p.t
	ntRun(t, "ip", "link", "set", ntTunName, "mtu", fmt.Sprint(p.req.MTU), "up")
	for _, a := range p.req.Inet4Address {
		ntRun(t, "ip", "addr", "add", a, "dev", ntTunName)
		_, n, _ := net.ParseCIDR(a)
		ntRun(t, "ip", "route", "del", n.String(), "dev", ntTunName, "table", "main")
		ntRun(t, "ip", "route", "add", n.String(), "dev", ntTunName, "table", ntTable)
	}
	for _, r := range p.req.Inet4Routes {
		ntRun(t, "ip", "route", "add", r, "dev", ntTunName, "table", ntTable)
	}
	ntRun(t, "ip", "rule", "add", "uidrange", fmt.Sprintf("%d-%d", ntOtherUID, ntOtherUID), "lookup", ntTable, "priority", "100")
	// The VPN app itself (uid 0 here) is in the VPN unless it excluded
	// its own package, like Android's per-UID ranges. Protected sockets
	// (VpnService.protect) carry a mark that bypasses the VPN table.
	p.selfIn = true
	for _, x := range p.req.ExcludePackages {
		if x == "app.veilon" {
			p.selfIn = false
		}
	}
	if p.selfIn {
		// Only for the test's own destinations: every root process of the
		// host shares uid 0 (the workspace file system must not be routed
		// into the tun).
		// The tun subnet is in it as on Android: an included app's packets
		// to 172.19.0.x (e.g. the system stack's TCP replies) use the VPN table.
		for _, dst := range []string{"203.0.113.0/24", "198.51.100.0/24", "1.1.1.1/32", "172.19.0.0/30"} {
			ntRun(t, "ip", "rule", "add", "to", dst, "uidrange", "0-0", "lookup", ntTable, "priority", "101")
		}
	}
	ntRun(t, "ip", "rule", "add", "fwmark", ntProtectMark, "lookup", "main", "priority", "50")
	// loose reverse-path filter, so only forward routing decides
	for _, k := range []string{"all", "default", ntTunName} {
		os.WriteFile("/proc/sys/net/ipv4/conf/"+k+"/rp_filter", []byte("0"), 0)
	}
	t.Logf("OpenTun: mtu=%d addr=%v routes=%d dns=%s exclude=%v include=%v",
		p.req.MTU, p.req.Inet4Address, len(p.req.Inet4Routes), p.req.DNSServer, p.req.ExcludePackages, p.req.IncludePackages)
	return int32(f.Fd()), nil
}

func (p *androidLikePlatform) Protect(fd int32) bool {
	p.mu.Lock()
	p.protect++
	p.mu.Unlock()
	if p.noProtect {
		return true // negative control: protect() does nothing
	}
	return unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, 0x5650) == nil
}

const ntProtectMark = "0x5650"

func (p *androidLikePlatform) protected() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.protect
}

func (p *androidLikePlatform) OnLog(level int32, message string) {}

func (p *androidLikePlatform) cleanup() {
	ntRun(p.t, "ip", "rule", "del", "priority", "100")
	if p.selfIn {
		for i := 0; i < 4; i++ {
			ntRun(p.t, "ip", "rule", "del", "priority", "101")
		}
	}
	ntRun(p.t, "ip", "rule", "del", "priority", "50")
	ntRun(p.t, "ip", "route", "flush", "table", ntTable)
	p.mu.Lock()
	for _, f := range p.files {
		f.Close()
	}
	p.files = nil
	p.mu.Unlock()
	ntRun(p.t, "ip", "link", "del", ntTunName)
}

func ntSelfSigned(t *testing.T) (certPEM, keyPEM string) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "srv.test"},
		DNSNames: []string{"srv.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// ntServers starts the HTTP + DNS "internet" and the VLESS+WS+TLS server.
func ntServers(t *testing.T) (vlessPort int, stop func()) { return ntServersOn(t, "127.0.0.1") }

func ntServersOn(t *testing.T, vlessListen string) (vlessPort int, stop func()) {
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "veilon-ok "+r.Host)
	})}
	go hs.Serve(hl)
	dl, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ds := &dns.Server{PacketConn: dl, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(q)
		if len(q.Question) > 0 && q.Question[0].Qtype == dns.TypeA {
			rr, _ := dns.NewRR(q.Question[0].Name + " 60 IN A " + ntPubIP)
			m.Answer = append(m.Answer, rr)
		}
		w.WriteMsg(m)
	})}
	go ds.ActivateAndServe()
	vp, _ := freePort()
	cert, key := ntSelfSigned(t)
	cfg := obj{
		"log": obj{"level": "warn"},
		"inbounds": []any{obj{"type": "vless", "tag": "vin", "listen": vlessListen, "listen_port": vp,
			"users":     []any{obj{"uuid": ntUUID}},
			"transport": obj{"type": "ws", "path": "/ws"},
			"tls": obj{"enabled": true, "server_name": "srv.test",
				"certificate": strings.Split(strings.TrimSpace(cert), "\n"), "key": strings.Split(strings.TrimSpace(key), "\n")}}},
		"outbounds": []any{obj{"type": "direct", "tag": "direct"}},
		"route": obj{"rules": []any{
			obj{"network": "udp", "port": 53, "action": "route-options", "override_address": "127.0.0.1",
				"override_port": dl.LocalAddr().(*net.UDPAddr).Port},
			obj{"network": "tcp", "action": "route-options", "override_address": "127.0.0.1",
				"override_port": hl.Addr().(*net.TCPAddr).Port},
		}, "final": "direct"},
	}
	ctx := boxContext(t.Context())
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
	return vp, func() { srv.Close(); hs.Close(); ds.Shutdown() }
}

// ntOtherApp runs the helper as uid 65534 ("another app on the phone").
func ntOtherApp(t *testing.T) (string, error) {
	cmd := exec.Command(os.Args[0], "-test.run", "^TestNetClientHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "VEILON_NETCLIENT=1")
	cmd.SysProcAttr = ntHelperAttr()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestTunAndroidRouting(t *testing.T) {
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	// The server sits behind a veth in its own network namespace at a
	// public-looking address (TEST-NET-3), so a core socket that is not
	// protected while the app is inside the VPN really loops into the tun
	// (loopback servers would always be reached through the local table).
	vp := ntNetnsServer(t)
	link := fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=ws&security=tls&path=%%2Fws&host=srv.test&sni=srv.test&allowInsecure=1#nettest", ntUUID, ntSrvIP, vp)
	for _, core := range []string{"sing-box", "xray"} {
		// 1.0.4: gVisor is the only tun stack (system / mixed were removed).
		for _, stack := range []string{"gvisor"} {
			t.Run(core+"/"+stack, func(t *testing.T) {
				p, err := ParseLink(link)
				if err != nil {
					t.Fatal(err)
				}
				st := DefaultSettings()
				st.Core = core
				st.Stack = stack
				// The opt-in the app would set to expose the stack choice
				// (see tunStack); without it every stack is gVisor.
				st.ExperimentalStack = true
				st.RemoteDNS = "udp://" + ntDNSIP
				st.DirectDNS = "127.0.0.1"
				st.LogLevel = "debug"
				st.AppPackage = "app.veilon"
				dir := t.TempDir()
				st.DataDir, st.TempDir = dir, dir
				st.normalizePaths()
				plat := &androidLikePlatform{t: t}
				defer plat.cleanup()
				before := ntLogSeq()
				if err := startGlobal(p, st, plat); err != nil {
					t.Fatalf("start: %v", err)
				}
				defer stopGlobal()
				s := runningStack()
				t.Logf("effective tun stack: %s (setting %s), app inside VPN: %v", s, stack, plat.selfIn)
				if s != stack {
					t.Errorf("effective stack %q, want %q", s, stack)
				}
				if want := stack != "gvisor"; plat.selfIn != want {
					t.Errorf("app inside the VPN = %v, want %v", plat.selfIn, want)
				}
				out, err := ntOtherApp(t)
				if strings.Contains(ntLogsSince(before), "missing default interface") {
					t.Errorf("sing-box reported a missing default interface (network paused)")
				}
				t.Logf("other app (uid %d):\n%s", ntOtherUID, strings.TrimSpace(out))
				if err != nil {
					t.Logf("core log:\n%s", ntLogsSince(before))
					t.Errorf("RESULT %s/%s: other apps have NO internet through the tun (%v)", core, stack, err)
				} else {
					t.Logf("RESULT %s/%s: ok (protected sockets: %d)", core, stack, plat.protected())
				}
				if plat.selfIn && plat.protected() == 0 {
					t.Errorf("app inside the VPN but no socket was protected")
				}
			})
		}
	}
}

func ntHelperAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: ntOtherUID, Gid: ntOtherUID}}
}

const (
	ntNetns  = "vsrv"
	ntHostIP = "203.0.113.1"
	ntSrvIP  = "203.0.113.2"
)

// ntNetnsServer runs TestNetServerHelper in a network namespace reached
// over a veth pair (main table only) and returns its VLESS port.
func ntNetnsServer(t *testing.T) int {
	t.Helper()
	ntRun(t, "ip", "netns", "del", ntNetns)
	ntRun(t, "ip", "link", "del", "vh0")
	for _, c := range [][]string{
		{"ip", "netns", "add", ntNetns},
		{"ip", "link", "add", "vh0", "type", "veth", "peer", "name", "vs0"},
		{"ip", "link", "set", "vs0", "netns", ntNetns},
		{"ip", "addr", "add", ntHostIP + "/24", "dev", "vh0"},
		{"ip", "link", "set", "vh0", "up"},
		{"ip", "netns", "exec", ntNetns, "ip", "addr", "add", ntSrvIP + "/24", "dev", "vs0"},
		{"ip", "netns", "exec", ntNetns, "ip", "link", "set", "vs0", "up"},
		{"ip", "netns", "exec", ntNetns, "ip", "link", "set", "lo", "up"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", c, err, out)
		}
	}
	cmd := exec.Command("ip", "netns", "exec", ntNetns, os.Args[0], "-test.run", "^TestNetServerHelper$", "-test.v", "-test.timeout", "30m")
	cmd.Env = append(os.Environ(), "VEILON_NETSERVER=1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
		ntRun(t, "ip", "netns", "del", ntNetns)
		ntRun(t, "ip", "link", "del", "vh0")
	})
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		var port int
		if _, err := fmt.Sscanf(sc.Text(), "VLESS_PORT=%d", &port); err == nil {
			go io.Copy(io.Discard, stdout)
			return port
		}
	}
	t.Fatal("server helper did not report its port")
	return 0
}

// TestNetServerHelper is the remote VLESS server + "internet" (run by
// ntNetnsServer inside the server network namespace).
func TestNetServerHelper(t *testing.T) {
	if os.Getenv("VEILON_NETSERVER") != "1" {
		t.Skip("helper")
	}
	vp, stop := ntServersOn(t, "0.0.0.0")
	defer stop()
	fmt.Printf("VLESS_PORT=%d\n", vp)
	io.Copy(io.Discard, os.Stdin)
}

// TestNetClientHelper is the "other app" (run by TestTunAndroidRouting).
func TestNetClientHelper(t *testing.T) {
	if os.Getenv("VEILON_NETCLIENT") != "1" {
		t.Skip("helper")
	}
	c := &dns.Client{Net: "udp", Timeout: 4 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("ipcheck.example.test.", dns.TypeA)
	var ip string
	for i := 0; i < 2 && ip == ""; i++ {
		r, _, err := c.Exchange(m, "172.19.0.2:53")
		if err != nil {
			t.Logf("dns try %d: %v", i, err)
			continue
		}
		for _, a := range r.Answer {
			if x, ok := a.(*dns.A); ok {
				ip = x.A.String()
			}
		}
	}
	if ip == "" {
		t.Fatal("DNS through tun failed")
	}
	t.Logf("DNS ok: %s", ip)
	hc := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := hc.Get("http://" + ip + "/")
	if err != nil {
		t.Fatalf("HTTP through tun failed: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(string(b), "veilon-ok") {
		t.Fatalf("HTTP body %q", b)
	}
	t.Logf("HTTP ok: %s", b)
	if os.Getenv("VEILON_NETCLIENT_UDP") == "1" {
		// Plain UDP (not DNS) to the public IP: answered by a UDP echo
		// server only reachable through the proxy.
		uc, err := net.Dial("udp", ip+":7")
		if err != nil {
			t.Fatalf("UDP dial: %v", err)
		}
		defer uc.Close()
		buf := make([]byte, 256)
		var got string
		for i := 0; i < 3 && got == ""; i++ {
			uc.Write([]byte("ping-udp"))
			uc.SetReadDeadline(time.Now().Add(2 * time.Second))
			if n, err := uc.Read(buf); err == nil {
				got = string(buf[:n])
			}
		}
		if got != "echo:ping-udp" {
			t.Fatalf("UDP through tun failed (got %q)", got)
		}
		t.Logf("UDP ok: %s", got)
	}
}

func ntLogSeq() int64 {
	logMu.Lock()
	defer logMu.Unlock()
	return logSeq
}

func ntLogsSince(seq int64) string {
	logMu.Lock()
	defer logMu.Unlock()
	var b strings.Builder
	for _, l := range logRing {
		if l.Seq > seq && l.Level != "debug" {
			fmt.Fprintf(&b, "  [%s] %s\n", l.Level, l.Msg)
		}
	}
	return b.String()
}

// runningStack is the tun stack of the running sing-box config.
func runningStack() string {
	runMu.Lock()
	defer runMu.Unlock()
	if running == nil {
		return ""
	}
	ins, _ := running.config["inbounds"].([]any)
	for _, i := range ins {
		if o, ok := i.(obj); ok && o["type"] == "tun" {
			s, _ := o["stack"].(string)
			return s
		}
	}
	return ""
}

// TestTunStackAlwaysGvisor: whatever the stored "stack" setting, the VPN
// tun uses gVisor (see tunStack); no root needed.
func TestTunStackAlwaysGvisor(t *testing.T) {
	for _, stack := range []string{"system", "mixed", "gvisor", ""} {
		st := DefaultSettings()
		st.Stack = stack
		cfg := buildSingboxConfig(&buildPlan{outbounds: []any{obj{"type": "direct", "tag": tagProxy}}}, st, true)
		if got, _ := tunInbound(cfg)["stack"].(string); got != "gvisor" {
			t.Errorf("setting %q -> tun stack %q, want gvisor", stack, got)
		}
	}
}

// TestDefaultInterfaceNeverNil: sing-box must always see a default
// interface (nil pauses the network), before and after the app reports one.
func TestDefaultInterfaceNeverNil(t *testing.T) {
	m := &staticMonitor{}
	SetDefaultInterface("", 0)
	if m.DefaultInterface() == nil {
		t.Fatal("nil before the app reported a network")
	}
	SetDefaultInterface("wlan0", 30)
	if i := m.DefaultInterface(); i == nil || i.Name != "wlan0" || i.Index != 30 {
		t.Fatalf("got %+v", i)
	}
	SetDefaultInterface("", 0)
}

// TestExperimentalStackIgnored: since 1.0.4 the opt-in is ignored too:
// always gVisor, app excluded from the VPN, no auto_detect_interface.
func TestExperimentalStackIgnored(t *testing.T) {
	for _, stack := range []string{"system", "mixed", "gvisor", ""} {
		for _, own := range []bool{false, true} {
			st := DefaultSettings()
			st.Stack = stack
			st.ExperimentalStack = true
			plan := &buildPlan{outbounds: []any{obj{"type": "direct", "tag": tagProxy}}, ownSockets: own}
			cfg := buildSingboxConfig(plan, st, true)
			got, _ := tunInbound(cfg)["stack"].(string)
			ad, _ := cfg["route"].(obj)["auto_detect_interface"].(bool)
			if got != "gvisor" || ad || selfIncluded(cfg) {
				t.Errorf("stack %q ownSockets %v: got stack %q auto_detect %v", stack, own, got, ad)
			}
		}
	}
}

// TestTunAndroidRoutingControls are the negative controls for the
// system/mixed fix: each must break other apps' internet, proving that
// both halves of the fix are needed: (1) keeping the app inside the VPN
// (1.0.1 excluded it: system-stack TCP replies left outside the tun), and
// (2) protecting the cores' sockets (else they loop into the tun).
func TestTunAndroidRoutingControls(t *testing.T) {
	t.Skip("1.0.4: the system / mixed tun stacks were removed (gVisor only); these controls no longer apply")
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	vp := ntNetnsServer(t)
	link := fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=ws&security=tls&path=%%2Fws&host=srv.test&sni=srv.test&allowInsecure=1#nettest", ntUUID, ntSrvIP, vp)
	for _, control := range []string{"app-excluded", "no-protect"} {
		for _, core := range []string{"sing-box", "xray"} {
			t.Run(control+"/"+core+"/system", func(t *testing.T) {
				p, _ := ParseLink(link)
				st := DefaultSettings()
				st.Core, st.Stack, st.ExperimentalStack = core, "system", true
				st.RemoteDNS, st.DirectDNS, st.AppPackage = "udp://"+ntDNSIP, "127.0.0.1", "app.veilon"
				dir := t.TempDir()
				st.DataDir, st.TempDir = dir, dir
				st.normalizePaths()
				plat := &androidLikePlatform{t: t, noProtect: control == "no-protect"}
				defer plat.cleanup()
				testExcludeSelf = control == "app-excluded"
				defer func() { testExcludeSelf = false }()
				if err := startGlobal(p, st, plat); err != nil {
					t.Fatalf("start: %v", err)
				}
				defer stopGlobal()
				out, err := ntOtherApp(t)
				if err == nil {
					t.Errorf("CONTROL %s: other apps still have internet; the control does not exercise the fix", t.Name())
				} else {
					last := strings.TrimSpace(out)
					if i := strings.LastIndex(last, "tunroute_test.go"); i >= 0 {
						last = last[i:]
					}
					t.Logf("CONTROL %s: broken as expected: %s", t.Name(), strings.SplitN(last, "\n", 2)[0])
				}
			})
		}
	}
}
