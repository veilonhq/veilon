package nodes

// Node agent self-update. The master sends the release asset URL for the
// node's architecture and its SHA-256 (POST /v1/update). The agent
// downloads it into its state directory, checks the hash and that the
// binary runs and reports the expected version, then starts the update
// helper outside its own service (systemd-run; the agent's unit cannot
// write /usr/local/bin). The helper (the current, known-good binary)
// backs up the installed binary, swaps it atomically, restarts the service
// and watches: the new agent confirms as soon as the master reaches it over
// mutual TLS. Without that confirmation within the timeout (90 s) the
// helper restores the previous binary and restarts again. The enrollment
// (certificate) is never touched.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/selfupdate"
)

// DefaultUpdateTimeout is how long a new agent has to be reached by the
// master before the helper rolls back.
const DefaultUpdateTimeout = 90 * time.Second

// UpdatePlan is what the agent hands to the helper (update/plan.json).
type UpdatePlan struct {
	Target  string `json:"target"`
	From    string `json:"from"`
	SHA256  string `json:"sha256"`
	BinPath string `json:"bin_path"`
	// RestartCmd restarts the agent ("" = systemctl restart veilon-node).
	RestartCmd string `json:"restart_cmd,omitempty"`
	TimeoutSec int    `json:"timeout_sec"`
}

// pendingMark is written by the helper once the new binary is installed;
// the new agent renames it to confirmed.json when the master reaches it,
// the helper renames it to rollback.json when time is up. Whichever rename
// succeeds first wins, so the two never both act.
type pendingMark struct {
	Target    string    `json:"target"`
	From      string    `json:"from"`
	Deadline  time.Time `json:"deadline"`
	HelperPID int       `json:"helper_pid"`
}

// SelfUpdater is the agent side of node updates.
type SelfUpdater struct {
	Dir     string // the agent's state directory (files go to Dir/update)
	BinPath string // the installed binary (/usr/local/bin/veilon)
	Version string
	Client  *http.Client
	// Launch starts "<exe> node apply-update --dir DIR [--watch]" outside
	// the agent's service (nil = updates are not possible here).
	Launch func(exe string, args []string) error
	// RestartCmd is copied into the plan ("" = systemctl).
	RestartCmd string
	Timeout    time.Duration

	mu      sync.Mutex
	running bool
	checked bool
}

// ErrUpdateBusy means an update of this node is already running.
var ErrUpdateBusy = errors.New("an update of this node is already running")

func updDir(dir string) string              { return filepath.Join(dir, "update") }
func updFile(dir, name string) string       { return filepath.Join(updDir(dir), name) }
func (u *SelfUpdater) file(n string) string { return updFile(u.Dir, n) }

var hexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ReadUpdateStatus returns the node's last update status (nil when none).
func ReadUpdateStatus(dir string) *UpdateStatus {
	b, err := os.ReadFile(updFile(dir, "status.json"))
	if err != nil {
		return nil
	}
	var st UpdateStatus
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	return &st
}

func writeUpdateStatus(dir string, st *UpdateStatus) {
	st.UpdatedAt = time.Now().UTC()
	if st.Final() && st.FinishedAt == nil {
		t := st.UpdatedAt
		st.FinishedAt = &t
	}
	if err := writeJSONFile(updFile(dir, "status.json"), st); err != nil {
		slog.Warn("node update status not written", "err", err)
	}
	slog.Info("node update", "phase", st.Phase, "target", st.Target, "err", st.Error)
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// staleUpdate: a non-final status this old belongs to a crashed update.
const staleUpdate = 20 * time.Minute

// Busy reports whether an update is running (here, or in a helper).
func (u *SelfUpdater) Busy() bool {
	u.mu.Lock()
	r := u.running
	u.mu.Unlock()
	if r {
		return true
	}
	if _, err := os.Stat(u.file("pending.json")); err == nil {
		return true
	}
	st := ReadUpdateStatus(u.Dir)
	return st != nil && !st.Final() && time.Since(st.UpdatedAt) < staleUpdate
}

// Start validates req and begins the update in the background.
func (u *SelfUpdater) Start(req UpdateRequest) error {
	if u.Launch == nil {
		return errors.New("this node cannot update itself (it does not run under systemd); update it with the install command")
	}
	if _, ok := selfupdate.ParseVersion(req.Version); !ok {
		return fmt.Errorf("%q is not a release version", req.Version)
	}
	if !selfupdate.Newer(req.Version, u.Version) {
		return fmt.Errorf("refusing to install %s over %s: not newer", req.Version, u.Version)
	}
	if !hexRe.MatchString(req.SHA256) {
		return errors.New("sha256 must be 64 hex digits")
	}
	if pu, err := url.Parse(req.URL); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
		return errors.New("url must be an http(s) URL")
	}
	if u.Busy() {
		return ErrUpdateBusy
	}
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return ErrUpdateBusy
	}
	u.running = true
	u.mu.Unlock()
	st := &UpdateStatus{Target: req.Version, From: u.Version, Phase: UpdDownloading, StartedAt: time.Now().UTC()}
	writeUpdateStatus(u.Dir, st)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("node update panicked", "panic", r)
			}
			u.mu.Lock()
			u.running = false
			u.mu.Unlock()
		}()
		u.run(req, st)
	}()
	return nil
}

func (u *SelfUpdater) run(req UpdateRequest, st *UpdateStatus) {
	fail := func(err error) {
		st.Phase, st.Error = UpdFailed, err.Error()
		writeUpdateStatus(u.Dir, st)
	}
	for _, n := range []string{"pending.json", "confirmed.json", "rollback.json"} {
		_ = os.Remove(u.file(n))
	}
	cl := u.Client
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Minute}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tmpPath := u.file(".download")
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		fail(err)
		return
	}
	defer os.Remove(tmpPath)
	h := sha256.New()
	if err := selfupdate.Download(ctx, cl, req.URL, io.MultiWriter(f, h)); err != nil {
		f.Close()
		fail(fmt.Errorf("download: %w", err))
		return
	}
	if err := f.Close(); err != nil {
		fail(err)
		return
	}
	st.Phase = UpdVerifying
	writeUpdateStatus(u.Dir, st)
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, req.SHA256) {
		fail(fmt.Errorf("checksum mismatch (got %s, want %s); nothing was changed", got, strings.ToLower(req.SHA256)))
		return
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		fail(err)
		return
	}
	vctx, vcancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := exec.CommandContext(vctx, tmpPath, "version").Output()
	vcancel()
	if err != nil {
		fail(fmt.Errorf("the downloaded binary does not run on this node: %v; nothing was changed", err))
		return
	}
	if !strings.Contains(string(out), req.Version) {
		fail(fmt.Errorf("the downloaded binary reports %q, expected %s; nothing was changed", firstLine(string(out)), req.Version))
		return
	}
	if err := os.Rename(tmpPath, u.file("new")); err != nil {
		fail(err)
		return
	}
	timeout := u.Timeout
	if timeout <= 0 {
		timeout = DefaultUpdateTimeout
	}
	plan := UpdatePlan{Target: req.Version, From: u.Version, SHA256: strings.ToLower(req.SHA256), BinPath: u.BinPath,
		RestartCmd: u.RestartCmd, TimeoutSec: int(timeout / time.Second)}
	if err := writeJSONFile(u.file("plan.json"), plan); err != nil {
		fail(err)
		return
	}
	st.Phase = UpdRestarting
	writeUpdateStatus(u.Dir, st)
	// The helper runs the binary installed now (this version): it is
	// started before the swap, so it is the known-good one.
	if err := u.Launch(u.BinPath, []string{"node", "apply-update", "--dir", u.Dir}); err != nil {
		fail(fmt.Errorf("could not start the update helper: %w; nothing was changed", err))
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// Confirm is called on every request that passed mutual TLS (so it came
// from the master): a new agent waiting for confirmation takes it.
func (u *SelfUpdater) Confirm() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.checked {
		return
	}
	u.checked = true // one look per process: the marker is written before the restart
	var p pendingMark
	if err := readJSONFile(u.file("pending.json"), &p); err != nil || p.Target != u.Version {
		return
	}
	if err := os.Rename(u.file("pending.json"), u.file("confirmed.json")); err == nil {
		slog.Info("node update confirmed: the main server reached the new version", "version", u.Version)
	}
}

// Resume runs at agent start: a pending update whose helper is gone (the
// node rebooted mid-update) gets a new watcher, started from the backup
// (the previous, known-good binary).
func (u *SelfUpdater) Resume() {
	var p pendingMark
	if readJSONFile(u.file("pending.json"), &p) != nil {
		return
	}
	if p.HelperPID > 0 && pidAlive(p.HelperPID) {
		return
	}
	if u.Launch == nil {
		return
	}
	bak := u.file("backup/veilon")
	if _, err := os.Stat(bak); err != nil {
		return
	}
	if err := u.Launch(bak, []string{"node", "apply-update", "--dir", u.Dir, "--watch"}); err != nil {
		slog.Warn("could not restart the update watcher", "err", err)
	}
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Helper performs the swap and the watch ("veilon node apply-update").
type Helper struct {
	Dir string
	// Restart restarts the agent (nil = the plan's command or systemctl).
	Restart func(ctx context.Context) error
	// Poll is how often confirmation is checked (default 1s).
	Poll time.Duration
}

func (h *Helper) restart(ctx context.Context, plan *UpdatePlan) error {
	if h.Restart != nil {
		return h.Restart(ctx)
	}
	var cmd *exec.Cmd
	if plan.RestartCmd != "" {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", plan.RestartCmd)
	} else {
		cmd = exec.CommandContext(ctx, "systemctl", "restart", "--no-block", "veilon-node")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, firstLine(string(out)))
	}
	return nil
}

// Apply installs the staged binary, restarts the agent and watches it.
func (h *Helper) Apply(ctx context.Context) error {
	var plan UpdatePlan
	if err := readJSONFile(updFile(h.Dir, "plan.json"), &plan); err != nil {
		return fmt.Errorf("no update plan: %w", err)
	}
	st := ReadUpdateStatus(h.Dir)
	if st == nil || st.Target != plan.Target {
		st = &UpdateStatus{Target: plan.Target, From: plan.From, StartedAt: time.Now().UTC()}
	}
	fail := func(err error) error {
		st.Phase, st.Error = UpdFailed, err.Error()
		writeUpdateStatus(h.Dir, st)
		_ = os.Remove(updFile(h.Dir, "plan.json"))
		return err
	}
	newPath := updFile(h.Dir, "new")
	sum, err := fileSHA256(newPath)
	if err != nil {
		return fail(fmt.Errorf("staged binary: %w; nothing was changed", err))
	}
	if !strings.EqualFold(sum, plan.SHA256) {
		return fail(fmt.Errorf("staged binary changed (sha256 %s); nothing was changed", sum))
	}
	bak := updFile(h.Dir, "backup/veilon")
	if err := os.MkdirAll(filepath.Dir(bak), 0o700); err != nil {
		return fail(err)
	}
	if err := copyFileMode(plan.BinPath, bak+".tmp", 0o755); err != nil {
		return fail(fmt.Errorf("backup: %w; nothing was changed", err))
	}
	if err := os.Rename(bak+".tmp", bak); err != nil {
		return fail(err)
	}
	if err := installAtomic(newPath, plan.BinPath); err != nil {
		return fail(fmt.Errorf("replace binary: %w; nothing was changed", err))
	}
	_ = os.Remove(newPath)
	timeout := time.Duration(plan.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = DefaultUpdateTimeout
	}
	mark := pendingMark{Target: plan.Target, From: plan.From, Deadline: time.Now().UTC().Add(timeout), HelperPID: os.Getpid()}
	if err := writeJSONFile(updFile(h.Dir, "pending.json"), mark); err != nil {
		_ = installAtomic(bak, plan.BinPath)
		return fail(err)
	}
	st.Phase = UpdRestarting
	writeUpdateStatus(h.Dir, st)
	if err := h.restart(ctx, &plan); err != nil {
		slog.Warn("restart after node update failed", "err", err)
	}
	return h.watch(ctx, &plan, st)
}

// Watch only waits for the confirmation (after a reboot mid-update).
func (h *Helper) Watch(ctx context.Context) error {
	var plan UpdatePlan
	if err := readJSONFile(updFile(h.Dir, "plan.json"), &plan); err != nil {
		return err
	}
	var mark pendingMark
	if err := readJSONFile(updFile(h.Dir, "pending.json"), &mark); err != nil {
		return nil // already confirmed or rolled back
	}
	mark.HelperPID = os.Getpid()
	_ = writeJSONFile(updFile(h.Dir, "pending.json"), mark)
	st := ReadUpdateStatus(h.Dir)
	if st == nil {
		st = &UpdateStatus{Target: plan.Target, From: plan.From, StartedAt: time.Now().UTC()}
	}
	return h.watch(ctx, &plan, st)
}

func (h *Helper) watch(ctx context.Context, plan *UpdatePlan, st *UpdateStatus) error {
	poll := h.Poll
	if poll <= 0 {
		poll = time.Second
	}
	var mark pendingMark
	_ = readJSONFile(updFile(h.Dir, "pending.json"), &mark)
	deadline := mark.Deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(DefaultUpdateTimeout)
	}
	done := func() error {
		st.Phase, st.Error, st.RolledBack = UpdDone, "", false
		writeUpdateStatus(h.Dir, st)
		_ = os.Remove(updFile(h.Dir, "confirmed.json"))
		_ = os.Remove(updFile(h.Dir, "plan.json"))
		return nil
	}
	for {
		if _, err := os.Stat(updFile(h.Dir, "confirmed.json")); err == nil {
			return done()
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
	// Time is up: claim the marker. Losing the race means the new agent
	// confirmed at the last moment.
	if err := os.Rename(updFile(h.Dir, "pending.json"), updFile(h.Dir, "rollback.json")); err != nil {
		if _, cerr := os.Stat(updFile(h.Dir, "confirmed.json")); cerr == nil {
			return done()
		}
		return fmt.Errorf("pending marker gone: %w", err)
	}
	limit := time.Duration(plan.TimeoutSec) * time.Second
	if limit <= 0 {
		limit = DefaultUpdateTimeout
	}
	reason := fmt.Sprintf("%s did not reconnect to the main server within %d s", plan.Target, int(limit/time.Second))
	slog.Error("node update: rolling back", "reason", reason)
	bak := updFile(h.Dir, "backup/veilon")
	if err := installAtomic(bak, plan.BinPath); err != nil {
		st.Phase, st.Error = UpdFailed, fmt.Sprintf("%s; rollback failed: %v (previous binary: %s)", reason, err, bak)
		writeUpdateStatus(h.Dir, st)
		return errors.New(st.Error)
	}
	st.Phase, st.Error, st.RolledBack = UpdFailed, reason+"; the previous version "+plan.From+" was restored", true
	writeUpdateStatus(h.Dir, st)
	_ = os.Remove(updFile(h.Dir, "rollback.json"))
	_ = os.Remove(updFile(h.Dir, "plan.json"))
	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.restart(rctx, plan); err != nil {
		slog.Warn("restart after rollback failed", "err", err)
	}
	return errors.New(reason)
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// installAtomic copies src next to dst and renames it over dst, so dst is
// always either the old or the new file, never half written.
func installAtomic(src, dst string) error {
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(dst); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(dst), ".veilon-node-update-"+strconv.Itoa(os.Getpid()))
	if err := copyFileMode(src, tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}
