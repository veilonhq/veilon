// Package cores runs the proxy engines next to Xray-core: sing-box (TUIC),
// mtg-multi (MTProto) and the built-in AmneziaWG sidecar. Each engine is a
// Supervisor that renders its config from the database, starts one or more
// processes, restarts them (debounced) when the config changes, keeps their
// logs and feeds per-user traffic into the database.
package cores

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/logbuf"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Instance is one process an engine wants running.
type Instance struct {
	// Key identifies the instance across plans ("main", "inbound-3").
	Key string
	// Config is written to ConfigFile before the process starts or reloads.
	Config     []byte
	ConfigFile string
	// Args are the process arguments (Bin is argv[0]).
	Args []string
	Env  []string
	// ReloadKey: when the running instance has the same ReloadKey and only
	// Config changed, the engine's ReloadSignal is sent instead of a restart.
	ReloadKey string
	// Meta is engine data handed back to Collect (e.g. a stats port).
	Meta any
}

// Spec describes an engine.
type Spec struct {
	Name      string
	Protocols []string
	// Bin returns the executable path.
	Bin func() string
	// Ensure makes the binary available (download); called before the first
	// start while the engine has work.
	Ensure func(ctx context.Context) error
	// Version reports the binary version ("" when unknown).
	Version func() string
	// Plan renders the desired instances. An empty plan stops the engine.
	Plan func() ([]Instance, error)
	// ReloadSignal enables hot reloads (0 = always restart).
	ReloadSignal syscall.Signal
	// Collect is called every StatsEvery with the running instances; started
	// maps instance keys to their start generation so counters that reset
	// with a restart can be detected.
	Collect func(ctx context.Context, running []Running)
	// LineHook may consume a process output line (return true to keep it
	// out of the log).
	LineHook   func(key string, gen uint64, line string) bool
	StatsEvery time.Duration
	Debounce   time.Duration
	// ConfigGlob matches the config files this engine writes; files it
	// matches that no planned instance uses (deleted inbounds) are removed.
	ConfigGlob string
	// DB stores the admin's on/off choice (setting StoppedKey(Name)); nil
	// keeps it in memory only.
	DB *gorm.DB
}

// StoppedKey is the setting that persists an engine's on/off choice ("1" =
// switched off). The "_" prefix keeps it out of the public settings API.
func StoppedKey(name string) string { return "_" + name + "_stopped" }

// Running describes a live instance for Collect.
type Running struct {
	Key  string
	Gen  uint64
	Meta any
}

type proc struct {
	inst    Instance
	cmd     *exec.Cmd
	gen     uint64
	started time.Time
	running bool
	fails   int
	done    chan struct{}
	lastOut atomic.Value // last output line (string)
}

type exitEvent struct {
	key string
	gen uint64
	err error
}

// Supervisor runs an engine.
type Supervisor struct {
	spec Spec

	mu       sync.Mutex
	procs    map[string]*proc
	lastErr  string
	enabled  bool
	version  string
	ensured  bool
	gen      uint64
	reloadCh chan bool
	powerCh  chan chan struct{}
	stopped  bool // desired state: switched off by an admin
	exits    chan exitEvent
	logs     *logbuf.Ring
	sysUp    atomic.Int64
	sysDown  atomic.Int64
}

// NewSupervisor creates a supervisor; call Run to start it.
func NewSupervisor(spec Spec) *Supervisor {
	if spec.Debounce == 0 {
		spec.Debounce = 2 * time.Second
	}
	if spec.StatsEvery == 0 {
		spec.StatsEvery = 10 * time.Second
	}
	return &Supervisor{spec: spec, procs: map[string]*proc{}, reloadCh: make(chan bool, 1), powerCh: make(chan chan struct{}), exits: make(chan exitEvent, 16), logs: logbuf.New(spec.Name, 2000)}
}

// Name returns the engine name.
func (s *Supervisor) Name() string { return s.spec.Name }

// AddSystem records system (server-wide) traffic.
func (s *Supervisor) AddSystem(up, down int64) {
	s.sysUp.Add(up)
	s.sysDown.Add(down)
}

// TakeSystemDelta returns and clears accumulated system traffic.
func (s *Supervisor) TakeSystemDelta() (int64, int64) { return s.sysUp.Swap(0), s.sysDown.Swap(0) }

// RequestReload schedules a debounced reload (only changed instances restart).
func (s *Supervisor) RequestReload() {
	select {
	case s.reloadCh <- false:
	default:
	}
}

// Restart forces all instances to restart (debounced).
func (s *Supervisor) Restart() {
	select {
	case <-s.reloadCh:
	default:
	}
	select {
	case s.reloadCh <- true:
	default:
	}
}

// Logs returns recent output lines.
func (s *Supervisor) Logs(n int) []string { return s.logs.Lines(n) }

// LogEntries returns recent output lines with the time each was written.
func (s *Supervisor) LogEntries(n int) []logbuf.Entry { return s.logs.Last(n) }

// GenerateConfig renders the current configuration of every instance.
func (s *Supervisor) GenerateConfig() ([]byte, error) {
	plan, err := s.plan()
	if err != nil {
		return nil, err
	}
	if len(plan) == 1 {
		return plan[0].Config, nil
	}
	var b bytes.Buffer
	for i, in := range plan {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "# ---- %s (%s)\n", in.Key, filepath.Base(in.ConfigFile))
		b.Write(in.Config)
		if len(in.Config) > 0 && in.Config[len(in.Config)-1] != '\n' {
			b.WriteString("\n")
		}
	}
	return b.Bytes(), nil
}

// SetPower switches the engine on or off. The choice is stored, so a stopped
// engine stays stopped after a panel restart and through Restart.
func (s *Supervisor) SetPower(ctx context.Context, on bool) error {
	if s.spec.DB != nil {
		v := "0"
		if !on {
			v = "1"
		}
		if err := db.SetSetting(s.spec.DB, StoppedKey(s.spec.Name), v); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.stopped = !on
	s.mu.Unlock()
	if on {
		s.logs.Add(fmt.Sprintf("[veilon] %s switched on", s.spec.Name))
	} else {
		s.logs.Add(fmt.Sprintf("[veilon] %s switched off", s.spec.Name))
	}
	done := make(chan struct{})
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	select {
	case s.powerCh <- done:
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return fmt.Errorf("%s supervisor is not running", s.spec.Name)
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stopped reports whether an admin switched the engine off.
func (s *Supervisor) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Supervisor) loadDesiredState() {
	if s.spec.DB == nil {
		return
	}
	off := db.GetSetting(s.spec.DB, StoppedKey(s.spec.Name)) == "1"
	s.mu.Lock()
	s.stopped = off
	s.mu.Unlock()
	if off {
		s.logs.Add(fmt.Sprintf("[veilon] %s is switched off; it will not be started", s.spec.Name))
	}
}

// Status reports the engine state.
func (s *Supervisor) Status() xray.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := xray.Status{Name: s.spec.Name, Version: s.version, Error: s.lastErr, Enabled: s.enabled, Protocols: s.spec.Protocols, Instances: len(s.procs), Warnings: []string{}, Stopped: s.stopped}
	if len(s.procs) > 0 {
		all := true
		var first time.Time
		for _, p := range s.procs {
			if !p.running {
				all = false
				continue
			}
			if first.IsZero() || p.started.Before(first) {
				first = p.started
			}
		}
		st.Running = all
		if all {
			st.StartedAt = &first
		}
	}
	return st
}

func (s *Supervisor) setErr(e string) {
	s.mu.Lock()
	s.lastErr = e
	s.mu.Unlock()
}

// Run supervises the engine until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) {
	s.loadDesiredState()
	s.apply(ctx, false)
	statsT := time.NewTicker(s.spec.StatsEvery)
	defer statsT.Stop()
	var debounce, retry <-chan time.Time
	force := false
	for {
		select {
		case <-ctx.Done():
			s.collect(context.Background())
			s.stopAll()
			return
		case f := <-s.reloadCh:
			force = force || f
			debounce = time.After(s.spec.Debounce)
		case done := <-s.powerCh:
			// Apply at once; a pending debounced reload is folded in.
			debounce, retry = nil, nil
			s.apply(ctx, force)
			force = false
			close(done)
		case <-debounce:
			debounce = nil
			s.apply(ctx, force)
			force = false
		case ev := <-s.exits:
			s.mu.Lock()
			p, ok := s.procs[ev.key]
			if ok && p.gen == ev.gen {
				p.running = false
				p.cmd = nil
				p.fails++
				msg := s.spec.Name + " exited"
				if ev.err != nil {
					msg += ": " + ev.err.Error()
				}
				if lo, _ := p.lastOut.Load().(string); lo != "" {
					msg += " (" + lo + ")"
				}
				s.lastErr = msg
				// Back off up to a minute for processes that keep failing.
				d := time.Duration(p.fails*5) * time.Second
				if d > time.Minute {
					d = time.Minute
				}
				s.logs.Add(fmt.Sprintf("[veilon] %s %s; restarting in %s", ev.key, msg, d))
				retry = time.After(d)
			}
			s.mu.Unlock()
		case <-retry:
			retry = nil
			s.apply(ctx, false)
		case <-statsT.C:
			s.collect(ctx)
		}
	}
}

func (s *Supervisor) running() []Running {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Running, 0, len(s.procs))
	for _, p := range s.procs {
		if p.running {
			out = append(out, Running{Key: p.inst.Key, Gen: p.gen, Meta: p.inst.Meta})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *Supervisor) collect(ctx context.Context) {
	if s.spec.Collect == nil {
		return
	}
	r := s.running()
	if len(r) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	util.Safe(s.spec.Name+" stats", func() { s.spec.Collect(cctx, r) })
}

// plan runs the engine's Plan; a panic there becomes an error so a bad row
// only stops this engine's config, not the panel.
func (s *Supervisor) plan() (plan []Instance, err error) {
	if !util.Safe(s.spec.Name+" config", func() { plan, err = s.spec.Plan() }) {
		return nil, fmt.Errorf("internal error while generating the config (see the service log)")
	}
	return plan, err
}

func (s *Supervisor) apply(ctx context.Context, force bool) {
	plan, err := s.plan()
	if err != nil {
		s.setErr("config generation failed: " + err.Error())
		slog.Error("engine config generation failed", "engine", s.spec.Name, "err", err)
		return
	}
	want := map[string]Instance{}
	for _, in := range plan {
		want[in.Key] = in
	}
	s.mu.Lock()
	s.enabled = len(plan) > 0
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		// Switched off: stop everything but keep the config files.
		s.collect(ctx)
		s.stopAll()
		s.mu.Lock()
		s.procs = map[string]*proc{}
		s.lastErr = ""
		s.mu.Unlock()
		return
	}
	// Grab counters before anything restarts.
	s.collect(ctx)
	// Stop instances that are no longer wanted.
	s.mu.Lock()
	var gone []*proc
	for k, p := range s.procs {
		if _, ok := want[k]; !ok {
			gone = append(gone, p)
			delete(s.procs, k)
		}
	}
	s.mu.Unlock()
	for _, p := range gone {
		s.stopProc(p)
	}
	s.removeStale(want)
	if len(plan) == 0 {
		s.setErr("")
		return
	}
	if !s.ensured {
		if s.spec.Ensure != nil {
			ectx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			err := s.spec.Ensure(ectx)
			cancel()
			if err != nil {
				s.setErr(err.Error())
				slog.Warn("engine binary unavailable", "engine", s.spec.Name, "err", err)
				return
			}
		}
		s.ensured = true
		if s.spec.Version != nil {
			v := s.spec.Version()
			s.mu.Lock()
			s.version = v
			s.mu.Unlock()
		}
	}
	errs := []string{}
	for _, key := range sortedKeys(want) {
		in := want[key]
		s.mu.Lock()
		p := s.procs[key]
		s.mu.Unlock()
		changed := p == nil || !bytes.Equal(p.inst.Config, in.Config) || strings.Join(p.inst.Args, "\x00") != strings.Join(in.Args, "\x00")
		if p != nil && p.running && !changed && !force {
			continue
		}
		if err := writeAtomic(in.ConfigFile, in.Config); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if p != nil && p.running && !force && s.spec.ReloadSignal != 0 && p.inst.ReloadKey == in.ReloadKey &&
			strings.Join(p.inst.Args, "\x00") == strings.Join(in.Args, "\x00") && p.cmd != nil {
			if err := p.cmd.Process.Signal(s.spec.ReloadSignal); err == nil {
				s.mu.Lock()
				p.inst = in
				s.mu.Unlock()
				s.logs.Add(fmt.Sprintf("[veilon] %s reloaded", key))
				continue
			}
		}
		if p != nil {
			s.stopProc(p)
		}
		np := &proc{inst: in}
		if p != nil {
			np.fails = p.fails
		}
		if err := s.startProc(np); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", key, err))
		}
		s.mu.Lock()
		s.procs[key] = np
		s.mu.Unlock()
	}
	s.setErr(strings.Join(errs, "; "))
}

// removeStale deletes config files matching ConfigGlob that no wanted
// instance writes.
func (s *Supervisor) removeStale(want map[string]Instance) {
	if s.spec.ConfigGlob == "" {
		return
	}
	files, _ := filepath.Glob(s.spec.ConfigGlob)
	keep := map[string]bool{}
	for _, in := range want {
		keep[filepath.Clean(in.ConfigFile)] = true
	}
	for _, f := range files {
		if !keep[filepath.Clean(f)] {
			if err := os.Remove(f); err == nil {
				slog.Info("removed stale engine config", "engine", s.spec.Name, "file", f)
			}
		}
	}
}

func sortedKeys(m map[string]Instance) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Supervisor) startProc(p *proc) error {
	bin := s.spec.Bin()
	cmd := exec.Command(bin, p.inst.Args...)
	cmd.Env = append(os.Environ(), p.inst.Env...)
	cmd.Dir = filepath.Dir(p.inst.ConfigFile)
	cmd.SysProcAttr = sysProcAttr()
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	done := make(chan struct{})
	s.mu.Lock()
	s.gen++
	p.gen, p.cmd, p.running, p.started, p.done = s.gen, cmd, true, time.Now().UTC(), done
	gen := p.gen
	s.mu.Unlock()
	key := p.inst.Key
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			// Engines such as sing-box color their output even into a
			// pipe; escapes are noise in the panel's log view.
			line := guard.StripANSI(sc.Text())
			if s.spec.LineHook != nil {
				// A panic in the hook must not stop this reader: the
				// engine would block on a full pipe.
				consumed := false
				util.Safe(s.spec.Name+" log hook", func() { consumed = s.spec.LineHook(key, gen, line) })
				if consumed {
					continue
				}
			}
			if strings.TrimSpace(line) != "" {
				p.lastOut.Store(line)
			}
			tag := ""
			if key != "main" {
				tag = key
			}
			s.logs.AddTagged(tag, line)
		}
	}()
	go func() {
		err := cmd.Wait()
		pw.Close()
		close(done)
		go func() { s.exits <- exitEvent{key: key, gen: gen, err: err} }()
	}()
	s.logs.Add(fmt.Sprintf("[veilon] %s %s started (pid %d)", s.spec.Name, key, cmd.Process.Pid))
	slog.Info("engine started", "engine", s.spec.Name, "instance", key, "pid", cmd.Process.Pid)
	return nil
}

func (s *Supervisor) stopProc(p *proc) {
	s.mu.Lock()
	cmd, done := p.cmd, p.done
	p.cmd, p.running = nil, false
	p.gen = 0 // ignore the exit event of this process
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	s.logs.Add(fmt.Sprintf("[veilon] %s %s stopped", s.spec.Name, p.inst.Key))
}

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	ps := make([]*proc, 0, len(s.procs))
	for _, p := range s.procs {
		ps = append(ps, p)
	}
	s.mu.Unlock()
	for _, p := range ps {
		s.stopProc(p)
	}
}
