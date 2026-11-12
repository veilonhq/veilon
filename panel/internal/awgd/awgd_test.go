package awgd

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun/tuntest"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// clientUAPI converts a client .conf (as served to users) into a UAPI set
// operation, the way amneziawg-tools would.
func clientUAPI(t *testing.T, conf string) string {
	t.Helper()
	var b strings.Builder
	section := ""
	keys := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(conf))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		keys[section+strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	h := func(s string) string {
		x, err := b64hex(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	fmt.Fprintf(&b, "private_key=%s\n", h(keys["[Interface]PrivateKey"]))
	for _, k := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "H1", "H2", "H3", "H4"} {
		fmt.Fprintf(&b, "%s=%s\n", strings.ToLower(k), keys["[Interface]"+k])
	}
	fmt.Fprintf(&b, "public_key=%s\npreshared_key=%s\nendpoint=%s\nallowed_ip=0.0.0.0/0\npersistent_keepalive_interval=1\n",
		h(keys["[Peer]PublicKey"]), h(keys["[Peer]PresharedKey"]), keys["[Peer]Endpoint"])
	return b.String()
}

func freeUDPPort(t *testing.T) int {
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// amneziawg-go keeps the obfuscation headers (H1-H4) in package-level
// variables, so two devices with different headers cannot share a process
// (the sidecar runs one process per inbound for that reason). The test
// server therefore runs in a child process (this test binary re-executed),
// and the clients run one after the other in the parent.
const srvEnv = "VEILON_AWG_TEST_SERVER"

// TestAWGServerHelper is the child process: it serves the UAPI config in
// $VEILON_AWG_TEST_SERVER and prints every IP packet that leaves the tunnel.
func TestAWGServerHelper(t *testing.T) {
	uapi := os.Getenv(srvEnv)
	if uapi == "" {
		t.Skip("helper process only")
	}
	stun := tuntest.NewChannelTUN()
	dev := device.NewDevice(stun.TUN(), conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "srv "))
	defer dev.Close()
	if err := dev.IpcSet(uapi); err != nil {
		fmt.Println("ERR", err)
		return
	}
	if err := dev.Up(); err != nil {
		fmt.Println("ERR", err)
		return
	}
	fmt.Println("READY")
	deadline := time.After(30 * time.Second)
	for {
		select {
		case got := <-stun.Inbound:
			fmt.Println("GOT", hex.EncodeToString(got))
		case <-deadline:
			return
		}
	}
}

// TestAmneziaHandshake runs a server configured exactly like the sidecar and
// a client configured from the generated .conf over loopback UDP with
// in-memory TUN devices, and checks that an IP packet crosses the tunnel and
// that a client with wrong obfuscation headers does not get through.
func TestAmneziaHandshake(t *testing.T) {
	spriv, spub, _ := inbounds.GenerateWGKeys()
	cpriv, cpub, _ := inbounds.GenerateWGKeys()
	psk := inbounds.GeneratePSK()
	a := inbounds.DefaultAWG()
	port := freeUDPPort(t)
	srv := Iface{Name: "t0", ListenPort: port, PrivateKey: spriv, Address: "10.99.0.1/24", MTU: 1420,
		Params: Params{Jc: a.Jc, Jmin: a.Jmin, Jmax: a.Jmax, S1: a.S1, S2: a.S2, H1: a.H1, H2: a.H2, H3: a.H3, H4: a.H4},
		Peers:  []Peer{{PublicKey: cpub, PresharedKey: psk, AllowedIP: "10.99.0.2/32"}}}
	su, err := UAPI(srv, false)
	if err != nil {
		t.Fatal(err)
	}
	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.99.0.2/32
Jc = %d
Jmin = %d
Jmax = %d
S1 = %d
S2 = %d
H1 = %d
H2 = %d
H3 = %d
H4 = %d

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 127.0.0.1:%d
`, cpriv, a.Jc, a.Jmin, a.Jmax, a.S1, a.S2, a.H1, a.H2, a.H3, a.H4, spub, psk, port)

	// Server: child process.
	cmd := exec.Command(os.Args[0], "-test.run=^TestAWGServerHelper$", "-test.v")
	cmd.Env = append(os.Environ(), srvEnv+"="+su)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	got := make(chan string, 16)
	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			l := sc.Text()
			switch {
			case l == "READY":
				close(ready)
			case strings.HasPrefix(l, "GOT "):
				got <- strings.TrimPrefix(l, "GOT ")
			case strings.HasPrefix(l, "ERR "):
				t.Error("server:", l)
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("test server did not start")
	}

	ping := tuntest.Ping(netip.MustParseAddr("10.99.0.1"), netip.MustParseAddr("10.99.0.2"))
	want := hex.EncodeToString(ping)

	// A client with wrong obfuscation headers must not get through (run
	// first, so a late packet of the good client cannot be mistaken for it).
	bad := strings.Replace(conf, fmt.Sprintf("H1 = %d", a.H1), fmt.Sprintf("H1 = %d", a.H1+1), 1)
	btun := tuntest.NewChannelTUN()
	bdev := device.NewDevice(btun.TUN(), conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	if err := bdev.IpcSet(clientUAPI(t, bad)); err != nil {
		t.Fatal(err)
	}
	if err := bdev.Up(); err != nil {
		t.Fatal(err)
	}
	btun.Outbound <- ping
	select {
	case <-got:
		t.Fatal("packet with mismatched H1 accepted")
	case <-time.After(1500 * time.Millisecond):
	}
	bdev.Close()

	// The good client.
	ctun := tuntest.NewChannelTUN()
	cdev := device.NewDevice(ctun.TUN(), conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "cli "))
	defer cdev.Close()
	if err := cdev.IpcSet(clientUAPI(t, conf)); err != nil {
		t.Fatal("client uapi:", err)
	}
	if err := cdev.Up(); err != nil {
		t.Fatal(err)
	}
	ctun.Outbound <- ping
	select {
	case g := <-got:
		if g != want {
			t.Fatal("packet corrupted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no packet through the AmneziaWG tunnel")
	}
	// Per-peer counters for the stats reporter (client side: the peer is
	// the server).
	g, err := cdev.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	st := ParseStats(g)
	if v, ok := st[spub]; !ok || v[0] == 0 || v[1] == 0 {
		t.Fatalf("stats %v", st)
	}
}

func TestUAPIPeersOnly(t *testing.T) {
	_, pub, _ := inbounds.GenerateWGKeys()
	u, err := UAPI(Iface{Peers: []Peer{{PublicKey: pub, AllowedIP: "10.0.0.2/32"}}}, true)
	if err != nil || strings.Contains(u, "private_key") || !strings.HasPrefix(u, "replace_peers=true\n") {
		t.Fatalf("%q %v", u, err)
	}
}

func TestParseEndpoints(t *testing.T) {
	get := "private_key=00\nlisten_port=51820\npublic_key=" + strings.Repeat("ab", 32) + "\nendpoint=203.0.113.7:51000\nrx_bytes=5\n" +
		"public_key=" + strings.Repeat("cd", 32) + "\nrx_bytes=0\n"
	eps := ParseEndpoints(get)
	if len(eps) != 1 {
		t.Fatalf("endpoints %v (a peer without handshake has none)", eps)
	}
	for _, v := range eps {
		if v != "203.0.113.7:51000" {
			t.Fatal(v)
		}
	}
}
