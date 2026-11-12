package veiloncore

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

// session is one running configuration: optional sidecars (Xray,
// AmneziaWG) chained into a sing-box instance.
type session struct {
	box      *box.Box
	cancel   context.CancelFunc
	closers  []io.Closer
	platform *boxPlatform
	config   obj
	internal *localProxy

	closeOnce sync.Once
	closeErr  error
}

// Close tears the session down. Idempotent (1.0.6): a second Close (e.g.
// Stop after a Start that already closed it, or two quick Stops) returns
// the first result without closing anything twice, and a panic in one
// component is recovered so the others are still closed.
func (s *session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = s.closeAll() })
	return s.closeErr
}

func (s *session) closeAll() error {
	var errs []error
	if s.box != nil {
		if err := safeClose("sing-box", s.box.Close); err != nil {
			errs = append(errs, err)
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	for i := len(s.closers) - 1; i >= 0; i-- {
		c := s.closers[i]
		if c == nil {
			continue
		}
		if err := safeClose("sidecar", c.Close); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type logFunc func(string)

// prepare builds (and unless dry, starts the sidecars of) a session.
func prepare(ctx context.Context, p *Profile, st Settings, dry bool, logf logFunc) (*buildPlan, []io.Closer, error) {
	var closers []io.Closer
	fail := func(err error) (*buildPlan, []io.Closer, error) {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i].Close()
		}
		return nil, nil, err
	}
	if removedTypes[p.Type] {
		return fail(ErrTypeRemoved)
	}
	p = p.withAddressOverride()
	core, err := ResolveCore(p, st)
	if err != nil {
		return fail(err)
	}
	plan := &buildPlan{}
	if !dry {
		if err := resolvePins(ctx, p, core); err != nil {
			return fail(err)
		}
	}
	switch core {
	case CoreNone:
		return fail(errors.New("MTProto proxies are opened in Telegram"))
	case CoreSingbox:
		o, isEP, err := singboxOutbound(p, tagProxy)
		if err != nil {
			return fail(err)
		}
		if isEP {
			plan.endpoints = append(plan.endpoints, o)
		} else {
			plan.outbounds = append(plan.outbounds, o)
		}
	case CoreXray:
		if xrayRefusesCleartext(p) {
			return fail(ErrXrayCleartextPublicIP)
		}
		user, pass := randomToken(8), randomToken(16)
		port := 1
		if !dry {
			var err error
			if port, err = freePort(); err != nil {
				return fail(err)
			}
		}
		cfg, err := xrayConfig(p, port, user, pass, st)
		if err != nil {
			return fail(err)
		}
		if dry {
			if err := checkXrayConfig(cfg); err != nil {
				return fail(fmt.Errorf("xray: %w", err))
			}
		} else {
			x, err := startXray(cfg)
			if err != nil {
				return fail(err)
			}
			closers = append(closers, x)
		}
		plan.outbounds = append(plan.outbounds, socksOutbound(tagProxy, port, user, pass, false))
	case CoreAWG:
		plan.ownSockets = true
		port, user, pass := 1, "u", "p"
		if dry {
			if _, err := awgUAPI(p, "127.0.0.1:1"); err != nil {
				return fail(err)
			}
		} else {
			t, err := startAWG(p, logf)
			if err != nil {
				return fail(err)
			}
			closers = append(closers, t)
			port, user, pass = t.socks.Port(), t.socks.user, t.socks.pass
		}
		plan.outbounds = append(plan.outbounds, socksOutbound(tagProxy, port, user, pass, false))
	default:
		return fail(fmt.Errorf("unsupported core %s", core))
	}
	return plan, closers, nil
}

func boxContext(ctx context.Context) context.Context {
	return include.Context(ctx)
}

func parseBoxOptions(ctx context.Context, cfg obj) (option.Options, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return option.Options{}, err
	}
	return sjson.UnmarshalExtendedContext[option.Options](ctx, b)
}

type platformLog struct{ f func(level int32, msg string) }

func (w platformLog) WriteMessage(level log.Level, message string) {
	if w.f != nil {
		level, message = cleanCoreLog(level, message)
		w.f(int32(level), message)
	}
}

// newSession builds and starts a session. iif may be nil (no tun).
func newSession(p *Profile, st Settings, iif Platform, withTun bool) (_ *session, retErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	var s *session
	// A panic while building / starting (1.0.6): close what was opened so
	// far (sidecar ports, tun), then let the API guard turn it into an error.
	defer func() {
		if r := recover(); r != nil {
			if s != nil {
				_ = s.Close()
			} else {
				cancel()
			}
			panic(r)
		}
	}()
	logf := func(m string) { pushLog(log.LevelWarn, m) }
	// Before any core (Xray / AWG / DNS tunnel sidecars in prepare, then
	// sing-box) can touch the file system.
	preparePaths(st)
	prepCtx, prepCancel := context.WithTimeout(ctx, 20*time.Second)
	plan, closers, err := prepare(prepCtx, p, st, false, logf)
	prepCancel()
	if err != nil {
		cancel()
		return nil, err
	}
	s = &session{cancel: cancel, closers: closers}
	if withTun {
		if port, err := freePort(); err == nil {
			plan.internal = &localProxy{Port: port, User: randomToken(8), Pass: randomToken(16)}
			s.internal = plan.internal
		}
	}
	cfg := buildSingboxConfig(plan, st, withTun)
	s.config = cfg
	bctx := withFileManager(boxContext(ctx), st)
	opts, err := parseBoxOptions(bctx, cfg)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("config: %w", err)
	}
	s.platform = newBoxPlatform(iif, st.AppPackage)
	if withTun && iif != nil && selfIncluded(cfg) {
		s.platform.includeSelf = true
		fn := func(fd int) bool { return iif.Protect(int32(fd)) }
		selfProtect.Store(&fn)
		s.closers = append(s.closers, closerFunc(func() error { selfProtect.CompareAndSwap(&fn, nil); return nil }))
	}
	bctx = service.ContextWith[adapter.PlatformInterface](bctx, s.platform)
	var lw log.PlatformWriter
	if withTun {
		lw = platformLog{f: func(level int32, msg string) {
			pushLog(log.Level(level), msg)
			if iif != nil {
				iif.OnLog(level, msg)
			}
		}}
	}
	instance, err := box.New(box.Options{Context: bctx, Options: opts, PlatformLogWriter: lw})
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("create: %w", err)
	}
	s.box = instance
	var diag *tunDiag
	if withTun {
		setXrayLogLevel(st.LogLevel)
		diag = newTunDiag()
		instance.Router().AppendTracker(diag)
		resetTraffic()
		instance.Router().AppendTracker(trafficTracker{})
	}
	if err := instance.Start(); err != nil {
		s.Close()
		return nil, fmt.Errorf("start: %w", err)
	}
	if diag != nil {
		s.closers = append(s.closers, diag)
		stack, _ := tunInbound(cfg)["stack"].(string)
		go func() {
			defer recoverGoKeep("tun diagnostics")
			diag.run(stack, st.MTU)
		}()
		go func() {
			defer recoverGoKeep("proxy check")
			s.checkProxy(ctx)
		}()
	}
	return s, nil
}

// proxyCheckTarget is dialed once through the proxy after a VPN session
// started. A session can start although its server is unreachable (sing-box
// does not dial at start), and then other apps silently get no internet
// (DNS queries just time out, nothing is logged at the default level). This
// puts the real reason on the log page.
var proxyCheckTarget = "1.1.1.1:443"

// proxyCheckSNI is the TLS server name sent to proxyCheckTarget.
var proxyCheckSNI = "one.one.one.one"

func (s *session) checkProxy(ctx context.Context) {
	var d N.Dialer
	if ob, ok := s.box.Outbound().Outbound(tagProxy); ok {
		d = ob
	} else if ep, ok := s.box.Endpoint().Get(tagProxy); ok {
		d = ep
	} else {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	start := time.Now()
	c, err := d.DialContext(cctx, N.NetworkTCP, M.ParseSocksaddr(proxyCheckTarget))
	if ctx.Err() != nil {
		return // session closed meanwhile
	}
	if err == nil {
		// A local sidecar (Xray / AmneziaWG SOCKS) and lazy outbounds
		// accept the dial without reaching the server: only an answer
		// from the target proves the path (TLS ServerHello from it).
		tc := tls.Client(c, &tls.Config{ServerName: proxyCheckSNI, InsecureSkipVerify: true}) //nolint:gosec // reachability only
		err = tc.HandshakeContext(cctx)
		tc.Close()
	}
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		proxyState.CompareAndSwap(proxyUnknown, proxyUnreachable)
		pushLogLevel("error", fmt.Sprintf("proxy check: cannot open a connection through the server (%v). Other apps will have no internet until the server is reachable.", err))
		return
	}
	proxyState.Store(proxyOK)
	pushLogLevel("info", fmt.Sprintf("proxy check: server reachable (%d ms)", time.Since(start).Milliseconds()))
}

// checkProfile validates the generated configuration without connecting.
func checkProfile(p *Profile, st Settings, withTun bool) (obj, error) {
	plan, _, err := prepare(context.Background(), p, st, true, nil)
	if err != nil {
		return nil, err
	}
	cfg := buildSingboxConfig(plan, st, withTun)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bctx := withFileManager(boxContext(ctx), st)
	opts, err := parseBoxOptions(bctx, cfg)
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	bctx = service.ContextWith[adapter.PlatformInterface](bctx, newBoxPlatform(nil, st.AppPackage))
	instance, err := box.New(box.Options{Context: bctx, Options: opts})
	if err != nil {
		return cfg, err
	}
	instance.Close()
	return cfg, nil
}

// urlTest measures the real delay (HTTP GET through the proxy).
func urlTest(p *Profile, st Settings, link string, timeout time.Duration) (int, error) {
	return urlTestOnce(p, st, link, timeout)
}

func urlTestOnce(p *Profile, st Settings, link string, timeout time.Duration) (int, error) {
	st.MixedPort = 0
	st.LogLevel = "error"
	s, err := newSession(p, st, nil, false)
	if err != nil {
		return -1, err
	}
	defer s.Close()
	for _, c := range s.closers {
		if t, ok := c.(*awgTunnel); ok {
			// Wait for the WireGuard handshake first so a failure says
			// why (instead of an opaque HTTP timeout) and the delay
			// measures the tunnel, not the handshake.
			if timeout < awgMinTimeout {
				timeout = awgMinTimeout
			}
			hctx, cancel := context.WithTimeout(context.Background(), timeout*2/3)
			err := t.waitHandshake(hctx)
			cancel()
			if err != nil {
				return -1, err
			}
		}
	}
	ob, ok := s.box.Outbound().Outbound(tagProxy)
	if !ok {
		if ep, ok2 := s.box.Endpoint().Get(tagProxy); ok2 {
			return measure(ep, link, timeout, st.FullHandshakePing)
		}
		return -1, errors.New("proxy outbound missing")
	}
	return measure(ob, link, timeout, st.FullHandshakePing)
}

func measure(d adapter.Outbound, link string, timeout time.Duration, fullHandshake bool) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if !fullHandshake {
		return measureRTT(ctx, d, link)
	}
	// "Full handshake" ping (pre-1.0.2 behaviour): each sample opens a new
	// proxy connection, so it includes TCP + TLS + transport handshakes.
	// The first request warms up; report the second when it succeeds.
	d1, err := urltest.URLTest(ctx, link, d)
	if err != nil {
		return -1, err
	}
	d2, err := urltest.URLTest(ctx, link, d)
	if err == nil && d2 > 0 {
		return int(d2), nil
	}
	// A sub-millisecond answer (local / LAN server) is a success, not
	// "no response": callers treat <= 0 as failure.
	return max(int(d1), 1), nil
}

// rttSamples is how many timed requests measureRTT sends after warm-up.
const rttSamples = 3

// measureRTT opens one proxy connection, sends one untimed warm-up request
// (proxy + transport + target TLS handshakes happen here), then times
// rttSamples HEAD requests over the same kept-alive connection and returns
// the median, rounded to whole milliseconds. Each timed request is one real
// HTTP round trip through the tunnel; nothing is estimated or adjusted.
func measureRTT(ctx context.Context, d N.Dialer, link string) (int, error) {
	samples, err := rttSamplesOver(ctx, d, link, rttSamples)
	if err != nil {
		return -1, err
	}
	ms := int(math.Round(float64(medianDuration(samples)) / float64(time.Millisecond)))
	if ms < 1 {
		// Sub-millisecond (same host / LAN): 0 would read as failure.
		ms = 1
	}
	return ms, nil
}

func medianDuration(v []time.Duration) time.Duration {
	s := append([]time.Duration(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// rttSamplesOver returns n timed request durations over one kept-alive
// connection (after an untimed warm-up). It fails if the connection was not
// reused for a timed request, so a sample never includes a handshake.
func rttSamplesOver(ctx context.Context, d N.Dialer, link string, n int) ([]time.Duration, error) {
	if link == "" {
		link = "https://www.gstatic.com/generate_204"
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	dials := 0
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials++
			return d.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr(addr))
		},
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		MaxConnsPerHost:     1,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
		ForceAttemptHTTP2:   false,
		TLSClientConfig:     &tls.Config{ServerName: u.Hostname()},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func() (time.Duration, bool, error) {
		var reused bool
		trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }}
		req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodHead, link, nil)
		if err != nil {
			return 0, false, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, false, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return time.Since(start), reused, nil
	}
	if _, _, err := do(); err != nil { // warm-up, not counted
		return nil, err
	}
	out := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		dt, reused, err := do()
		if err != nil {
			return nil, err
		}
		if !reused {
			return nil, fmt.Errorf("ping: proxy connection was not kept alive (dials=%d)", dials)
		}
		out = append(out, dt)
	}
	return out, nil
}

// ---- global running session ----

var (
	runMu   sync.Mutex
	running *session
)

func startGlobal(p *Profile, st Settings, iif Platform) error {
	runMu.Lock()
	defer runMu.Unlock()
	if old := running; old != nil {
		// Cleared before Close: even if closing panics, the next Stop /
		// Start never sees (and closes again) a half-closed session.
		running = nil
		_ = old.Close()
	}
	s, err := newSession(p, st, iif, true)
	if err != nil {
		return err
	}
	running = s
	return nil
}

// internalProxy returns the running session's local authenticated proxy.
func internalProxy() *localProxy {
	runMu.Lock()
	defer runMu.Unlock()
	if running == nil {
		return nil
	}
	return running.internal
}

func stopGlobal() error {
	runMu.Lock()
	defer runMu.Unlock()
	old := running
	if old == nil {
		return nil
	}
	running = nil
	return old.Close()
}

// tunInbound returns the tun inbound of a rendered config (nil if none).
func tunInbound(cfg obj) obj {
	ins, _ := cfg["inbounds"].([]any)
	for _, i := range ins {
		if o, ok := i.(obj); ok && o["type"] == "tun" {
			return o
		}
	}
	return nil
}
