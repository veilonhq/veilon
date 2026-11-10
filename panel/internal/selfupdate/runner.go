package selfupdate

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Updater performs one update. It runs privileged (root), outside the
// panel's service, so it survives the restart it causes.
type Updater struct {
	DataDir string
	// BinPath is the installed panel binary that gets replaced.
	BinPath string
	// BackupDir receives the binary and database backups (/root/backups).
	BackupDir string
	DBDriver  string
	DBDSN     string
	// Restart restarts the panel service (systemctl restart veilon).
	Restart func(ctx context.Context) error
	// Current is the version that is installed now.
	Current string
	Client  *http.Client
	// HealthTimeout is how long the new version has to answer healthy.
	HealthTimeout time.Duration
	// KeepBackups is how many backups are kept (oldest removed first).
	KeepBackups int
	APIURL      string

	st Status
}

// AssetName is the release asset for this machine.
func AssetName() string { return "veilon-linux-" + runtime.GOARCH }

func (u *Updater) save(phase, msg string) {
	u.st.Phase, u.st.Message, u.st.UpdatedAt = phase, msg, time.Now().UTC()
	switch phase {
	case PhaseDone, PhaseRolledBack, PhaseFailed:
		u.st.Running = false
		now := u.st.UpdatedAt
		u.st.FinishedAt = &now
	default:
		u.st.Running = true
	}
	if err := writeJSONAtomic(StatusPath(u.DataDir), u.st, 0o644); err != nil {
		slog.Warn("update status not written", "err", err)
	}
	// let the unprivileged panel replace the file later (next update)
	if fi, err := os.Stat(Dir(u.DataDir)); err == nil {
		chownLike(StatusPath(u.DataDir), fi)
	}
	slog.Info("update", "phase", phase, "msg", msg)
}

func (u *Updater) fail(err error) error {
	u.st.Error = err.Error()
	u.save(PhaseFailed, "")
	return err
}

// lock makes sure only one updater runs.
func (u *Updater) lock() (func(), error) {
	p := filepath.Join(Dir(u.DataDir), "update.lock")
	if fi, err := os.Stat(p); err == nil && (time.Since(fi.ModTime()) > staleAfter || !lockHolderAlive(p)) {
		_ = os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrBusy
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { _ = os.Remove(p) }, nil
}

// RunRequest reads the request file and performs the update it asks for.
func (u *Updater) RunRequest(ctx context.Context) error {
	b, err := os.ReadFile(RequestPath(u.DataDir))
	if err != nil {
		return fmt.Errorf("no update request: %w", err)
	}
	var req Request
	if err := json.Unmarshal(b, &req); err != nil {
		return fmt.Errorf("bad update request: %w", err)
	}
	defer os.Remove(RequestPath(u.DataDir))
	return u.Run(ctx, req)
}

// Run performs the update to req.Tag.
func (u *Updater) Run(ctx context.Context, req Request) error {
	if err := os.MkdirAll(Dir(u.DataDir), 0o755); err != nil {
		return err
	}
	unlock, err := u.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if u.Client == nil {
		u.Client = &http.Client{Timeout: 10 * time.Minute}
	}
	if u.HealthTimeout == 0 {
		u.HealthTimeout = 30 * time.Second
	}
	if u.KeepBackups == 0 {
		u.KeepBackups = 5
	}
	u.st = Status{Tag: req.Tag, From: u.Current, StartedAt: time.Now().UTC()}
	u.save(PhaseDownload, "")
	if !Newer(req.Tag, u.Current) {
		return u.fail(fmt.Errorf("%s is not newer than the installed %s", req.Tag, u.Current))
	}
	api := u.APIURL
	if api == "" {
		api = APIURL()
	}
	rel, err := FetchLatest(ctx, u.Client, api, u.Current)
	if err != nil {
		return u.fail(fmt.Errorf("release list: %w", err))
	}
	if rel == nil || rel.Tag != req.Tag {
		return u.fail(fmt.Errorf("%s is not the newest stable release", req.Tag))
	}
	binURL, sumURL := rel.Assets[AssetName()], rel.Assets["checksums.txt"]
	if binURL == "" || sumURL == "" {
		return u.fail(fmt.Errorf("release %s has no %s or checksums.txt", req.Tag, AssetName()))
	}
	dir := filepath.Dir(u.BinPath)
	tmp, err := os.CreateTemp(dir, ".veilon-new-*")
	if err != nil {
		return u.fail(err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	h := sha256.New()
	if err := get(ctx, u.Client, binURL, io.MultiWriter(tmp, h)); err != nil {
		tmp.Close()
		return u.fail(fmt.Errorf("download: %w", err))
	}
	tmp.Close()
	u.save(PhaseVerify, "")
	var sums strings.Builder
	if err := get(ctx, u.Client, sumURL, &sums); err != nil {
		return u.fail(fmt.Errorf("checksums.txt: %w", err))
	}
	want := ChecksumFor(sums.String(), AssetName())
	if want == "" {
		return u.fail(fmt.Errorf("checksums.txt has no entry for %s", AssetName()))
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return u.fail(fmt.Errorf("checksum mismatch (got %s, want %s); nothing was changed", got, want))
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return u.fail(err)
	}
	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := exec.CommandContext(vctx, tmpName, "version").Output()
	cancel()
	if err != nil {
		return u.fail(fmt.Errorf("the downloaded binary does not run on this server: %w", err))
	}
	if !strings.Contains(string(out), req.Tag) {
		return u.fail(fmt.Errorf("the downloaded binary reports %q, expected %s", strings.TrimSpace(string(out)), req.Tag))
	}

	u.save(PhaseBackup, "")
	bdir, err := u.backup()
	if err != nil {
		return u.fail(fmt.Errorf("backup: %w (nothing was changed)", err))
	}
	u.st.BackupDir = bdir
	oldBin := filepath.Join(bdir, "veilon")

	u.save(PhaseInstall, "")
	if fi, err := os.Stat(u.BinPath); err == nil {
		_ = os.Chmod(tmpName, fi.Mode().Perm())
	}
	if err := os.Rename(tmpName, u.BinPath); err != nil {
		return u.fail(fmt.Errorf("replace binary: %w", err))
	}

	u.save(PhaseRestart, "")
	if err := u.Restart(ctx); err != nil {
		slog.Warn("restart after update failed", "err", err)
	}
	u.save(PhaseHealth, "")
	if herr := waitHealthy(ctx, req.HealthURL, req.Tag, u.HealthTimeout); herr == nil {
		u.save(PhaseDone, "")
		return nil
	} else {
		// Roll back: previous binary, restart, wait for it.
		slog.Error("new version is not healthy; rolling back", "err", herr)
		if err := installCopy(oldBin, u.BinPath); err != nil {
			u.st.Error = fmt.Sprintf("%v; rollback failed: %v (previous binary is in %s)", herr, err, bdir)
			u.save(PhaseFailed, "")
			return errors.New(u.st.Error)
		}
		_ = u.Restart(ctx)
		if err := waitHealthy(ctx, req.HealthURL, u.Current, 2*u.HealthTimeout); err != nil {
			u.st.Error = fmt.Sprintf("%v; the previous version was restored but does not answer either: %v (backups in %s)", herr, err, bdir)
			u.save(PhaseFailed, "")
			return errors.New(u.st.Error)
		}
		u.st.Error = herr.Error()
		u.save(PhaseRolledBack, "")
		return herr
	}
}

// backup copies the current binary and the database into a new directory
// under BackupDir and prunes old backups.
func (u *Updater) backup() (string, error) {
	if err := os.MkdirAll(u.BackupDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("veilon-%s-%s", strings.TrimPrefix(u.Current, "v"), time.Now().UTC().Format("20060102-150405"))
	dir := filepath.Join(u.BackupDir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	if err := copyFile(u.BinPath, filepath.Join(dir, "veilon"), 0o755); err != nil {
		return "", fmt.Errorf("binary: %w", err)
	}
	switch u.DBDriver {
	case "sqlite":
		if err := sqliteBackup(u.DBDSN, filepath.Join(dir, "veilon.db")); err != nil {
			return "", fmt.Errorf("sqlite: %w", err)
		}
	case "postgres":
		pgDump, err := exec.LookPath("pg_dump")
		if err != nil {
			return "", errors.New("pg_dump is not installed")
		}
		cmd := exec.Command(pgDump, "--format=custom", "--file="+filepath.Join(dir, "veilon.pgdump"), "--dbname="+u.DBDSN)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("pg_dump: %v: %s", err, strings.TrimSpace(string(out)))
		}
	default:
		return "", fmt.Errorf("unknown database driver %q", u.DBDriver)
	}
	ok = true
	pruneBackups(u.BackupDir, u.KeepBackups)
	return dir, nil
}

// sqliteBackup writes a consistent copy of a live SQLite database.
func sqliteBackup(dsn, dst string) error {
	path, _, _ := strings.Cut(dsn, "?")
	gdb, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(10000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return gdb.Exec("VACUUM INTO ?", dst).Error
}

func pruneBackups(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "veilon-") {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, _ := os.Stat(filepath.Join(dir, names[i]))
		b, _ := os.Stat(filepath.Join(dir, names[j]))
		return a.ModTime().After(b.ModTime())
	})
	for i := keep; i < len(names); i++ {
		_ = os.RemoveAll(filepath.Join(dir, names[i]))
	}
}

// installCopy puts a copy of src at dst atomically.
func installCopy(src, dst string) error {
	tmp := filepath.Join(filepath.Dir(dst), ".veilon-rollback")
	if err := copyFile(src, tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func copyFile(src, dst string, mode os.FileMode) error {
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
	return out.Close()
}

// waitHealthy polls the panel's health endpoint until it reports want.
func waitHealthy(ctx context.Context, url, want string, timeout time.Duration) error {
	if url == "" {
		return errors.New("no health URL")
	}
	cl := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		// loopback check of our own panel, whose certificate names the
		// public address
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		Proxy:           nil,
	}}
	deadline := time.Now().Add(timeout)
	last := "no answer"
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		resp, err := cl.Get(url)
		if err != nil {
			last = err.Error()
			continue
		}
		var h struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&h)
		resp.Body.Close()
		if resp.StatusCode == 200 && h.Status == "ok" && h.Version == want {
			return nil
		}
		last = fmt.Sprintf("HTTP %d, version %q", resp.StatusCode, h.Version)
	}
	return fmt.Errorf("version %s did not answer healthy within %s (%s)", want, timeout, last)
}

func get(ctx context.Context, cl *http.Client, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, 512<<20))
	return err
}

// ChecksumFor returns the SHA-256 for name from a sha256sum-style list.
func ChecksumFor(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

// lockHolderAlive reports whether the process that wrote the lock file still
// runs. A lock left by a killed updater is cleared instead of blocking every
// later update until it goes stale.
func lockHolderAlive(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		// unreadable content: trust the age check only
		return true
	}
	if pid == os.Getpid() {
		return true
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
