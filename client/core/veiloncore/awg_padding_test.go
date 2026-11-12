package veiloncore

import (
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/miekg/dns"
)

// amneziawg-go race (AWG 2.0, S4 > 0): RoutineReadFromTUN loads S4 before
// its first blocking TUN read, i.e. before IpcSet, so the first packet read
// from the TUN goes out without the S4 prefix and the server drops it as
// "unknown type". The sleep makes the race deterministic (on a phone with
// many cores the reader goroutine usually wins it anyway). Without the
// primer the first datagram is lost; with it, it arrives.
func TestAWGFirstPacketPadding(t *testing.T) {
	v := awgVersions[2] // S4 = 9
	for _, prime := range []bool{false, true} {
		t.Run(fmt.Sprintf("prime=%v", prime), func(t *testing.T) {
			cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
			port := startAWGServer(t, srv, cli, psk, v.server)
			p, err := ParseWGConf(clientConf(cli, srv, psk, fmt.Sprintf("127.0.0.1:%d", port), v.client), v.name)
			if err != nil {
				t.Fatal(err)
			}
			tdev, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.9.0.2")}, nil, 1280)
			if err != nil {
				t.Fatal(err)
			}
			dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
			defer dev.Close()
			time.Sleep(100 * time.Millisecond) // reader goroutine now blocks with S4 = 0
			uapi, _ := awgUAPI(p, fmt.Sprintf("127.0.0.1:%d", port))
			if err := dev.IpcSet(uapi); err != nil {
				t.Fatal(err)
			}
			if err := dev.Up(); err != nil {
				t.Fatal(err)
			}
			if prime {
				primeTransportPadding(tnet)
			}
			c, err := tnet.DialUDP(nil, &net.UDPAddr{IP: net.IPv4(10, 9, 0, 1), Port: 53})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			q := new(dns.Msg)
			q.SetQuestion("probe.test.", dns.TypeA)
			b, _ := q.Pack()
			c.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := c.Write(b); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 1500)
			_, err = c.Read(buf)
			switch {
			case prime && err != nil:
				t.Fatalf("with primer the first query must be answered: %v", err)
			case !prime && err == nil:
				t.Fatalf("negative control: expected the unprimed first packet to be dropped (amneziawg-go fixed upstream?)")
			case !prime:
				t.Logf("reproduced: first packet without S4 prefix dropped (%v)", err)
			default:
				t.Logf("primed: first query answered")
			}
		})
	}
}
