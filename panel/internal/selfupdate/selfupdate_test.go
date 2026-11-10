package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.2.2", true}, {"v0.2.10", "v0.2.9", true}, {"v1.0.0", "v0.9.9", true},
		{"v0.2.2", "v0.2.2", false}, {"v0.2.1", "v0.2.2", false}, {"v0.3.0-rc1", "v0.2.2", false},
		{"v0.3.0", "dev", false}, {"garbage", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s,%s)=%v", c.a, c.b, got)
		}
	}
}

func script(version string, ok bool) []byte {
	run := "exec sleep 600"
	if !ok {
		run = "exit 1"
	}
	return []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = version ]; then echo \"veilon %s (commit x)\"; exit 0; fi\n%s\n", version, run))
}

type fakeSource struct {
	srv      *httptest.Server
	releases []map[string]any
	files    map[string][]byte
	hits     int
	mu       sync.Mutex
	fail     bool
}

func newSource(t *testing.T) *fakeSource {
	f := &fakeSource{files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/releases" {
			f.hits++
			if f.fail {
				http.Error(w, "down", 503)
				return
			}
			_ = json.NewEncoder(w).Encode(f.releases)
			return
		}
		b, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// publish adds a release with a binary for this arch and checksums.txt.
func (f *fakeSource) publish(tag string, bin []byte, badSum, pre bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(bin)
	h := hex.EncodeToString(sum[:])
	if badSum {
		h = strings.Repeat("0", 64)
	}
	f.files["/dl/"+tag+"/"+AssetName()] = bin
	f.files["/dl/"+tag+"/checksums.txt"] = []byte(h + "  " + AssetName() + "\n")
	f.releases = append(f.releases, map[string]any{
		"tag_name": tag, "name": "Veilon " + tag, "body": "### Changes\n- something for " + tag, "prerelease": pre, "draft": false,
		"html_url": "https://example.invalid/" + tag, "published_at": time.Now().UTC().Format(time.RFC3339),
		"assets": []map[string]any{
			{"name": AssetName(), "browser_download_url": f.srv.URL + "/dl/" + tag + "/" + AssetName()},
			{"name": "checksums.txt", "browser_download_url": f.srv.URL + "/dl/" + tag + "/checksums.txt"},
		},
	})
}

func TestCheckerFiltersAndTolerates(t *testing.T) {
	src := newSource(t)
	src.publish("v0.2.3", script("v0.2.3", true), false, false)
	src.publish("v0.4.0-beta", script("v0.4.0-beta", true), false, true)
	src.publish("v0.3.0", script("v0.3.0", true), false, false)
	c := &Checker{Current: "v0.2.2", URL: src.srv.URL + "/releases"}
	if err := c.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, _ := c.Latest()
	if r == nil || r.Tag != "v0.3.0" || !strings.Contains(r.Notes, "v0.3.0") {
		t.Fatalf("latest %+v", r)
	}
	// an unreachable source keeps the last answer
	src.fail = true
	if err := c.CheckOnce(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if r2, _ := c.Latest(); r2 == nil || r2.Tag != "v0.3.0" {
		t.Fatal("last answer lost after a failed check")
	}
	up := &Checker{Current: "v0.3.0", URL: src.srv.URL + "/releases"}
	src.fail = false
	_ = up.CheckOnce(context.Background())
	if r, _ := up.Latest(); r != nil {
		t.Fatalf("up to date should report nothing, got %+v", r)
	}
}

// fakePanel answers /health with the version of the binary that is
// "running": Restart re-reads the installed binary.
type fakePanel struct {
	srv     *httptest.Server
	mu      sync.Mutex
	version string
	bin     string
}

func newPanel(t *testing.T, bin string) *fakePanel {
	p := &fakePanel{bin: bin}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		v := p.version
		p.mu.Unlock()
		if v == "" {
			http.Error(w, "down", 502)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": v})
	}))
	t.Cleanup(p.srv.Close)
	p.restart(context.Background())
	return p
}

// restart "starts" the installed binary: a binary whose main command
// exits at once never becomes healthy.
func (p *fakePanel) restart(ctx context.Context) error {
	out, _ := exec.Command(p.bin, "version").Output()
	v := strings.Fields(string(out))
	healthy := exec.Command("/bin/sh", "-c", "grep -q 'exit 1' "+p.bin).Run() != nil
	p.mu.Lock()
	defer p.mu.Unlock()
	p.version = ""
	if len(v) >= 2 && healthy {
		p.version = v[1]
	}
	return nil
}

func setup(t *testing.T) (*Updater, *fakeSource, *fakePanel) {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	bin := filepath.Join(binDir, "veilon")
	if err := os.WriteFile(bin, script("v0.2.2", true), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "data", "veilon.db")
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	src := newSource(t)
	panel := newPanel(t, bin)
	u := &Updater{DataDir: filepath.Join(dir, "data"), BinPath: bin, BackupDir: filepath.Join(dir, "backups"), DBDriver: "sqlite",
		DBDSN: sqlitePath(t, dbPath), Current: "v0.2.2", Restart: panel.restart, HealthTimeout: 3 * time.Second, APIURL: src.srv.URL + "/releases"}
	return u, src, panel
}

func sqlitePath(t *testing.T, p string) string {
	gdb := testutil.DBAt(t, p)
	gdb.Exec("INSERT INTO settings (key, value) VALUES ('marker', 'kept')")
	return p
}

func installed(t *testing.T, bin string) string {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))[1]
}

func TestUpdateSucceeds(t *testing.T) {
	u, src, panel := setup(t)
	src.publish("v0.3.0", script("v0.3.0", true), false, false)
	err := u.Run(context.Background(), Request{Tag: "v0.3.0", HealthURL: panel.srv.URL + "/health"})
	if err != nil {
		t.Fatal(err)
	}
	st := ReadStatus(u.DataDir)
	if st == nil || st.Phase != PhaseDone || st.Running || st.FinishedAt == nil {
		t.Fatalf("status %+v", st)
	}
	if v := installed(t, u.BinPath); v != "v0.3.0" {
		t.Fatalf("installed %s", v)
	}
	if v := installed(t, filepath.Join(st.BackupDir, "veilon")); v != "v0.2.2" {
		t.Fatalf("backup binary %s", v)
	}
	if fi, err := os.Stat(filepath.Join(st.BackupDir, "veilon.db")); err != nil || fi.Size() == 0 {
		t.Fatalf("database backup missing: %v", err)
	}
	out, _ := exec.Command("/bin/sh", "-c", "grep -c marker "+filepath.Join(st.BackupDir, "veilon.db")).Output()
	if strings.TrimSpace(string(out)) == "0" {
		t.Fatal("database backup lacks the data")
	}
}

func TestUpdateChecksumMismatchChangesNothing(t *testing.T) {
	u, src, panel := setup(t)
	src.publish("v0.3.0", script("v0.3.0", true), true, false)
	err := u.Run(context.Background(), Request{Tag: "v0.3.0", HealthURL: panel.srv.URL + "/health"})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if v := installed(t, u.BinPath); v != "v0.2.2" {
		t.Fatalf("binary changed to %s", v)
	}
	if st := ReadStatus(u.DataDir); st.Phase != PhaseFailed || st.Running {
		t.Fatalf("status %+v", st)
	}
	if ents, _ := os.ReadDir(u.BackupDir); len(ents) != 0 {
		t.Fatal("no backup should be made before verification passes")
	}
}

func TestUnhealthyUpdateRollsBack(t *testing.T) {
	u, src, panel := setup(t)
	src.publish("v0.3.0", script("v0.3.0", false), false, false)
	err := u.Run(context.Background(), Request{Tag: "v0.3.0", HealthURL: panel.srv.URL + "/health"})
	if err == nil {
		t.Fatal("expected a rollback error")
	}
	st := ReadStatus(u.DataDir)
	if st.Phase != PhaseRolledBack || st.Running || st.Error == "" {
		t.Fatalf("status %+v", st)
	}
	if v := installed(t, u.BinPath); v != "v0.2.2" {
		t.Fatalf("previous binary not restored: %s", v)
	}
	panel.mu.Lock()
	v := panel.version
	panel.mu.Unlock()
	if v != "v0.2.2" {
		t.Fatalf("panel not running the previous version: %q", v)
	}
}

func TestRequestValidation(t *testing.T) {
	u, src, panel := setup(t)
	src.publish("v0.3.0", script("v0.3.0", true), false, false)
	src.publish("v0.3.1", script("v0.3.1", true), false, false)
	// only the newest stable release may be installed, never an older one
	for _, tag := range []string{"v0.2.1", "v0.3.0", "v9.9.9"} {
		if err := u.Run(context.Background(), Request{Tag: tag, HealthURL: panel.srv.URL + "/health"}); err == nil {
			t.Errorf("%s: expected refusal", tag)
		}
	}
	if v := installed(t, u.BinPath); v != "v0.2.2" {
		t.Fatalf("binary changed: %s", v)
	}
	// a second updater cannot run while one holds the lock
	_ = os.MkdirAll(Dir(u.DataDir), 0o755)
	_ = os.WriteFile(filepath.Join(Dir(u.DataDir), "update.lock"), []byte("1"), 0o600)
	if err := u.Run(context.Background(), Request{Tag: "v0.3.1"}); err != ErrBusy {
		t.Fatalf("want ErrBusy, got %v", err)
	}
}

func TestServiceStart(t *testing.T) {
	t.Setenv("VEILON_UPDATER_SPAWN", "1")
	src := newSource(t)
	src.publish("v0.3.0", script("v0.3.0", true), false, false)
	dir := t.TempDir()
	c := &Checker{Current: "v0.2.2", URL: src.srv.URL + "/releases"}
	_ = c.CheckOnce(context.Background())
	// Self is a stub that records it was started.
	stub := filepath.Join(dir, "stub")
	_ = os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+filepath.Join(dir, "ran")+"\n"), 0o755)
	s := &Service{Checker: c, DataDir: filepath.Join(dir, "data"), Current: "v0.2.2", Self: stub, HealthURL: func() string { return "http://127.0.0.1:1/x/api/v1/health" }}
	if _, err := s.Start("v0.2.9", "admin"); err == nil {
		t.Fatal("unknown tag accepted")
	}
	st, err := s.Start("v0.3.0", "admin")
	if err != nil || st.Phase != PhaseQueued {
		t.Fatalf("start: %+v %v", st, err)
	}
	b, _ := os.ReadFile(RequestPath(s.DataDir))
	if !strings.Contains(string(b), `"tag": "v0.3.0"`) || !strings.Contains(string(b), "health") {
		t.Fatalf("request %s", b)
	}
	if _, err := s.Start("v0.3.0", "admin"); err != ErrBusy {
		t.Fatalf("second start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if out, _ := os.ReadFile(filepath.Join(dir, "ran")); !strings.Contains(string(out), "self-update --run") {
		t.Fatalf("updater not spawned: %q", out)
	}
}

func TestLockLeftByDeadUpdaterIsCleared(t *testing.T) {
	dir := t.TempDir()
	u := &Updater{DataDir: dir}
	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(Dir(dir), "update.lock")
	// a pid that cannot exist
	if err := os.WriteFile(p, []byte("2147483646\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := u.lock()
	if err != nil {
		t.Fatalf("stale lock blocked the updater: %v", err)
	}
	// a live holder (this process) keeps the lock
	if _, err := u.lock(); err != ErrBusy {
		t.Fatalf("second lock = %v, want ErrBusy", err)
	}
	unlock()
}
