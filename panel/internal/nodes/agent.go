package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/veilonhq/veilon/panel/internal/cores"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Core runs Xray on the node.
type Core interface {
	// Apply tests cfg and, when it is valid, (re)starts Xray with it. An
	// invalid config leaves the running one untouched.
	Apply(ctx context.Context, cfg []byte) error
	// Status reports whether Xray runs, its version and its last error.
	Status() (running bool, version, lastErr string)
	// Collect reads and resets Xray's traffic counters.
	Collect(ctx context.Context) (xray.Traffic, error)
	Stop()
}

// OnlineCore is a Core that lists the users with open connections.
type OnlineCore interface {
	// Online returns the users with at least one open connection, or
	// ErrOnlineUnsupported when this Xray cannot tell.
	Online(ctx context.Context) ([]int64, error)
}

// ErrOnlineUnsupported: the node's Xray does not list online users.
var ErrOnlineUnsupported = errors.New("this Xray does not list online users")

// Agent is the node side ("veilon node serve").
type Agent struct {
	Enrollment *Enrollment
	Dir        string // state: config.json, config.hash
	Core       Core
	APIPort    int
	Version    string
	// StatsEvery is how often Xray's counters are read (default 10s).
	StatsEvery time.Duration
	// IPs collects client IPs from Xray's access log (nil = not reported).
	IPs *guard.IPCollector
	// Firewall drops the IPs the master bans (nil = bans are ignored).
	Firewall *guard.NodeFirewall
	// Shaper applies the master's speed limits (nil = ignored).
	Shaper interface{ ApplyShapes([]guard.Shape) }
	// Updater updates the agent on the master's request (nil = the agent
	// does not announce CapUpdate).
	Updater *SelfUpdater
	// Arch overrides the reported CPU architecture (tests; "" = GOARCH).
	Arch string
	// Engines runs the AmneziaWG and MTProto inbounds (nil = the agent
	// does not announce CapAWG / CapMTProto).
	Engines *cores.NodeEngines

	mu          sync.Mutex
	hash        string
	enginesHash string
	xrayPorts   []int
	enginePorts []int
	pending     StatsReply
	unacked     *StatsReply
	boot        string
	batch       int
	started     time.Time
	applyMu     sync.Mutex
}

func (a *Agent) init() {
	if a.boot == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		a.boot = hex.EncodeToString(b)
		a.started = time.Now().UTC()
		a.pending.Users = map[int64][2]int64{}
	}
	if a.StatsEvery == 0 {
		a.StatsEvery = 10 * time.Second
	}
	if a.APIPort == 0 {
		a.APIPort = DefaultAPIPort
	}
	if a.Engines != nil {
		if a.Engines.Traffic == nil {
			a.Engines.Traffic = a.engineTraffic
		}
		if a.Engines.SeenIP == nil && a.IPs != nil {
			a.Engines.SeenIP = a.IPs.Observe
		}
	}
}

// Restore starts Xray with the last config the master pushed, so a node
// that reboots serves its users before the master reaches it again.
func (a *Agent) Restore(ctx context.Context) {
	a.init()
	a.loadStats()
	a.restoreEngines()
	cfg, err := os.ReadFile(filepath.Join(a.Dir, "config.json"))
	if err != nil {
		return
	}
	if err := a.Core.Apply(ctx, cfg); err != nil {
		slog.Warn("saved node config does not start", "err", err)
		return
	}
	a.mu.Lock()
	a.hash = Hash(cfg)
	a.mu.Unlock()
	a.setPorts(cfg)
}

// restoreEngines runs the engines spec the master pushed last.
func (a *Agent) restoreEngines() {
	if a.Engines == nil {
		return
	}
	b, err := os.ReadFile(a.enginesFile())
	if err != nil {
		return
	}
	var spec cores.NodeSpec
	if err := json.Unmarshal(b, &spec); err != nil || spec.Validate() != nil {
		slog.Warn("saved engines spec is not valid; waiting for the master", "err", err)
		return
	}
	a.Engines.Set(spec)
	a.mu.Lock()
	a.enginesHash = Hash(b)
	a.enginePorts = spec.Ports()
	a.mu.Unlock()
	a.applyPorts()
}

func (a *Agent) enginesFile() string { return filepath.Join(a.Dir, "engines.json") }

// engineTraffic adds the engines' traffic to the pending batch.
func (a *Agent) engineTraffic(users map[int64][2]int64, up, down int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending.Users == nil {
		a.pending.Users = map[int64][2]int64{}
	}
	if a.pending.Engines == nil {
		a.pending.Engines = map[int64][2]int64{}
	}
	for id, v := range users {
		p := a.pending.Users[id]
		a.pending.Users[id] = [2]int64{p[0] + v[0], p[1] + v[1]}
		e := a.pending.Engines[id]
		a.pending.Engines[id] = [2]int64{e[0] + v[0], e[1] + v[1]}
	}
	a.pending.Up += up
	a.pending.Down += down
}

// setPorts tells the firewall which ports the config's inbounds use.
func (a *Agent) setPorts(cfg []byte) {
	if a.Firewall == nil {
		return
	}
	var c struct {
		Inbounds []struct {
			Tag  string `json:"tag"`
			Port any    `json:"port"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(cfg, &c) != nil {
		return
	}
	ports := []int{}
	for _, ib := range c.Inbounds {
		if ib.Tag == "api" {
			continue
		}
		switch v := ib.Port.(type) {
		case float64:
			if v >= 1 && v <= 65535 {
				ports = append(ports, int(v))
			}
		case string:
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 65535 {
				ports = append(ports, n)
			}
		}
	}
	a.mu.Lock()
	a.xrayPorts = ports
	a.mu.Unlock()
	a.applyPorts()
}

// applyPorts gives the firewall the ports of Xray's and the engines'
// inbounds.
func (a *Agent) applyPorts() {
	if a.Firewall == nil {
		return
	}
	a.mu.Lock()
	ports := append(append([]int{}, a.xrayPorts...), a.enginePorts...)
	a.mu.Unlock()
	a.Firewall.SetPorts(ports)
}

// Serve listens with mutual TLS until ctx ends.
func (a *Agent) Serve(ctx context.Context, listen string) error {
	a.init()
	tc, err := ServerTLS(a.Enrollment)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: a.Handler(), TLSConfig: tc, ErrorLog: log.New(&serverErrLog{}, "", 0), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	go a.statsLoop(ctx)
	var engDone chan struct{}
	if a.Engines != nil {
		engDone = make(chan struct{})
		go func() { defer close(engDone); a.Engines.Run(ctx) }()
	}
	slog.Info("veilon node listening", "addr", listen, "node", a.Enrollment.ID)
	err = hs.ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	a.collect(context.Background())
	a.Core.Stop()
	if engDone != nil {
		// The engines read their last traffic as they stop.
		select {
		case <-engDone:
		case <-time.After(15 * time.Second):
		}
	}
	a.saveStats()
	return err
}

// statsFile keeps traffic the master has not stored yet across a node
// restart (an update or a reboot), so it is neither lost nor counted twice:
// an unacknowledged batch keeps its id.
func (a *Agent) statsFile() string { return filepath.Join(a.Dir, "stats-pending.json") }

type savedStats struct {
	Pending StatsReply  `json:"pending"`
	Unacked *StatsReply `json:"unacked"`
}

func (a *Agent) saveStats() {
	a.mu.Lock()
	s := savedStats{Pending: a.pending, Unacked: a.unacked}
	a.mu.Unlock()
	if s.Unacked == nil && len(s.Pending.Users) == 0 && s.Pending.Up == 0 && s.Pending.Down == 0 && len(s.Pending.Engines) == 0 {
		_ = os.Remove(a.statsFile())
		return
	}
	b, err := json.Marshal(s)
	if err != nil || os.MkdirAll(a.Dir, 0o700) != nil {
		return
	}
	tmp := a.statsFile() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, a.statsFile())
	}
}

func (a *Agent) loadStats() {
	b, err := os.ReadFile(a.statsFile())
	if err != nil {
		return
	}
	_ = os.Remove(a.statsFile())
	var s savedStats
	if json.Unmarshal(b, &s) != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.Unacked != nil && s.Unacked.Seq != "" {
		u := *s.Unacked
		if u.Users == nil {
			u.Users = map[int64][2]int64{}
		}
		a.unacked = &u
	}
	for id, v := range s.Pending.Users {
		p := a.pending.Users[id]
		a.pending.Users[id] = [2]int64{p[0] + v[0], p[1] + v[1]}
	}
	for id, v := range s.Pending.Engines {
		if a.pending.Engines == nil {
			a.pending.Engines = map[int64][2]int64{}
		}
		p := a.pending.Engines[id]
		a.pending.Engines[id] = [2]int64{p[0] + v[0], p[1] + v[1]}
	}
	a.pending.Up += s.Pending.Up
	a.pending.Down += s.Pending.Down
}

// serverErrLog takes the agent's http.Server errors. The agent port is open
// to the internet, so port scanners and "veilon node status" (which only
// checks that the port answers) end in failed handshakes all day; those are
// dropped. A handshake refused for its certificate (a wrong or old master)
// is worth seeing, at most once a minute so a retrying client cannot flood
// the journal. Everything else is logged as it comes.
type serverErrLog struct {
	mu   sync.Mutex
	last time.Time
	now  func() time.Time
}

func (l *serverErrLog) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if !strings.Contains(msg, "TLS handshake error") {
		slog.Warn(msg)
		return len(p), nil
	}
	if !strings.Contains(msg, "failed to verify certificate") {
		return len(p), nil
	}
	now := time.Now
	if l.now != nil {
		now = l.now
	}
	l.mu.Lock()
	t := now()
	show := l.last.IsZero() || t.Sub(l.last) >= time.Minute
	if show {
		l.last = t
	}
	l.mu.Unlock()
	if show {
		slog.Warn("refused a client certificate (repeats are hidden for a minute)", "err", msg)
	}
	return len(p), nil
}

func (a *Agent) statsLoop(ctx context.Context) {
	t := time.NewTicker(a.StatsEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			util.Safe("node stats", func() { a.collect(ctx) })
		}
	}
}

// collect moves Xray's counters into the pending batch.
func (a *Agent) collect(ctx context.Context) {
	if running, _, _ := a.Core.Status(); !running {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	t, err := a.Core.Collect(cctx)
	if err != nil {
		slog.Debug("node stats read failed", "err", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, v := range t.Users {
		p := a.pending.Users[id]
		a.pending.Users[id] = [2]int64{p[0] + v[0], p[1] + v[1]}
	}
	a.pending.Up += t.SysUp
	a.pending.Down += t.SysDown
}

// Handler serves the agent API (behind mutual TLS).
func (a *Agent) Handler() http.Handler {
	a.init()
	m := http.NewServeMux()
	m.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) { writeJ(w, 200, a.info()) })
	m.HandleFunc("PUT /v1/config", a.putConfig)
	m.HandleFunc("PUT /v1/engines", a.putEngines)
	m.HandleFunc("GET /v1/stats", a.stats)
	m.HandleFunc("PUT /v1/bans", a.putBans)
	m.HandleFunc("GET /v1/online", a.online)
	m.HandleFunc("POST /v1/update", a.postUpdate)
	m.HandleFunc("GET /v1/update", func(w http.ResponseWriter, r *http.Request) { writeJ(w, 200, a.updateStatus()) })
	if a.Updater == nil {
		return m
	}
	// Every request here passed mutual TLS, so it came from the master: a
	// freshly updated agent takes it as the confirmation it reconnected.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.Updater.Confirm()
		m.ServeHTTP(w, r)
	})
}

func (a *Agent) updateStatus() *UpdateStatus {
	if a.Updater == nil {
		return nil
	}
	return ReadUpdateStatus(a.Dir)
}

// postUpdate starts a self-update (202) or says why not.
func (a *Agent) postUpdate(w http.ResponseWriter, r *http.Request) {
	if a.Updater == nil {
		writeJ(w, 404, map[string]string{"error": "this agent does not update itself"})
		return
	}
	var req UpdateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJ(w, 400, map[string]string{"error": "bad request: " + err.Error()})
		return
	}
	if err := a.Updater.Start(req); err != nil {
		code := 400
		if errors.Is(err, ErrUpdateBusy) {
			code = 409
		}
		writeJ(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJ(w, 202, a.updateStatus())
}

func (a *Agent) info() Info {
	running, ver, lastErr := a.Core.Status()
	a.mu.Lock()
	hash, ehash := a.hash, a.enginesHash
	a.mu.Unlock()
	in := Info{NodeID: a.Enrollment.ID, Version: a.Version, XrayVersion: ver, XrayRunning: running, XrayError: lastErr,
		ConfigHash: hash, APIPort: a.APIPort, StartedAt: a.started, CPUs: runtime.NumCPU(), Arch: runtime.GOARCH}
	if a.Arch != "" {
		in.Arch = a.Arch
	}
	if a.Updater != nil {
		in.Caps = append(in.Caps, CapUpdate)
		in.Update = a.updateStatus()
	}
	if _, ok := a.Core.(OnlineCore); ok {
		in.Caps = append(in.Caps, CapOnline)
	}
	if a.Engines != nil {
		in.Caps = append(in.Caps, CapAWG, CapMTProto)
		in.EnginesHash = ehash
		in.EngineErrors = a.Engines.Errors()
	}
	in.Load1, in.MemUsed = hostLoad()
	return in
}

// online lists the users with open connections (501 when this Xray cannot
// tell, 503 when it is not running).
func (a *Agent) online(w http.ResponseWriter, r *http.Request) {
	oc, ok := a.Core.(OnlineCore)
	if !ok {
		writeJ(w, 501, map[string]string{"error": ErrOnlineUnsupported.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	ids, err := oc.Online(ctx)
	switch {
	case errors.Is(err, ErrOnlineUnsupported):
		writeJ(w, 501, map[string]string{"error": err.Error()})
	case err != nil:
		writeJ(w, 503, map[string]string{"error": err.Error()})
	default:
		if ids == nil {
			ids = []int64{}
		}
		rep := OnlineReply{Users: ids, Now: time.Now().UTC()}
		if a.IPs != nil {
			rep.IPs = a.IPs.Fresh()
		}
		writeJ(w, 200, rep)
	}
}

func (a *Agent) putConfig(w http.ResponseWriter, r *http.Request) {
	var p ConfigPush
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&p); err != nil {
		writeJ(w, 400, map[string]string{"error": "bad request: " + err.Error()})
		return
	}
	if !json.Valid(p.Config) || Hash(p.Config) != p.Hash {
		writeJ(w, 400, map[string]string{"error": "config does not match its hash"})
		return
	}
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	a.collect(r.Context()) // keep the counters of the process about to restart
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := a.Core.Apply(ctx, p.Config); err != nil {
		writeJ(w, 422, map[string]string{"error": err.Error()})
		return
	}
	if err := os.MkdirAll(a.Dir, 0o700); err == nil {
		tmp := filepath.Join(a.Dir, ".config.json")
		if os.WriteFile(tmp, p.Config, 0o600) == nil {
			_ = os.Rename(tmp, filepath.Join(a.Dir, "config.json"))
		}
	}
	a.mu.Lock()
	a.hash = p.Hash
	a.mu.Unlock()
	a.setPorts(p.Config)
	slog.Info("node config applied", "hash", p.Hash[:12])
	writeJ(w, 200, a.info())
}

// putEngines replaces the AmneziaWG and MTProto inbounds the node runs.
// The engines apply it a moment later; their errors show in /v1/info.
func (a *Agent) putEngines(w http.ResponseWriter, r *http.Request) {
	if a.Engines == nil {
		writeJ(w, 404, map[string]string{"error": "this agent does not run AmneziaWG or MTProto inbounds"})
		return
	}
	var p EnginesPush
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&p); err != nil {
		writeJ(w, 400, map[string]string{"error": "bad request: " + err.Error()})
		return
	}
	if !json.Valid(p.Spec) || Hash(p.Spec) != p.Hash {
		writeJ(w, 400, map[string]string{"error": "engines spec does not match its hash"})
		return
	}
	var spec cores.NodeSpec
	if err := json.Unmarshal(p.Spec, &spec); err != nil {
		writeJ(w, 400, map[string]string{"error": "bad engines spec: " + err.Error()})
		return
	}
	if err := spec.Validate(); err != nil {
		writeJ(w, 422, map[string]string{"error": err.Error()})
		return
	}
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	if err := os.MkdirAll(a.Dir, 0o700); err == nil {
		tmp := a.enginesFile() + ".tmp"
		if os.WriteFile(tmp, p.Spec, 0o600) == nil {
			_ = os.Rename(tmp, a.enginesFile())
		}
	}
	a.Engines.Set(spec)
	a.mu.Lock()
	a.enginesHash = p.Hash
	a.enginePorts = spec.Ports()
	a.mu.Unlock()
	a.applyPorts()
	slog.Info("node engines applied", "hash", p.Hash[:12], "amneziawg", len(spec.AWG), "mtproto", len(spec.MTG))
	writeJ(w, 200, a.info())
}

func (a *Agent) stats(w http.ResponseWriter, r *http.Request) {
	ack := r.URL.Query().Get("ack")
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.unacked != nil && ack == a.unacked.Seq {
		a.unacked = nil
	}
	if a.unacked == nil {
		a.batch++
		b := a.pending
		b.Seq = a.boot + ":" + strconv.Itoa(a.batch)
		if a.IPs != nil {
			b.IPs = a.IPs.Drain()
		}
		a.unacked = &b
		a.pending = StatsReply{Users: map[int64][2]int64{}}
	}
	a.unacked.Now = time.Now().UTC()
	writeJ(w, 200, a.unacked)
}

// putBans drops the IPs the master banned for the IP limit.
func (a *Agent) putBans(w http.ResponseWriter, r *http.Request) {
	var p BansPush
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&p); err != nil {
		writeJ(w, 400, map[string]string{"error": "bad request: " + err.Error()})
		return
	}
	if a.Shaper != nil && p.Shapes != nil {
		a.Shaper.ApplyShapes(p.Shapes)
	}
	if a.Firewall == nil {
		writeJ(w, 200, map[string]bool{"ok": false})
		return
	}
	if err := a.Firewall.Apply(p.Bans); err != nil {
		writeJ(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJ(w, 200, map[string]bool{"ok": true})
}

func writeJ(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// hostLoad reads the 1-minute load average and the used memory share.
func hostLoad() (load float64, mem float64) {
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			load, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail float64
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseFloat(f[1], 64)
			switch f[0] {
			case "MemTotal:":
				total = v
			case "MemAvailable:":
				avail = v
			}
		}
		if total > 0 {
			mem = (total - avail) / total
		}
	}
	return
}

// errorf keeps error text short enough for the UI.
func errorf(format string, a ...any) error {
	s := fmt.Sprintf(format, a...)
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return errors.New(s)
}
