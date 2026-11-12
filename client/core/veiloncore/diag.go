package veiloncore

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	tun "github.com/sagernet/sing-tun"
	N "github.com/sagernet/sing/common/network"
)

// tunDiag counts connections that other apps open through the TUN and
// writes short diagnostics to the log page (always, whatever the log level):
// the first few TUN connections, a periodic summary, and a warning when no
// app traffic reaches the tunnel at all. Dial / DNS errors themselves are
// logged by sing-box (error level) and by Xray (xrayLogHandler).
type tunDiag struct {
	tcp, udp, dns atomic.Int64
	shown         atomic.Int32
	stop          chan struct{}
	once          sync.Once
}

const (
	diagShowFirst = 5
	diagNoTraffic = 20 * time.Second
	diagSummary   = 60 * time.Second
)

func newTunDiag() *tunDiag { return &tunDiag{stop: make(chan struct{})} }

func (d *tunDiag) note(network string, m adapter.InboundContext, out adapter.Outbound) {
	if m.Inbound != "tun-in" {
		return
	}
	switch {
	case m.Protocol == "dns":
		d.dns.Add(1)
	case network == N.NetworkUDP:
		d.udp.Add(1)
	default:
		d.tcp.Add(1)
	}
	if d.shown.Add(1) <= diagShowFirst {
		via := ""
		if out != nil {
			via = " via " + out.Type() + "[" + out.Tag() + "]"
		}
		dst := m.Destination.String()
		if m.Domain != "" {
			dst = m.Domain + " (" + dst + ")"
		}
		pushLogLevel("info", fmt.Sprintf("tun: %s %s -> %s%s", network, m.Source.String(), dst, via))
	}
}

func (d *tunDiag) RoutedConnection(_ context.Context, conn net.Conn, m adapter.InboundContext, _ adapter.Rule, out adapter.Outbound) net.Conn {
	d.note(N.NetworkTCP, m, out)
	return conn
}

func (d *tunDiag) RoutedPacketConnection(_ context.Context, conn N.PacketConn, m adapter.InboundContext, _ adapter.Rule, out adapter.Outbound) N.PacketConn {
	d.note(N.NetworkUDP, m, out)
	return conn
}

func (d *tunDiag) RoutedFlow(_ context.Context, m adapter.InboundContext, _ adapter.Rule, out adapter.Outbound) tun.FlowTracker {
	d.note(m.Network, m, out)
	return nil
}

func (d *tunDiag) total() int64 { return d.tcp.Load() + d.udp.Load() + d.dns.Load() }

// run reports until Close. stack is the effective tun stack.
func (d *tunDiag) run(stack string, mtu int) {
	pushLogLevel("info", fmt.Sprintf("tun: up (stack %s, mtu %d, dns 172.19.0.2, app itself excluded from the VPN)", stack, mtu))
	select {
	case <-d.stop:
		return
	case <-time.After(diagNoTraffic):
	}
	if d.total() == 0 {
		pushLogLevel("warn", fmt.Sprintf("tun: no connection from other apps reached the tunnel in %d s (stack %s). Open a website, then send these logs.", int(diagNoTraffic/time.Second), stack))
	}
	t := time.NewTicker(diagSummary)
	defer t.Stop()
	var last int64 = -1
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			if n := d.total(); n != last {
				last = n
				pushLogLevel("info", fmt.Sprintf("tun: connections so far: %d TCP, %d UDP, %d DNS", d.tcp.Load(), d.udp.Load(), d.dns.Load()))
			}
		}
	}
}

func (d *tunDiag) Close() error {
	d.once.Do(func() { close(d.stop) })
	return nil
}
