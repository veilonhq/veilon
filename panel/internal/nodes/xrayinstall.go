package nodes

// Xray-core install on a node (agent side). The master asks for a release
// tag (POST /v1/xray); the agent downloads the official release for its
// own architecture, requires its published SHA-256 digest to match, checks
// that the new binary runs and reports that version, tests the node's
// current config with it (xray run -test), then swaps it in (keeping
// xray.bak), restarts Xray and watches it. If Xray does not stay up, the
// previous binary is put back and started again. The geo files and the
// config are not touched. Progress is in GET /v1/xray and /v1/info.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/veilonhq/veilon/panel/internal/xray"
)

// CapXrayInstall: the agent installs a chosen Xray-core release on request
// (POST /v1/xray). Older agents only get Xray once, at install time.
const CapXrayInstall = "xray_install"

// XrayInstallRequest is the body of POST /v1/xray.
type XrayInstallRequest struct {
	Version string `json:"version"`
}

// BinaryCore is a Core whose Xray binary can be replaced in place.
type BinaryCore interface {
	// BinPath is the installed Xray binary.
	BinPath() string
	// TestWith checks the current config with another binary ("" config
	// yet = nil).
	TestWith(ctx context.Context, bin string) error
	// SwapBinary puts newBin in place of the installed binary (keeping
	// <bin>.bak), restarts Xray with the current config and waits verify:
	// if Xray does not stay up, the previous binary is restored and
	// started again (rolledBack true, with an error).
	SwapBinary(ctx context.Context, newBin string, verify time.Duration) (rolledBack bool, err error)
}

// DefaultXrayVerify is how long a newly installed Xray must stay up.
const DefaultXrayVerify = 5 * time.Second

// xrayJob is the agent's current or last Xray install.
type xrayJob struct {
	mu      sync.Mutex
	st      *UpdateStatus
	running bool
}

func (j *xrayJob) get() *UpdateStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.st == nil {
		return nil
	}
	c := *j.st
	return &c
}

func (j *xrayJob) set(f func(st *UpdateStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.st != nil {
		f(j.st)
		j.st.UpdatedAt = time.Now().UTC()
	}
}

func (a *Agent) binaryCore() BinaryCore {
	bc, _ := a.Core.(BinaryCore)
	return bc
}

// postXray starts an Xray-core install (202) or says why not.
func (a *Agent) postXray(w http.ResponseWriter, r *http.Request) {
	bc := a.binaryCore()
	if bc == nil {
		writeJ(w, 404, map[string]string{"error": "this agent cannot install Xray-core"})
		return
	}
	var req XrayInstallRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req); err != nil {
		writeJ(w, 400, map[string]string{"error": "bad request: " + err.Error()})
		return
	}
	v := strings.TrimSpace(req.Version)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !xray.ValidTag(v) {
		writeJ(w, 400, map[string]string{"error": xray.ErrBadVersion.Error()})
		return
	}
	_, cur, _ := a.Core.Status()
	if cur != "" && strings.TrimPrefix(v, "v") == cur {
		writeJ(w, 409, map[string]string{"error": "xray " + cur + " is already installed"})
		return
	}
	a.xjob.mu.Lock()
	if a.xjob.running {
		a.xjob.mu.Unlock()
		writeJ(w, 409, map[string]string{"error": "an Xray-core install is already running"})
		return
	}
	now := time.Now().UTC()
	a.xjob.running = true
	a.xjob.st = &UpdateStatus{Target: v, From: cur, Phase: UpdDownloading, StartedAt: now, UpdatedAt: now}
	st := *a.xjob.st
	a.xjob.mu.Unlock()
	go a.installXray(bc, v)
	writeJ(w, 202, &st)
}

func (a *Agent) installXray(bc BinaryCore, version string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rb, err := a.runXrayInstall(ctx, bc, version)
	now := time.Now().UTC()
	a.xjob.mu.Lock()
	a.xjob.running = false
	if a.xjob.st != nil {
		a.xjob.st.FinishedAt = &now
		a.xjob.st.UpdatedAt = now
		a.xjob.st.RolledBack = rb
		if err != nil {
			a.xjob.st.Phase, a.xjob.st.Error = UpdFailed, cut(err.Error(), 600)
		} else {
			a.xjob.st.Phase, a.xjob.st.Error = UpdDone, ""
		}
	}
	a.xjob.mu.Unlock()
	if err != nil {
		slog.Warn("xray install failed", "version", version, "err", err, "rolled_back", rb)
	} else {
		slog.Info("xray installed", "version", version)
	}
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (a *Agent) runXrayInstall(ctx context.Context, bc BinaryCore, version string) (bool, error) {
	dir := filepath.Dir(bc.BinPath())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return false, err
	}
	stage, err := os.MkdirTemp(dir, ".xray-install-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stage)
	dl := a.XrayDownload
	if dl == nil {
		dl = xray.DownloadBinary
	}
	if err := dl(ctx, stage, version); err != nil {
		return false, fmt.Errorf("download: %w", err)
	}
	a.xjob.set(func(st *UpdateStatus) { st.Phase = UpdVerifying })
	newBin := filepath.Join(stage, "xray")
	got := xrayVersion(newBin)
	if got == "" {
		return false, errors.New("the downloaded xray does not run on this server")
	}
	if want := strings.TrimPrefix(version, "v"); !strings.HasPrefix(got, want) {
		return false, fmt.Errorf("the downloaded xray reports version %s, expected %s", got, want)
	}
	if err := bc.TestWith(ctx, newBin); err != nil {
		return false, fmt.Errorf("xray %s rejects the node's current config: %w", version, err)
	}
	a.xjob.set(func(st *UpdateStatus) { st.Phase = UpdRestarting })
	verify := a.XrayVerify
	if verify <= 0 {
		verify = DefaultXrayVerify
	}
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	a.collect(ctx) // keep the counters of the process about to restart
	return bc.SwapBinary(ctx, newBin, verify)
}

// BinPath is the installed Xray binary.
func (c *ProcCore) BinPath() string { return c.Bin }

// TestWith checks the running config with bin.
func (c *ProcCore) TestWith(ctx context.Context, bin string) error {
	if _, err := os.Stat(c.cfgPath()); err != nil {
		return nil // no config yet
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, bin, "run", "-test", "-c", c.cfgPath())
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(c.Bin))
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(lastLines(string(out), 3))
	}
	return nil
}

// SwapBinary replaces the binary, restarts Xray and rolls back when the new
// one does not stay up for verify.
func (c *ProcCore) SwapBinary(ctx context.Context, newBin string, verify time.Duration) (bool, error) {
	bak := c.Bin + ".bak"
	_, haveOld := os.Stat(c.Bin)
	c.Stop()
	if haveOld == nil {
		_ = os.Remove(bak)
		if err := os.Link(c.Bin, bak); err != nil {
			if err := copyBin(c.Bin, bak); err != nil {
				c.restartIfConfigured()
				return false, fmt.Errorf("backup of the current xray: %w", err)
			}
		}
	}
	if err := os.Rename(newBin, c.Bin); err != nil {
		c.restartIfConfigured()
		return false, fmt.Errorf("replace xray: %w", err)
	}
	c.mu.Lock()
	c.version = ""
	c.mu.Unlock()
	if _, err := os.Stat(c.cfgPath()); err != nil {
		return false, nil // no config yet: nothing to start
	}
	if c.startedStays(ctx, verify) {
		return false, nil
	}
	c.mu.Lock()
	why := c.lastErr
	c.mu.Unlock()
	if why == "" {
		why = "the process did not stay up"
	}
	if b, err := os.ReadFile(filepath.Join(c.Dir, "xray.log")); err == nil {
		if l := lastLines(string(b), 2); l != "" {
			why += ": " + cut(l, 300)
		}
	}
	c.Stop()
	if haveOld != nil {
		return false, fmt.Errorf("the new xray did not stay up (%s)", why)
	}
	if err := os.Rename(bak, c.Bin); err != nil {
		return false, fmt.Errorf("the new xray did not stay up (%s) and restoring the previous one failed: %w", why, err)
	}
	c.mu.Lock()
	c.version = ""
	c.mu.Unlock()
	if err := c.start(); err != nil {
		return true, fmt.Errorf("the new xray did not stay up (%s); the previous one was restored but did not start: %w", why, err)
	}
	return true, fmt.Errorf("the new xray did not stay up (%s); the previous version was restored", why)
}

// startedStays starts Xray and reports whether the same process still runs
// after d.
func (c *ProcCore) startedStays(ctx context.Context, d time.Duration) bool {
	if err := c.start(); err != nil {
		return false
	}
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return cmd != nil && c.cmd == cmd
}

func (c *ProcCore) restartIfConfigured() {
	if _, err := os.Stat(c.cfgPath()); err == nil {
		_ = c.start()
	}
}

func copyBin(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}
