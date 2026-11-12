package veiloncore

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"golang.org/x/sys/unix"
)

// Platform is implemented by the Android app (Kotlin VpnService).
type Platform interface {
	// OpenTun establishes the VPN interface described by optionsJSON
	// (see TunRequest) and returns its file descriptor.
	OpenTun(optionsJSON string) (int32, error)
	// Protect excludes a socket from the VPN (VpnService.protect).
	Protect(fd int32) bool
	// OnLog receives core log lines.
	OnLog(level int32, message string)
}

// TunRequest is what the app needs to build the VpnService interface.
type TunRequest struct {
	MTU             int      `json:"mtu"`
	Inet4Address    []string `json:"inet4Address"`
	Inet6Address    []string `json:"inet6Address"`
	Inet4Routes     []string `json:"inet4Routes"`
	Inet6Routes     []string `json:"inet6Routes"`
	DNSServer       string   `json:"dnsServer"`
	IncludePackages []string `json:"includePackages"`
	ExcludePackages []string `json:"excludePackages"`
}

// testExcludeSelf (tests only) restores the 1.0.1 behaviour of always
// excluding the app, as a negative control for TestTunAndroidRouting.
var testExcludeSelf bool

// boxPlatform adapts Platform to sing-box's adapter.PlatformInterface.
// A nil iif gives a tun-less platform used for latency tests and checks.
type boxPlatform struct {
	iif            Platform
	networkManager adapter.NetworkManager
	monitor        *staticMonitor
	myAddrs        []netip.Addr
	appPackage     string
	// includeSelf: keep the app inside the VPN (non-gVisor stack; every
	// core socket is protected instead). See tunStack.
	includeSelf bool
}

var _ adapter.PlatformInterface = (*boxPlatform)(nil)

func newBoxPlatform(iif Platform, appPackage string) *boxPlatform {
	p := &boxPlatform{iif: iif, monitor: &staticMonitor{}, appPackage: appPackage}
	p.monitor.platform = p
	return p
}

// updateInterfaces refreshes sing-box's network interface list (like
// libbox's platform monitor). Only needed when auto_detect_interface is on
// (app inside the VPN); the default gVisor mode never reads the list.
func (p *boxPlatform) updateInterfaces() {
	if p.includeSelf && p.networkManager != nil {
		if err := p.networkManager.UpdateInterfaces(); err != nil {
			pushLogLevel("warn", "network: update interfaces: "+err.Error())
		}
	}
}

func (p *boxPlatform) Initialize(nm adapter.NetworkManager) error {
	p.networkManager = nm
	return nil
}

func (p *boxPlatform) UsePlatformAutoDetectInterfaceControl() bool { return true }

func (p *boxPlatform) AutoDetectInterfaceControl(fd int) error {
	if p.iif != nil {
		p.iif.Protect(int32(fd))
	} else if f := selfProtect.Load(); f != nil {
		// tun-less instance (latency test) while the VPN keeps the app
		// inside it: protect like the session does.
		(*f)(fd)
	}
	return nil
}

func (p *boxPlatform) UsePlatformInterface() bool { return p.iif != nil }

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, 0, len(ps))
	for _, x := range ps {
		out = append(out, x.String())
	}
	return out
}

func (p *boxPlatform) OpenInterface(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	if p.iif == nil {
		return nil, os.ErrInvalid
	}
	ranges, err := options.BuildAutoRouteRanges(true)
	if err != nil {
		return nil, err
	}
	req := TunRequest{MTU: int(options.MTU), Inet4Address: prefixStrings(options.Inet4Address), Inet6Address: prefixStrings(options.Inet6Address),
		IncludePackages: options.IncludePackage, ExcludePackages: options.ExcludePackage}
	for _, r := range ranges {
		if r.Addr().Is4() {
			req.Inet4Routes = append(req.Inet4Routes, r.String())
		} else {
			req.Inet6Routes = append(req.Inet6Routes, r.String())
		}
	}
	if len(options.Inet4Address) > 0 {
		req.DNSServer = options.Inet4Address[0].Addr().Next().String()
	}
	if p.appPackage != "" && len(req.IncludePackages) == 0 && (!p.includeSelf || testExcludeSelf) {
		req.ExcludePackages = appendUnique(req.ExcludePackages, p.appPackage)
	}
	b, _ := json.Marshal(req)
	fd, err := p.iif.OpenTun(string(b))
	if err != nil {
		return nil, err
	}
	name, err := tunName(fd)
	if err != nil {
		return nil, fmt.Errorf("query tun name: %w", err)
	}
	options.Name = name
	if options.InterfaceMonitor != nil {
		options.InterfaceMonitor.RegisterMyInterface(name)
	}
	dupFd, err := syscall.Dup(int(fd))
	if err != nil {
		return nil, fmt.Errorf("dup tun fd: %w", err)
	}
	options.FileDescriptor = dupFd
	p.myAddrs = nil
	for _, a := range options.Inet4Address {
		p.myAddrs = append(p.myAddrs, a.Addr())
	}
	for _, a := range options.Inet6Address {
		p.myAddrs = append(p.myAddrs, a.Addr())
	}
	return tun.New(*options)
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func tunName(fd int32) (string, error) {
	var ifr [unix.IFNAMSIZ + 64]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNGETIFF), uintptr(unsafe.Pointer(&ifr[0])))
	if errno != 0 {
		return "", errno
	}
	return unix.ByteSliceToString(ifr[:]), nil
}

func (p *boxPlatform) ProcessPlatformOptions(option.TunPlatformOptions) error { return nil }
func (p *boxPlatform) UsePlatformDefaultInterfaceMonitor() bool               { return true }
func (p *boxPlatform) CreateDefaultInterfaceMonitor(logger.Logger) tun.DefaultInterfaceMonitor {
	return p.monitor
}
func (p *boxPlatform) UsePlatformNetworkInterfaces() bool { return true }

// NetworkInterfaces reports the default network only. sing-box consults
// it when auto_detect_interface is on (app inside the VPN, see tunStack):
// its "default" network strategy dials over the interface whose index
// equals the default interface's, without binding (the socket is
// protected instead). An empty list fails every dial ("no available
// network interface").
func (p *boxPlatform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	iif := *p.monitor.DefaultInterface()
	iif.Flags |= net.FlagUp | net.FlagRunning // sing-box drops interfaces that are not up
	return []adapter.NetworkInterface{{Interface: iif, Type: C.InterfaceTypeOther}}, nil
}
func (p *boxPlatform) UnderNetworkExtension() bool                     { return false }
func (p *boxPlatform) NetworkExtensionIncludeAllNetworks() bool        { return false }
func (p *boxPlatform) ClearDNSCache()                                  {}
func (p *boxPlatform) RequestPermissionForWIFIState() error            { return nil }
func (p *boxPlatform) ReadWIFIState(context.Context) adapter.WIFIState { return adapter.WIFIState{} }
func (p *boxPlatform) UsePlatformConnectionOwnerFinder() bool          { return false }
func (p *boxPlatform) FindConnectionOwner(*adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	return nil, os.ErrInvalid
}
func (p *boxPlatform) UsePlatformWIFIMonitor() bool                 { return false }
func (p *boxPlatform) UsePlatformNotification() bool                { return false }
func (p *boxPlatform) SendNotification(*adapter.Notification) error { return nil }
func (p *boxPlatform) CancelNotification(string, int32) error       { return nil }
func (p *boxPlatform) MyInterfaceAddress() []netip.Addr             { return p.myAddrs }
func (p *boxPlatform) UsePlatformNeighborResolver() bool            { return false }
func (p *boxPlatform) StartNeighborMonitor(adapter.NeighborUpdateListener) error {
	return os.ErrInvalid
}
func (p *boxPlatform) CloseNeighborMonitor(adapter.NeighborUpdateListener) error { return nil }
func (p *boxPlatform) UsePlatformShell() bool                                    { return false }
func (p *boxPlatform) CheckPlatformShell() error                                 { return os.ErrInvalid }
func (p *boxPlatform) OpenShellSession(*adapter.PlatformUser, string, []string, string, int32, int32) (adapter.ShellSession, error) {
	return nil, os.ErrInvalid
}
func (p *boxPlatform) LookupUser(string) (*adapter.PlatformUser, error) { return nil, os.ErrInvalid }
func (p *boxPlatform) LookupSFTPServer() (string, error)                { return "", os.ErrInvalid }
func (p *boxPlatform) ReadSystemSSHHostKey() ([]byte, error)            { return nil, os.ErrInvalid }
func (p *boxPlatform) TailscaleHostname() string                        { return "" }
func (p *boxPlatform) UsePlatformBridge() bool                          { return false }
func (p *boxPlatform) CreateBridge(adapter.BridgeOptions) (adapter.BridgeSession, error) {
	return nil, os.ErrInvalid
}
func (p *boxPlatform) UsePlatformLocalDNSTransport() bool { return false }
func (p *boxPlatform) LocalDNSTransport() dns.TransportConstructorFunc[option.LocalDNSServerOptions] {
	return nil
}

// defaultIface is Android's current default (underlying) network
// interface, reported by the app (SetDefaultInterface from its default
// NetworkCallback). The cores never bind to it (the app's own sockets are
// excluded from the VPN and reach the network through the default route);
// sing-box only needs a non-nil default interface, otherwise it logs
// "missing default interface" and pauses the network (WireGuard endpoints
// stop sending; URL-test groups stop) at start and on every network change.
var defaultIface atomic.Pointer[control.Interface]

// placeholderIface stands in until the app has reported a network.
var placeholderIface = &control.Interface{Index: 0, Name: "android-default"}

// staticMonitor is the default-interface monitor fed by the app.
type staticMonitor struct {
	platform  *boxPlatform
	mu        sync.Mutex
	callbacks list.List[tun.DefaultInterfaceUpdateCallback]
	mine      []string
}

func (m *staticMonitor) Start() error {
	if m.platform != nil {
		m.platform.updateInterfaces()
	}
	return nil
}
func (m *staticMonitor) Close() error { return nil }
func (m *staticMonitor) DefaultInterface() *control.Interface {
	if i := defaultIface.Load(); i != nil {
		return i
	}
	return placeholderIface
}
func (m *staticMonitor) OverrideAndroidVPN() bool { return false }
func (m *staticMonitor) AndroidVPNEnabled() bool  { return false }
func (m *staticMonitor) RegisterCallback(cb tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callbacks.PushBack(cb)
}
func (m *staticMonitor) UnregisterCallback(e *list.Element[tun.DefaultInterfaceUpdateCallback]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks.Remove(e)
}
func (m *staticMonitor) RegisterMyInterface(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mine = append(m.mine, name)
}
func (m *staticMonitor) MyInterfaces() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.mine...)
}

// notifyNetworkChanged tells sing-box the network changed (connections are
// reset so they re-dial over the new default network).
func (m *staticMonitor) notifyNetworkChanged() {
	if m.platform != nil {
		m.platform.updateInterfaces()
	}
	m.mu.Lock()
	cbs := m.callbacks.Array()
	m.mu.Unlock()
	iface := m.DefaultInterface()
	for _, cb := range cbs {
		cb(iface, 0)
	}
}
