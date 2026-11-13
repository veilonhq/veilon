package xray

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/presence"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// Status describes the Xray process.
type Status struct {
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Running   bool       `json:"running"`
	StartedAt *time.Time `json:"started_at"`
	Error     string     `json:"error,omitempty"`
	// Warnings lists inbounds the engine could not serve (and why).
	Warnings []string `json:"warnings"`
	// Enabled is false for an engine that has no inbounds to serve (it is
	// then not started at all).
	Enabled bool `json:"enabled"`
	// Protocols lists the inbound protocols this engine serves.
	Protocols []string `json:"protocols"`
	// Instances is the number of processes (one per inbound for some engines).
	Instances int `json:"instances"`
	// Stopped is true when an admin switched the engine off; it then stays
	// off (also across panel restarts) until switched on again.
	Stopped bool `json:"stopped"`
	// Busy names a running maintenance job ("install", "geo"), if any.
	Busy string `json:"busy,omitempty"`
}

// Options configures a Manager.
type Options struct {
	DB           *gorm.DB
	DataDir      string
	BinPath      string
	APIPort      int
	BridgePort   int // loopback port for sing-box (TUIC) traffic, see Input.BridgePort
	AutoDownload bool
	Version      string
	Debounce     time.Duration
	StatsEvery   time.Duration
	// OnlineEvery is how often the users with open connections are read
	// (default 5s).
	OnlineEvery time.Duration
	// OnLine receives every output line (the access log feeds the
	// connection guard).
	OnLine func(line string)
	// Hop receives the Hysteria 2 port-hopping inbounds Xray serves after
	// each config apply (nil = none, also when Xray is switched off or the
	// manager stops), for the nftables redirect (package porthop).
	Hop func(ibs []inbounds.View)
}

// Manager supervises the Xray process.
type Manager struct {
	opt Options

	mu         sync.Mutex
	cmd        *exec.Cmd
	exitCh     chan error
	running    bool
	startedAt  time.Time
	version    string
	lastErr    string
	warnings   []string
	hop        []inbounds.View // port-hopping inbounds of the last config
	lastConfig []byte

	reloadCh chan bool // true = force restart
	ctlCh    chan ctlReq
	stopped  bool // desired state: switched off by an admin
	job      jobState
	jobMu    sync.Mutex // serialises install / geo update jobs
	releases releaseCache
	logs     *ring
	sysUp    atomic.Int64
	sysDown  atomic.Int64
	stats    *StatsClient
	// noOnline: this Xray answered GetAllOnlineUsers with Unimplemented
	// (cleared when Xray starts again).
	noOnline bool
}

// XrayProtocols are the inbound protocols served by Xray-core.
var XrayProtocols = []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria", "wireguard", "http", "mixed", "socks", "tunnel", "tun", "masque"}

// Name returns the engine name.
func (m *Manager) Name() string { return "xray" }

// NewManager creates a manager; call Run to start it.
func NewManager(opt Options) *Manager {
	if opt.Debounce == 0 {
		opt.Debounce = 2 * time.Second
	}
	if opt.StatsEvery == 0 {
		opt.StatsEvery = 10 * time.Second
	}
	if opt.OnlineEvery == 0 {
		opt.OnlineEvery = 5 * time.Second
	}
	return &Manager{opt: opt, reloadCh: make(chan bool, 1), ctlCh: make(chan ctlReq), logs: newRing(2000)}
}

// ConfigPath is where the generated config is written.
func (m *Manager) ConfigPath() string { return filepath.Join(m.opt.DataDir, "xray", "config.json") }

// GenerateConfig renders the config from the DB without applying it.
func (m *Manager) GenerateConfig() ([]byte, error) {
	in, err := LoadMainInput(m.opt.DB, m.opt.DataDir, m.opt.APIPort)
	if err != nil {
		return nil, err
	}
	in.BridgePort = m.opt.BridgePort
	m.mu.Lock()
	in.XrayVersion = m.version
	m.mu.Unlock()
	if in.XrayVersion == "" {
		in.XrayVersion = detectVersion(m.opt.BinPath)
	}
	b, err := Build(in)
	var hop []inbounds.View
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if len(ib.HopRanges()) > 0 && len(inbounds.XrayMissing(ib, in.XrayVersion)) == 0 {
			hop = append(hop, *ib)
		}
	}
	m.mu.Lock()
	m.hop = hop
	m.warnings = in.Skipped
	m.mu.Unlock()
	return b, err
}

// RequestReload schedules a debounced config reload (restart only if the
// generated config changed).
func (m *Manager) RequestReload() {
	select {
	case m.reloadCh <- false:
	default:
	}
}

// Restart forces a restart (debounced).
func (m *Manager) Restart() {
	// Replace a pending soft reload with a forced one.
	select {
	case <-m.reloadCh:
	default:
	}
	select {
	case m.reloadCh <- true:
	default:
	}
}

// Status returns the current process status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Name: "xray", Version: m.version, Running: m.running, Error: m.lastErr, Enabled: true, Protocols: XrayProtocols, Instances: 1, Warnings: append([]string{}, m.warnings...), Stopped: m.stopped}
	if m.job.Running {
		s.Busy = m.job.Kind
	}
	if m.running {
		t := m.startedAt
		s.StartedAt = &t
	}
	return s
}

// Logs returns up to n recent log lines.
func (m *Manager) Logs(n int) []string { return m.logs.last(n) }

// TakeSystemDelta returns and clears system traffic accumulated since the last call.
func (m *Manager) TakeSystemDelta() (up, down int64) {
	return m.sysUp.Swap(0), m.sysDown.Swap(0)
}

// Run supervises Xray until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	m.loadDesiredState()
	if m.version == "" {
		if v := detectVersion(m.opt.BinPath); v != "" {
			m.mu.Lock()
			m.version = v
			m.mu.Unlock()
		}
	}
	if err := m.ensureBinary(ctx); err != nil {
		m.setErr(err.Error())
		slog.Warn("xray binary unavailable; core will not run", "err", err)
	}
	m.apply(true)
	statsT := time.NewTicker(m.opt.StatsEvery)
	defer statsT.Stop()
	onlineT := time.NewTicker(m.opt.OnlineEvery)
	defer onlineT.Stop()
	var (
		debounce  <-chan time.Time
		force     bool
		restartAt <-chan time.Time
	)
	for {
		m.mu.Lock()
		exitCh := m.exitCh
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			// Read the counters one last time so the bytes since the last
			// poll are not lost on shutdown.
			if m.isRunning() {
				m.collect(context.Background())
			}
			m.stop()
			m.setHop(false)
			return
		case f := <-m.reloadCh:
			force = force || f
			debounce = time.After(m.opt.Debounce)
		case <-debounce:
			debounce = nil
			m.apply(force)
			force = false
		case err := <-exitCh:
			m.mu.Lock()
			m.running = false
			m.cmd = nil
			m.exitCh = nil
			msg := "xray exited"
			if err != nil {
				msg = "xray exited: " + err.Error()
			}
			m.lastErr = msg
			m.mu.Unlock()
			slog.Warn(msg + "; restarting in 5s")
			restartAt = time.After(5 * time.Second)
		case <-restartAt:
			restartAt = nil
			// Something else (switch on, update) may have started it meanwhile.
			if !m.isRunning() {
				m.apply(true)
			}
		case <-statsT.C:
			m.collect(ctx)
		case <-onlineT.C:
			m.pollOnline(ctx)
		case req := <-m.ctlCh:
			if !util.Safe("xray control", req.fn) {
				m.setErr("internal error (see the service log)")
			}
			close(req.done)
		}
	}
}

func (m *Manager) setErr(s string) {
	m.mu.Lock()
	m.lastErr = s
	m.mu.Unlock()
}

func (m *Manager) ensureBinary(ctx context.Context) error {
	if _, err := os.Stat(m.opt.BinPath); err == nil {
		return nil
	}
	if !m.opt.AutoDownload {
		return fmt.Errorf("xray binary not found at %s (auto-download disabled)", m.opt.BinPath)
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	dir := filepath.Dir(m.opt.BinPath)
	if err := Download(dctx, dir, m.opt.Version); err != nil {
		return fmt.Errorf("download xray: %w", err)
	}
	if _, err := os.Stat(m.opt.BinPath); err != nil {
		return fmt.Errorf("xray binary not found at %s after download", m.opt.BinPath)
	}
	return nil
}

// apply regenerates the config and (re)starts Xray when needed.
func (m *Manager) apply(force bool) {
	var cfg []byte
	var err error
	if !util.Safe("xray config", func() { cfg, err = m.GenerateConfig() }) {
		err = fmt.Errorf("internal error (see the service log)")
	}
	if err != nil {
		m.setErr("config generation failed: " + err.Error())
		slog.Error("xray config generation failed", "err", err)
		return
	}
	m.mu.Lock()
	same := bytes.Equal(cfg, m.lastConfig) && m.running
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		// Switched off: keep the rendered config on disk for inspection but
		// do not run the process.
		if m.isRunning() {
			m.collect(context.Background())
			m.stop()
		}
		m.mu.Lock()
		m.lastConfig = nil
		m.lastErr = ""
		m.mu.Unlock()
		m.setHop(false)
		return
	}
	m.setHop(true)
	if same && !force {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.ConfigPath()), 0o750); err != nil {
		m.setErr(err.Error())
		return
	}
	if err := os.WriteFile(m.ConfigPath(), cfg, 0o600); err != nil {
		m.setErr(err.Error())
		return
	}
	m.mu.Lock()
	m.lastConfig = cfg
	m.mu.Unlock()
	if _, err := os.Stat(m.opt.BinPath); err != nil {
		m.setErr("xray binary not found at " + m.opt.BinPath)
		return
	}
	// Grab counters right before a restart so no traffic is lost.
	m.collect(context.Background())
	m.stop()
	if err := m.start(); err != nil {
		m.setErr(err.Error())
		slog.Error("xray start failed", "err", err)
	}
}

// setHop hands the port-hopping inbounds of the last config to Options.Hop
// (on = false: none).
func (m *Manager) setHop(on bool) {
	if m.opt.Hop == nil {
		return
	}
	var hop []inbounds.View
	if on {
		m.mu.Lock()
		hop = m.hop
		m.mu.Unlock()
	}
	util.Safe("port hopping", func() { m.opt.Hop(hop) })
}

func (m *Manager) start() error {
	ver := detectVersion(m.opt.BinPath)
	cmd := exec.Command(m.opt.BinPath, "run", "-c", m.ConfigPath())
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(m.opt.BinPath))
	cmd.SysProcAttr = sysProcAttr()
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if m.opt.OnLine != nil {
				// A panic in the hook must not stop this reader: xray
				// would block on a full pipe.
				util.Safe("xray log hook", func() { m.opt.OnLine(line) })
			}
			m.logs.add(line)
		}
	}()
	exitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		exitCh <- err
	}()
	m.mu.Lock()
	m.cmd, m.exitCh, m.running, m.startedAt, m.version, m.lastErr = cmd, exitCh, true, time.Now().UTC(), ver, ""
	m.mu.Unlock()
	m.logs.add(fmt.Sprintf("[veilon] xray %s started (pid %d)", ver, cmd.Process.Pid))
	slog.Info("xray started", "version", ver, "pid", cmd.Process.Pid)
	return nil
}

func (m *Manager) stop() {
	m.mu.Lock()
	cmd, exitCh := m.cmd, m.exitCh
	m.cmd, m.exitCh, m.running = nil, nil, false
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-exitCh:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exitCh
	}
	if m.stats != nil {
		m.stats.Close()
		m.stats = nil
	}
	m.noOnline = false
	// Its connections are gone with it.
	presence.Default.ReportLive(presence.MainXray, nil, time.Now().UTC())
	m.logs.add("[veilon] xray stopped")
}

func detectVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) >= 2 {
		return f[1]
	}
	return ""
}

func (m *Manager) collect(ctx context.Context) {
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if !running {
		return
	}
	if m.stats == nil {
		c, err := NewStatsClient(fmt.Sprintf("127.0.0.1:%d", m.opt.APIPort))
		if err != nil {
			return
		}
		m.stats = c
	}
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stats, err := m.stats.Query(qctx, "", true)
	if err != nil {
		slog.Debug("xray stats query failed", "err", err)
		return
	}
	t := ParseStats(stats)
	m.sysUp.Add(t.SysUp)
	m.sysDown.Add(t.SysDown)
	now := time.Now().UTC()
	presence.Default.Traffic(presence.MainXray, t.Users, now)
	if err := ApplyUserTraffic(m.opt.DB, t.Users, now); err != nil {
		slog.Warn("store user traffic failed", "err", err)
	}
}

// pollOnline reads the users with at least one open connection, so a user
// shows online within seconds of connecting and offline within seconds of
// leaving. Xray versions that do not track connections exactly keep the
// traffic rule (see package presence).
func (m *Manager) pollOnline(ctx context.Context) {
	m.mu.Lock()
	running, ver := m.running, m.version
	m.mu.Unlock()
	if !running || m.noOnline || !TracksConnections(ver) {
		return
	}
	if m.stats == nil {
		c, err := NewStatsClient(fmt.Sprintf("127.0.0.1:%d", m.opt.APIPort))
		if err != nil {
			return
		}
		m.stats = c
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ids, err := m.stats.OnlineUsers(qctx)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			m.noOnline = true
			presence.Default.NotLive(presence.MainXray)
			slog.Info("xray does not list online users; using the traffic rule", "version", ver)
			return
		}
		slog.Debug("xray online query failed", "err", err)
		return
	}
	presence.Default.ReportLive(presence.MainXray, ids, time.Now().UTC())
}

// ApplyUserTraffic adds traffic deltas ({up, down} bytes) to users. Only
// uplink bytes, which the server receives from the client, mark a user as
// online: downlink bytes can still be written for a while to a client that
// has already gone (buffered writes, stale UDP endpoints), so they would keep
// a disconnected user "online". last_online_at is the time of the poll that
// saw the client's bytes.
func ApplyUserTraffic(gdb *gorm.DB, deltas map[int64][2]int64, now time.Time) error {
	return ApplyUserTrafficFrom(gdb, deltas, now, 0)
}

// ApplyUserTrafficFrom is ApplyUserTraffic for traffic carried by a server
// (0 = main server, otherwise a node id): users' counters get it the same
// way, and the history records it under that server.
func ApplyUserTrafficFrom(gdb *gorm.DB, deltas map[int64][2]int64, now time.Time, server int64) error {
	if len(deltas) == 0 {
		return nil
	}
	var addUp, addDown int64
	err := gdb.Transaction(func(tx *gorm.DB) error {
		var err error
		addUp, addDown, err = ApplyUserTrafficTx(tx, deltas, now)
		return err
	})
	if err == nil {
		// History and live rate come from exactly the bytes added to users.
		usage.For(server).Add(addUp, addDown)
	}
	return err
}

// ApplyUserTrafficTx adds the deltas to users inside tx and returns the
// counted bytes added (deleted users' bytes are not counted). The caller
// adds them to the server's usage meter once tx commits.
func ApplyUserTrafficTx(tx *gorm.DB, deltas map[int64][2]int64, now time.Time) (addUp, addDown int64, err error) {
	if len(deltas) == 0 {
		return 0, 0, nil
	}
	ids := make([]int64, 0, len(deltas))
	for id := range deltas {
		ids = append(ids, id)
	}
	// Counted usage = real bytes x the user's traffic multiplier.
	var ms []struct {
		ID                int64
		TrafficMultiplier float64
	}
	tx.Model(&db.User{}).Select("id", "traffic_multiplier").Where("id IN ?", ids).Scan(&ms)
	mult := make(map[int64]float64, len(ms))
	for _, m := range ms {
		mult[m.ID] = m.TrafficMultiplier
	}
	for id, d := range deltas {
		if d[0] <= 0 && d[1] <= 0 {
			continue
		}
		up, down := users.Counted(d[0], mult[id]), users.Counted(d[1], mult[id])
		upd := map[string]any{
			"up":         gorm.Expr("up + ?", up),
			"down":       gorm.Expr("down + ?", down),
			"daily_used": gorm.Expr("daily_used + ?", up+down),
		}
		res := tx.Model(&db.User{}).Where("id = ?", id).Updates(upd)
		if res.Error != nil {
			return 0, 0, res.Error
		}
		if res.RowsAffected > 0 { // a deleted user's bytes are not counted
			addUp += up
			addDown += down
		}
	}
	return addUp, addDown, nil
}

// ErrNotRunning is returned when an operation needs a running core.
var ErrNotRunning = errors.New("xray is not running")

type ring struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newRing(max int) *ring { return &ring{max: max} }

func (r *ring) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, s)
	if len(r.lines) > r.max {
		r.lines = append([]string(nil), r.lines[len(r.lines)-r.max:]...)
	}
}

func (r *ring) last(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n > len(r.lines) {
		n = len(r.lines)
	}
	return append([]string{}, r.lines[len(r.lines)-n:]...)
}
