package veiloncore

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

// TestAWGSocksUDPAssociate reproduces the 1.0.2 AmneziaWG crash without a
// TUN: in a connected session every UDP flow (first of all the DNS queries
// of other apps, hijacked and sent to the remote DNS through the proxy)
// reaches the AWG SOCKS sidecar as UDP ASSOCIATE. awgTunnel.ListenPacket
// used netstack.ListenUDP(&net.UDPAddr{}) and gVisor panicked ("invalid
// protocol number = 0") in the sidecar goroutine, which killed the process.
// Path: sing-box socks outbound (the session's proxy outbound) →
// ListenPacket → AWG sidecar → amneziawg-go → server DNS at 10.9.0.1.
func TestAWGSocksUDPAssociate(t *testing.T) {
	type tc struct {
		name string
		v    int
		v6   bool
	}
	cases := []tc{{"awg1.0", 0, false}, {"awg1.5", 1, false}, {"awg2.0", 2, false}, {"awg2.0-dualstack", 2, true}}
	for _, c := range cases {
		v := awgVersions[c.v]
		t.Run(c.name, func(t *testing.T) {
			cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
			port := startAWGServer(t, srv, cli, psk, v.server)
			p, err := ParseWGConf(clientConf(cli, srv, psk, fmt.Sprintf("127.0.0.1:%d", port), v.client), v.name)
			if err != nil {
				t.Fatal(err)
			}
			if c.v6 { // Amnezia configs often carry an IPv6 address too
				p.WG.Address = append(p.WG.Address, "fd00:9::2/128")
			}
			st, _ := parseSettings(`{"dataDir":"` + t.TempDir() + `","logLevel":"error"}`)
			s, err := newSession(p, st, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ob, ok := s.box.Outbound().Outbound(tagProxy)
			if !ok {
				t.Fatal("no proxy outbound")
			}
			for i := 0; i < 2; i++ { // two associations: a fresh socket each time
				if err := awgDNSOverUDP(t, ob); err != nil {
					t.Fatalf("association %d: %v", i, err)
				}
			}
		})
	}
}

func awgDNSOverUDP(t *testing.T, ob adapter.Outbound) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dst := M.ParseSocksaddr("10.9.0.1:53")
	pc, err := ob.ListenPacket(ctx, dst)
	if err != nil {
		return fmt.Errorf("listen packet: %w", err)
	}
	defer pc.Close()
	q := new(dns.Msg)
	q.SetQuestion("probe.test.", dns.TypeA)
	b, _ := q.Pack()
	ua := &net.UDPAddr{IP: net.IPv4(10, 9, 0, 1), Port: 53}
	buf := make([]byte, 2048)
	// The first datagram of a fresh handshake can be lost; retry.
	for try := 0; try < 4; try++ {
		if _, err := pc.WriteTo(b, ua); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		var r dns.Msg
		if err := r.Unpack(buf[:n]); err != nil || len(r.Answer) == 0 {
			return fmt.Errorf("bad answer from %v: %v", from, err)
		}
		t.Logf("DNS over AWG UDP ok from %v: %s", from, r.Answer[0].String())
		return nil
	}
	return fmt.Errorf("no DNS answer over UDP")
}

// panicBackend panics in every call (stands in for any bug in a sidecar).
type panicBackend struct{}

func (panicBackend) DialTCP(context.Context, string) (net.Conn, error) { panic("boom tcp") }
func (panicBackend) ListenPacket(context.Context) (net.PacketConn, error) {
	panic("boom udp")
}
func (panicBackend) ResolveUDP(context.Context, string, int) (net.Addr, error) { panic("boom resolve") }

// TestRecoveredPanicIsLoggedAndSaved: a panic in a goroutine the core owns
// must not kill the process; it is logged, written to the crash file and
// ends the session (onRecoveredPanic).
func TestRecoveredPanicIsLoggedAndSaved(t *testing.T) {
	dir := t.TempDir()
	InitCrashLog(dir)
	var stopped atomic.Int32
	old := onRecoveredPanic
	onRecoveredPanic = func(string, any) { stopped.Add(1) }
	defer func() { onRecoveredPanic = old }()
	before := ntLogSeq()
	s, err := startSocksServer(panicBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte{5, 1, 2})
	c.Write(append(append([]byte{1, byte(len(s.user))}, s.user...), append([]byte{byte(len(s.pass))}, s.pass...)...))
	c.Write([]byte{5, 1, 0, 1, 1, 2, 3, 4, 0, 80})
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	for {
		if _, err := c.Read(buf); err != nil {
			break // the handler died and closed the connection
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for stopped.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if stopped.Load() == 0 {
		t.Fatal("session was not told to stop")
	}
	if l := ntLogsSince(before); !strings.Contains(l, "recovered panic in socks sidecar: boom tcp") {
		t.Fatalf("log line missing:\n%s", l)
	}
	crash := LastCrash()
	if !strings.Contains(crash, "panic in socks sidecar: boom tcp") || !strings.Contains(crash, "socks5.go") {
		t.Fatalf("crash file: %q", crash)
	}
	if _, err := os.Stat(filepath.Join(dir, goCrashFileName)); err != nil {
		t.Fatalf("fatal crash output not set up: %v", err)
	}
	// Kotlin side: uncaught exceptions are recorded in the same file.
	RecordCrash("kotlin", "java.lang.IllegalStateException: test\n\tat app.veilon.X")
	if !strings.Contains(LastCrash(), "kotlin") {
		t.Fatal("RecordCrash not persisted")
	}
	ClearLastCrash()
	if LastCrash() != "" {
		t.Fatal("ClearLastCrash")
	}
}

// TestPreviousFatalCrashIsReported: a report the Go runtime wrote to
// go-crash.txt before the process died shows up after the next start.
func TestPreviousFatalCrashIsReported(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, goCrashFileName), []byte("panic: invalid protocol number = 0\n\ngoroutine 567 [running]:\n"), 0o600)
	before := ntLogSeq()
	InitCrashLog(dir)
	if !strings.Contains(LastCrash(), "invalid protocol number") {
		t.Fatalf("crash file: %q", LastCrash())
	}
	if !strings.Contains(ntLogsSince(before), "previous run crashed (Go)") {
		t.Fatal("not on the log page")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, goCrashFileName)); len(b) != 0 {
		t.Fatalf("go-crash.txt not reset: %q", b)
	}
}
