package veiloncore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	stdlog "log"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
)

// awgTunnel runs an AmneziaWG (or plain WireGuard) peer in userspace
// (amneziawg-go + gVisor netstack) and serves it as a local SOCKS5 proxy.
type awgTunnel struct {
	dev   *device.Device
	tnet  *netstack.Net
	socks *socksServer
	// endpoint is the resolved peer address (for error messages).
	endpoint string
	// local4 / local6: the interface addresses (UDP sockets bind to them;
	// invalid when the config has none of that family).
	local4, local6 netip.Addr
}

// lastHandshake returns the peer's last handshake time (zero if none).
func (t *awgTunnel) lastHandshake() time.Time {
	out, err := t.dev.IpcGet()
	if err != nil {
		return time.Time{}
	}
	var sec, nsec int64
	for _, l := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(l, "=")
		switch k {
		case "last_handshake_time_sec":
			sec, _ = strconv.ParseInt(v, 10, 64)
		case "last_handshake_time_nsec":
			nsec, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	if sec == 0 && nsec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, nsec)
}

// waitHandshake blocks until the first handshake completed or ctx ends.
func (t *awgTunnel) waitHandshake(ctx context.Context) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if !t.lastHandshake().IsZero() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("amneziawg: no handshake with %s (server unreachable, or keys / Jc, S1-S4, H1-H4 do not match the server)", t.endpoint)
		case <-tick.C:
		}
	}
}

func keyHex(b64 string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("invalid key")
	}
	return hex.EncodeToString(b), nil
}

// awgUAPI renders the device configuration in the UAPI format.
func awgUAPI(p *Profile, endpoint string) (string, error) {
	w := p.WG
	priv, err := keyHex(w.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("private key: %w", err)
	}
	pub, err := keyHex(w.PeerPublicKey)
	if err != nil {
		return "", fmt.Errorf("peer public key: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", priv)
	if a := w.AWG; a != nil {
		num := func(k string, v int) {
			if v != 0 {
				fmt.Fprintf(&b, "%s=%d\n", k, v)
			}
		}
		str := func(k, v string) {
			if v = strings.TrimSpace(v); v != "" {
				fmt.Fprintf(&b, "%s=%s\n", k, v)
			}
		}
		// AWG 2.0 header ranges: "100 - 200" → "100-200" (amneziawg-go
		// parses the halves with ParseUint, spaces are an error).
		rng := func(k, v string) { str(k, strings.Join(strings.Fields(v), "")) }
		num("jc", a.Jc)
		num("jmin", a.Jmin)
		num("jmax", a.Jmax)
		num("s1", a.S1)
		num("s2", a.S2)
		num("s3", a.S3)
		num("s4", a.S4)
		rng("h1", a.H1)
		rng("h2", a.H2)
		rng("h3", a.H3)
		rng("h4", a.H4)
		str("i1", a.I1)
		str("i2", a.I2)
		str("i3", a.I3)
		str("i4", a.I4)
		str("i5", a.I5)
		// AWG 3.0 / 3.1 (amneziawg-go v3 UAPI keys).
		if a.HeaderProtectionKey != "" {
			k, err := keyHex(a.HeaderProtectionKey)
			if err != nil {
				return "", fmt.Errorf("header protection key: %w", err)
			}
			fmt.Fprintf(&b, "header_protection_key=%s\n", k)
		}
		rng("content_padding_addition", a.ContentPaddingAddition)
		rng("rekey_after_time", a.RekeyAfterTime)
		rng("rekey_timeout", a.RekeyTimeout)
		rng("reject_after_time", a.RejectAfterTime)
		rng("keepalive_timeout", a.KeepaliveTimeout)
		rng("max_handshake_attempts", a.MaxHandshakeAttempts)
		if a.RandomTrailers {
			b.WriteString("random_trailers=true\n")
		}
		if a.DisableCookies {
			b.WriteString("disable_cookies=true\n")
		}
	}
	b.WriteString("replace_peers=true\n")
	fmt.Fprintf(&b, "public_key=%s\n", pub)
	if w.PresharedKey != "" {
		psk, err := keyHex(w.PresharedKey)
		if err != nil {
			return "", fmt.Errorf("preshared key: %w", err)
		}
		fmt.Fprintf(&b, "preshared_key=%s\n", psk)
	}
	fmt.Fprintf(&b, "endpoint=%s\n", endpoint)
	ka := w.Keepalive
	if ka <= 0 {
		ka = 25
	}
	if w.AWG != nil && w.AWG.PersistentKeepalive != "" {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%s\n", w.AWG.PersistentKeepalive)
	} else {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", ka)
	}
	b.WriteString("replace_allowed_ips=true\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n")
	return b.String(), nil
}

// awgMinTimeout is the shortest URL-test budget for AmneziaWG/WireGuard:
// the first handshake (junk + I-packets, one retry after REKEY_TIMEOUT 5 s)
// must fit before the HTTP request.
const awgMinTimeout = 12 * time.Second

// fallbackResolvers are used when the system resolver fails (Go's pure
// resolver has no /etc/resolv.conf on Android).
var fallbackResolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

func lookupDirect(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := directResolver().LookupNetIP(ctx, "ip", host)
	if err == nil && len(ips) > 0 {
		return ips, nil
	}
	for _, srv := range fallbackResolvers {
		srv := srv
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return protectedDialer(&net.Dialer{}).DialContext(ctx, "udp", srv)
		}}
		c, cancel := context.WithTimeout(ctx, 4*time.Second)
		ips2, err2 := r.LookupNetIP(c, "ip", host)
		cancel()
		if err2 == nil && len(ips2) > 0 {
			return ips2, nil
		}
	}
	return nil, err
}

func resolveEndpoint(host string, port int) (string, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return netip.AddrPortFrom(ip, uint16(port)).String(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDialTimeout)
	defer cancel()
	ips, err := lookupDirect(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("amneziawg: resolve endpoint %s: %v", host, err)
	}
	pick := ips[0]
	for _, ip := range ips {
		if ip.Is4() {
			pick = ip
			break
		}
	}
	return netip.AddrPortFrom(pick.Unmap(), uint16(port)).String(), nil
}

func startAWG(p *Profile, logf func(string)) (*awgTunnel, error) {
	if p.WG == nil {
		return nil, fmt.Errorf("amneziawg: missing config")
	}
	var addrs []netip.Addr
	for _, a := range p.WG.Address {
		pre, err := netip.ParsePrefix(a)
		if err != nil {
			return nil, fmt.Errorf("amneziawg: address %q: %w", a, err)
		}
		addrs = append(addrs, pre.Addr())
	}
	var dnsAddrs []netip.Addr
	for _, d := range p.WG.DNS {
		if ip, err := netip.ParseAddr(strings.TrimSpace(d)); err == nil {
			dnsAddrs = append(dnsAddrs, ip)
		}
	}
	if len(dnsAddrs) == 0 {
		dnsAddrs = []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}
	}
	mtu := p.WG.MTU
	if mtu <= 0 {
		mtu = 1280
	}
	endpoint, err := resolveEndpoint(p.Server, p.Port)
	if err != nil {
		return nil, err
	}
	uapi, err := awgUAPI(p, endpoint)
	if err != nil {
		return nil, err
	}
	tdev, tnet, err := netstack.CreateNetTUN(addrs, dnsAddrs, mtu)
	if err != nil {
		return nil, fmt.Errorf("amneziawg netstack: %w", err)
	}
	logger := &device.Logger{
		Verbosef: awgVerbosef,
		Errorf: func(f string, a ...any) {
			if logf != nil {
				logf("amneziawg: " + fmt.Sprintf(f, a...))
			}
		},
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), logger)
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("amneziawg config: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("amneziawg up: %w", err)
	}
	if p.WG.AWG != nil && p.WG.AWG.S4 > 0 {
		primeTransportPadding(tnet)
	}
	t := &awgTunnel{dev: dev, tnet: tnet, endpoint: endpoint}
	for _, a := range addrs {
		a = a.Unmap()
		if a.Is4() && !t.local4.IsValid() {
			t.local4 = a
		} else if a.Is6() && !t.local6.IsValid() {
			t.local6 = a
		}
	}
	s, err := startSocksServer(t)
	if err != nil {
		dev.Close()
		return nil, err
	}
	t.socks = s
	return t, nil
}

// primeTransportPadding works around an amneziawg-go race (AWG 2.0, S4 > 0):
// device.NewDevice starts RoutineReadFromTUN, which loads the transport
// padding (S4) *before* blocking on its first TUN read — i.e. before IpcSet
// configured S4 — so the first packet the device ever reads from its TUN is
// sent without the S4 prefix. The server cannot classify it ("Received
// message with unknown type") and drops it: the first DNS query / TCP SYN of
// a session is lost and the first request stalls for a retransmit (5 s for
// DNS). Proven by TestAWGFirstPacketPadding. Sending one throw-away datagram
// first lets that stale read consume it; it goes to TEST-NET-1 discard.
func primeTransportPadding(tnet *netstack.Net) {
	c, err := tnet.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, 1}), 9))
	if err != nil {
		return
	}
	_, _ = c.Write([]byte{0})
	c.Close()
}

// awgVerbosef prints amneziawg-go's verbose log to stderr when
// VEILON_AWG_DEBUG is set (diagnostics on a host; off in the app).
var awgVerbosef = func() func(string, ...any) {
	if os.Getenv("VEILON_AWG_DEBUG") == "" {
		return func(string, ...any) {}
	}
	l := stdlog.New(os.Stderr, "awg-client: ", stdlog.Lmicroseconds)
	return l.Printf
}()

func (t *awgTunnel) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	return t.tnet.DialContext(ctx, "tcp", addr)
}

// ListenPacket returns an unconnected UDP socket in the tunnel for SOCKS5
// UDP ASSOCIATE (DNS and every other UDP flow from the TUN).
//
// The local address must name an address family: netstack.ListenUDP(&net.
// UDPAddr{}) (1.0.0-1.0.2) passes an invalid netip.AddrPort, so gVisor gets
// network protocol 0 and panics ("invalid protocol number = 0") in the SOCKS
// handler goroutine, killing the whole app on the first UDP packet (the
// first DNS query from another app) of an AmneziaWG session. Binding to
// 0.0.0.0 is not enough either (gVisor then finds no route: "network is
// unreachable"), so each family's socket binds to the interface address.
// Proven by TestAWGSocksUDPAssociate and TestTunAndroidProfiles/amneziawg/*.
func (t *awgTunnel) ListenPacket(ctx context.Context) (net.PacketConn, error) {
	pc := &awgPacketConn{}
	if t.local4.IsValid() {
		c, err := t.tnet.ListenUDPAddrPort(netip.AddrPortFrom(t.local4, 0))
		if err != nil {
			return nil, err
		}
		pc.v4 = c
	}
	if t.local6.IsValid() {
		c, err := t.tnet.ListenUDPAddrPort(netip.AddrPortFrom(t.local6, 0))
		if err != nil {
			if pc.v4 != nil {
				pc.v4.Close()
			}
			return nil, err
		}
		pc.v6 = c
	}
	if pc.v4 == nil && pc.v6 == nil {
		return nil, fmt.Errorf("amneziawg: no interface address for UDP")
	}
	return pc.start(), nil
}

// awgPacketConn is one unconnected UDP socket per address family in the
// tunnel's netstack (either may be nil), presented as one net.PacketConn.
type awgPacketConn struct {
	v4, v6 net.PacketConn
	in     chan awgDatagram
	done   chan struct{}
	once   sync.Once
	err    atomic.Pointer[error]
}

type awgDatagram struct {
	b    []byte
	from net.Addr
}

// single returns the only socket when there is just one family.
func (c *awgPacketConn) single() net.PacketConn {
	switch {
	case c.v6 == nil:
		return c.v4
	case c.v4 == nil:
		return c.v6
	}
	return nil
}

func (c *awgPacketConn) all() []net.PacketConn {
	var out []net.PacketConn
	for _, pc := range []net.PacketConn{c.v4, c.v6} {
		if pc != nil {
			out = append(out, pc)
		}
	}
	return out
}

func (c *awgPacketConn) start() *awgPacketConn {
	if c.single() != nil {
		return c
	}
	c.in = make(chan awgDatagram, 64)
	c.done = make(chan struct{})
	for _, pc := range c.all() {
		pc := pc
		go func() {
			defer recoverGo("amneziawg udp reader")
			buf := make([]byte, 65535)
			for {
				n, from, err := pc.ReadFrom(buf)
				if err != nil {
					c.err.CompareAndSwap(nil, &err)
					c.Close()
					return
				}
				d := awgDatagram{b: append([]byte(nil), buf[:n]...), from: from}
				select {
				case c.in <- d:
				case <-c.done:
					return
				}
			}
		}()
	}
	return c
}

func (c *awgPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if s := c.single(); s != nil {
		return s.ReadFrom(p)
	}
	select {
	case d := <-c.in:
		return copy(p, d.b), d.from, nil
	case <-c.done:
		if e := c.err.Load(); e != nil {
			return 0, nil, *e
		}
		return 0, nil, net.ErrClosed
	}
}

func (c *awgPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("amneziawg udp: unsupported address %v", addr)
	}
	if ip4 := ua.IP.To4(); ip4 != nil {
		if c.v4 == nil {
			return 0, fmt.Errorf("amneziawg udp: no IPv4 address in the tunnel for %v", addr)
		}
		return c.v4.WriteTo(p, &net.UDPAddr{IP: ip4, Port: ua.Port})
	}
	if c.v6 == nil {
		return 0, fmt.Errorf("amneziawg udp: no IPv6 address in the tunnel for %v", addr)
	}
	return c.v6.WriteTo(p, ua)
}

func (c *awgPacketConn) Close() error {
	c.once.Do(func() {
		if c.done != nil {
			close(c.done)
		}
		for _, pc := range c.all() {
			pc.Close()
		}
	})
	return nil
}

func (c *awgPacketConn) LocalAddr() net.Addr { return c.all()[0].LocalAddr() }

func (c *awgPacketConn) each(f func(net.PacketConn) error) error {
	var first error
	for _, pc := range c.all() {
		if err := f(pc); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (c *awgPacketConn) SetDeadline(t time.Time) error {
	return c.each(func(pc net.PacketConn) error { return pc.SetDeadline(t) })
}
func (c *awgPacketConn) SetReadDeadline(t time.Time) error {
	return c.each(func(pc net.PacketConn) error { return pc.SetReadDeadline(t) })
}
func (c *awgPacketConn) SetWriteDeadline(t time.Time) error {
	return c.each(func(pc net.PacketConn) error { return pc.SetWriteDeadline(t) })
}

func (t *awgTunnel) ResolveUDP(ctx context.Context, host string, port int) (net.Addr, error) {
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}
	ips, err := t.tnet.LookupContextHost(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: %v", host, err)
	}
	return net.ResolveUDPAddr("udp", net.JoinHostPort(ips[0], strconv.Itoa(port)))
}

func (t *awgTunnel) Close() error {
	if t.socks != nil {
		t.socks.Close()
	}
	if t.dev != nil {
		t.dev.Close()
	}
	return nil
}
