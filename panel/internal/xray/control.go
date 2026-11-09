package xray

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// KeyStopped persists the admin's on/off choice for Xray-core ("1" = off).
// The "_" prefix keeps it out of the public settings API.
const KeyStopped = "_xray_stopped"

var (
	// ErrBusy is returned while another install / geo update is running.
	ErrBusy = errors.New("another core update is already running")
	// ErrNotSupervised is returned when the supervisor loop is not running.
	ErrNotSupervised = errors.New("xray supervisor is not running")
	// ErrBadVersion is returned for a malformed release tag.
	ErrBadVersion = errors.New("version must be a release tag such as v25.10.15")
)

var tagRe = regexp.MustCompile(`^v\d+\.\d+(\.\d+)?(-[A-Za-z0-9.]+)?$`)

// verifyWindow is how long a freshly (re)started Xray must stay up after an
// install or geo update before the change is kept.
var verifyWindow = 4 * time.Second

type ctlReq struct {
	fn   func()
	done chan struct{}
}

// JobState describes the last (or current) maintenance job.
type JobState struct {
	Kind       string     `json:"kind"` // "install" | "geo"
	Version    string     `json:"version,omitempty"`
	Running    bool       `json:"running"`
	Error      string     `json:"error,omitempty"`
	RolledBack bool       `json:"rolled_back,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type jobState = JobState

func (m *Manager) loadDesiredState() {
	if m.opt.DB == nil {
		return
	}
	off := db.GetSetting(m.opt.DB, KeyStopped) == "1"
	m.mu.Lock()
	m.stopped = off
	m.mu.Unlock()
	if off {
		m.logs.add("[veilon] xray is switched off; it will not be started")
	}
}

func (m *Manager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// exec runs fn on the supervisor goroutine (so it never races the process
// lifecycle) and waits for it.
func (m *Manager) exec(ctx context.Context, fn func()) error {
	req := ctlReq{fn: fn, done: make(chan struct{})}
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	select {
	case m.ctlCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ErrNotSupervised
	}
	select {
	case <-req.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetPower switches Xray-core on or off. The choice is stored, so a stopped
// core stays stopped after a panel restart.
func (m *Manager) SetPower(ctx context.Context, on bool) error {
	v := "0"
	if !on {
		v = "1"
	}
	if m.opt.DB != nil {
		if err := db.SetSetting(m.opt.DB, KeyStopped, v); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.stopped = !on
	m.mu.Unlock()
	if on {
		m.logs.add("[veilon] xray switched on")
	} else {
		m.logs.add("[veilon] xray switched off")
	}
	return m.exec(ctx, func() { m.apply(true) })
}

// Job returns the current or last maintenance job (nil if none ran yet).
func (m *Manager) Job() *JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job.Kind == "" {
		return nil
	}
	j := m.job
	return &j
}

func (m *Manager) beginJob(kind, version string) bool {
	if !m.jobMu.TryLock() {
		return false
	}
	m.mu.Lock()
	m.job = JobState{Kind: kind, Version: version, Running: true, StartedAt: time.Now().UTC()}
	m.mu.Unlock()
	return true
}

func (m *Manager) endJob(err error, rolledBack bool) {
	now := time.Now().UTC()
	m.mu.Lock()
	m.job.Running = false
	m.job.FinishedAt = &now
	m.job.RolledBack = rolledBack
	if err != nil {
		m.job.Error = err.Error()
	}
	m.mu.Unlock()
	m.jobMu.Unlock()
}

// StartInstall installs the given Xray-core release in the background.
// Progress and the outcome are reported by Job.
func (m *Manager) StartInstall(version string) error {
	if !tagRe.MatchString(version) {
		return ErrBadVersion
	}
	if !m.beginJob("install", version) {
		return ErrBusy
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		rb, err := m.install(ctx, version)
		if err != nil {
			slog.Error("xray install failed", "version", version, "err", err, "rolled_back", rb)
			m.logs.add(fmt.Sprintf("[veilon] install of xray %s failed: %v", version, err))
		}
		m.endJob(err, rb)
	}()
	return nil
}

// StartGeoUpdate refreshes geoip.dat and geosite.dat in the background.
func (m *Manager) StartGeoUpdate() error {
	if !m.beginJob("geo", "") {
		return ErrBusy
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		rb, err := m.updateGeo(ctx)
		if err != nil {
			slog.Error("geo files update failed", "err", err, "rolled_back", rb)
			m.logs.add(fmt.Sprintf("[veilon] geo files update failed: %v", err))
		}
		m.endJob(err, rb)
	}()
	return nil
}

func (m *Manager) install(ctx context.Context, version string) (rolledBack bool, err error) {
	dir := filepath.Dir(m.opt.BinPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return false, err
	}
	stage, err := os.MkdirTemp(dir, ".xray-install-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stage)
	m.logs.add("[veilon] downloading xray " + version)
	if err := downloadRelease(ctx, httpClient(), releaseBase(version), stage, true, map[string]os.FileMode{"xray": 0o755}); err != nil {
		return false, err
	}
	newBin := filepath.Join(stage, "xray")
	got := detectVersion(newBin)
	if got == "" {
		return false, fmt.Errorf("downloaded binary does not run on this host")
	}
	if want := strings.TrimPrefix(version, "v"); !strings.HasPrefix(got, want) {
		return false, fmt.Errorf("downloaded binary reports version %s, expected %s", got, want)
	}
	if err := m.testConfig(ctx, newBin, dir); err != nil {
		return false, fmt.Errorf("the current config is rejected by xray %s: %w", version, err)
	}
	return m.swapAndVerify(ctx, []swap{{src: newBin, dst: m.opt.BinPath}})
}

// testConfig checks the current config with the candidate binary.
func (m *Manager) testConfig(ctx context.Context, bin, assets string) error {
	if _, err := os.Stat(m.ConfigPath()); err != nil {
		return nil // nothing rendered yet
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, bin, "run", "-test", "-c", m.ConfigPath())
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assets)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(lastLines(string(out), 3))
	}
	return nil
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.TrimSpace(strings.Join(ls, " | "))
}

type swap struct{ src, dst string }

// swapAndVerify replaces each dst with src atomically (keeping a .bak copy),
// restarts Xray and, if it does not stay up, restores the backups.
func (m *Manager) swapAndVerify(ctx context.Context, swaps []swap) (rolledBack bool, err error) {
	var swapErr error
	var done []swap
	restore := func() {
		for i := len(done) - 1; i >= 0; i-- {
			s := done[i]
			if _, err := os.Stat(s.dst + ".bak"); err == nil {
				_ = os.Rename(s.dst+".bak", s.dst)
			} else {
				_ = os.Remove(s.dst)
			}
		}
	}
	var startedAt time.Time
	err = m.exec(ctx, func() {
		if m.isRunning() {
			m.collect(context.Background())
			m.stop()
		}
		for _, s := range swaps {
			if _, err := os.Stat(s.dst); err == nil {
				_ = os.Remove(s.dst + ".bak")
				if err := os.Link(s.dst, s.dst+".bak"); err != nil {
					if err := copyFile(s.dst, s.dst+".bak"); err != nil {
						swapErr = fmt.Errorf("backup %s: %w", filepath.Base(s.dst), err)
						break
					}
				}
			}
			if err := os.Rename(s.src, s.dst); err != nil {
				swapErr = fmt.Errorf("replace %s: %w", filepath.Base(s.dst), err)
				break
			}
			done = append(done, s)
		}
		if swapErr != nil {
			restore()
		}
		m.apply(true)
		m.mu.Lock()
		startedAt = m.startedAt
		if v := detectVersion(m.opt.BinPath); v != "" {
			m.version = v
		}
		m.mu.Unlock()
	})
	if err != nil {
		return false, err
	}
	if swapErr != nil {
		return true, swapErr
	}
	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		m.logs.add("[veilon] update installed; xray is switched off, so it was not started")
		return false, nil
	}
	select {
	case <-time.After(verifyWindow):
	case <-ctx.Done():
	}
	m.mu.Lock()
	ok := m.running && m.startedAt.Equal(startedAt)
	lastErr := m.lastErr
	m.mu.Unlock()
	if ok {
		m.logs.add("[veilon] update applied, xray is running")
		return false, nil
	}
	m.logs.add("[veilon] xray did not stay up after the update; rolling back")
	if e := m.exec(context.Background(), func() {
		if m.isRunning() {
			m.stop()
		}
		restore()
		m.apply(true)
		m.mu.Lock()
		if v := detectVersion(m.opt.BinPath); v != "" {
			m.version = v
		}
		m.mu.Unlock()
	}); e != nil {
		return false, fmt.Errorf("xray failed after the update (%s) and rollback failed: %w", lastErr, e)
	}
	if lastErr == "" {
		lastErr = "process did not stay up"
	}
	return true, fmt.Errorf("xray failed after the update (%s); previous files restored", lastErr)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, st.Mode().Perm())
}

// Controller is the Xray-core maintenance surface used by the API.
type Controller interface {
	Status() Status
	SetPower(ctx context.Context, on bool) error
	StartInstall(version string) error
	StartGeoUpdate() error
	Releases(ctx context.Context, refresh bool) (ReleaseList, error)
	Job() *JobState
	GeoStatus() []GeoInfo
}

var _ Controller = (*Manager)(nil)
