package veiloncore

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/bufio"
	N "github.com/sagernet/sing/common/network"
)

// Traffic accounting for the VPN session (Run 105).
//
// Until 1.0.3 the app showed its own UID's socket counters. Those also move
// when the server is dead: sing-box hands a TCP connection to a local
// sidecar (Xray SOCKS, AmneziaWG SOCKS) that accepts at once, apps keep
// sending, and every retry / handshake attempt is counted, so a dead
// server looked like a working one with traffic.
//
// Now the core counts the payload of the connections other apps open
// through the tun (at the tun side of each routed connection), and an
// upload is only credited once the remote side has answered on that
// connection (at least one byte came back). Bytes an app pushes into a
// connection that never gets an answer (unreachable / wrong server) are
// never counted.

var (
	trafficUp   atomic.Int64
	trafficDown atomic.Int64
	// proxyState: 0 unknown (checking), 1 server reachable, 2 unreachable.
	proxyState atomic.Int32
	// proxyWaitSince (unix ns, 0 = none) is when the oldest connection
	// routed to the proxy that is still waiting for its first answer was
	// opened, counted only since the last answer from the proxy. While the
	// server answers it keeps returning to 0; when the tunnel is dead every
	// new connection waits and the age grows (1.0.6: tunnel-loss watch).
	proxyWaitSince atomic.Int64
)

const (
	proxyUnknown int32 = iota
	proxyOK
	proxyUnreachable
)

func resetTraffic() {
	trafficUp.Store(0)
	trafficDown.Store(0)
	proxyState.Store(proxyUnknown)
	proxyWaitSince.Store(0)
}

// nowNano is time.Now().UnixNano(); a variable for the tests.
var nowNano = func() int64 { return time.Now().UnixNano() }

// ProxyStalledMs is how long (ms) connections through the proxy have been
// waiting for an answer with no answer from the proxy at all since then;
// 0 when nothing waits. A healthy session drops back to 0 with every
// answer; a dead tunnel (server gone, blocked, sidecar dead) grows without
// bound while apps keep trying. The app uses it, with the kill switch off,
// to notice a lost tunnel and give the phone its normal internet back.
// Only statistics: routing and the ping path are not affected.
func ProxyStalledMs() (ms int64) {
	defer apiGuard("ProxyStalledMs", func(string) { ms = 0 })
	since := proxyWaitSince.Load()
	if since == 0 {
		return 0
	}
	d := (nowNano() - since) / int64(time.Millisecond)
	if d < 0 {
		return 0
	}
	return d
}

// TrafficUp is the session's upload in bytes (payload that reached the
// remote side through the tunnel; see stats.go).
func TrafficUp() int64 { return trafficUp.Load() }

// TrafficDown is the session's download in bytes.
func TrafficDown() int64 { return trafficDown.Load() }

// ProxyStatus is "" while unknown, "ok" when the server answered (proxy
// check or real traffic), "unreachable" when the one-time proxy check
// failed and no traffic came back since.
func ProxyStatus() string {
	switch proxyState.Load() {
	case proxyOK:
		return "ok"
	case proxyUnreachable:
		return "unreachable"
	}
	return ""
}

type trafficTracker struct{}

// connCounter holds one connection's upload until the first answer.
type connCounter struct {
	answered atomic.Bool
	pending  atomic.Int64
	viaProxy bool
}

func (c *connCounter) up(n int64) {
	if c.answered.Load() {
		trafficUp.Add(n)
		return
	}
	c.pending.Add(n)
	if c.answered.Load() { // answered meanwhile: flush what we just parked
		trafficUp.Add(c.pending.Swap(0))
	}
}

func (c *connCounter) down(n int64) {
	if n <= 0 {
		return
	}
	if c.answered.CompareAndSwap(false, true) {
		trafficUp.Add(c.pending.Swap(0))
		if c.viaProxy {
			proxyState.Store(proxyOK)
			proxyWaitSince.Store(0)
		}
	}
	trafficDown.Add(n)
}

func newConnCounter(m adapter.InboundContext, out adapter.Outbound) *connCounter {
	c := &connCounter{viaProxy: out != nil && out.Tag() == tagProxy}
	if c.viaProxy {
		proxyWaitSince.CompareAndSwap(0, nowNano())
	}
	return c
}

func (trafficTracker) RoutedConnection(_ context.Context, conn net.Conn, m adapter.InboundContext, _ adapter.Rule, out adapter.Outbound) net.Conn {
	if m.Inbound != "tun-in" {
		return conn
	}
	c := newConnCounter(m, out)
	// Read = app → remote (upload), Write = remote → app (download).
	return bufio.NewCounterConn(conn, []N.CountFunc{c.up}, []N.CountFunc{c.down})
}

func (trafficTracker) RoutedPacketConnection(_ context.Context, conn N.PacketConn, m adapter.InboundContext, _ adapter.Rule, out adapter.Outbound) N.PacketConn {
	if m.Inbound != "tun-in" {
		return conn
	}
	c := newConnCounter(m, out)
	return bufio.NewCounterPacketConn(conn, []N.CountFunc{c.up}, []N.CountFunc{c.down})
}

func (trafficTracker) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}
