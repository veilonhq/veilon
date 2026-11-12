package veiloncore

import (
	"net"
	"sync/atomic"
	"syscall"

	xinternet "github.com/xtls/xray-core/transport/internet"
)

// selfProtect is set while a VPN session keeps the app inside the VPN
// (non-gVisor stack, see tunStack): every socket a core opens towards the
// server must then be protected (VpnService.protect) or it would loop into
// the tun. nil in the default gVisor mode (app excluded, nothing to do).
var selfProtect atomic.Pointer[func(fd int) bool]

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// protectRaw protects a socket when selfProtect is set.
func protectRaw(c syscall.RawConn) error {
	f := selfProtect.Load()
	if f == nil {
		return nil
	}
	fn := *f
	return c.Control(func(fd uintptr) { fn(int(fd)) })
}

// protectControl is a net.Dialer / net.ListenConfig Control for the core's
// own direct sockets (certificate pin probes, SSH transport, resolvers).
func protectControl(_, _ string, c syscall.RawConn) error { return protectRaw(c) }

// protectedDialer is a net.Dialer with protectControl.
func protectedDialer(d *net.Dialer) *net.Dialer {
	d.Control = protectControl
	return d
}

func init() {
	// Xray (sidecar in this process): every outbound socket it dials goes
	// through the registered controllers.
	_ = xinternet.RegisterDialerController(func(_, _ string, c syscall.RawConn) error { return protectRaw(c) })
}
