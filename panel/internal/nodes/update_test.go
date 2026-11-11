package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// fakeBinary is a script standing in for a release binary: "version"
// prints the version, anything else fails.
func fakeBinary(ver string) []byte {
	return []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo \"veilon " + ver + " (test)\"; exit 0; fi\nexit 3\n")
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// feed is a local release source: a GitHub-style release list with the
// two binaries and checksums.txt.
type feed struct {
	srv   *httptest.Server
	bins  map[string][]byte // asset name -> content
	sums  string            // checksums.txt override
	hits  atomic.Int64
	block chan struct{} // when set, binary downloads wait on it
}

func newFeed(t *testing.T, tag string, bins map[string][]byte) *feed {
	t.Helper()
	f := &feed{bins: bins}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		assets := []map[string]string{{"name": "checksums.txt", "browser_download_url": f.srv.URL + "/dl/checksums.txt"}}
		for n := range f.bins {
			assets = append(assets, map[string]string{"name": n, "browser_download_url": f.srv.URL + "/dl/" + n})
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v9.9.9", "draft": false, "prerelease": true, "assets": []any{}},
			{"tag_name": tag, "draft": false, "prerelease": false, "html_url": "https://example.invalid/" + tag, "assets": assets},
		})
	})
	mux.HandleFunc("/dl/{name}", func(w http.ResponseWriter, r *http.Request) {
		n := r.PathValue("name")
		if n == "checksums.txt" {
			if f.sums != "" {
				_, _ = w.Write([]byte(f.sums))
				return
			}
			for k, v := range f.bins {
				fmt.Fprintf(w, "%s  %s\n", sum(v), k)
			}
			return
		}
		if f.block != nil {
			<-f.block
		}
		b, ok := f.bins[n]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// launches records helper starts instead of running them.
type launches struct {
	mu    sync.Mutex
	calls [][]string
}

func (l *launches) launch(exe string, args []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, append([]string{exe}, args...))
	return nil
}

func (l *launches) n() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

func installedBin(t *testing.T, ver string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "veilon")
	if err := os.WriteFile(p, fakeBinary(ver), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitStatus(t *testing.T, dir string, final bool, phase string) *UpdateStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := ReadUpdateStatus(dir)
		if st != nil && ((final && st.Final()) || (!final && st.Phase == phase)) {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s status; last %+v", phase, ReadUpdateStatus(dir))
	return nil
}

func TestAgentUpdateChecksumMismatchChangesNothing(t *testing.T) {
	newBin := fakeBinary("v0.4.0")
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-arm64": newBin})
	dir := t.TempDir()
	bin := installedBin(t, "v0.3.5")
	l := &launches{}
	u := &SelfUpdater{Dir: dir, BinPath: bin, Version: "v0.3.5", Launch: l.launch}
	wrong := sum([]byte("something else"))
	if err := u.Start(UpdateRequest{Version: "v0.4.0", URL: f.srv.URL + "/dl/veilon-linux-arm64", SHA256: wrong}); err != nil {
		t.Fatal(err)
	}
	st := waitStatus(t, dir, true, "")
	if st.Phase != UpdFailed || !strings.Contains(st.Error, "checksum mismatch") || !strings.Contains(st.Error, "nothing was changed") {
		t.Fatalf("status %+v", st)
	}
	if l.n() != 0 {
		t.Fatal("helper started after a checksum mismatch")
	}
	if b, _ := os.ReadFile(bin); string(b) != string(fakeBinary("v0.3.5")) {
		t.Fatal("installed binary changed")
	}
	for _, f := range []string{"new", "plan.json", ".download", "pending.json"} {
		if _, err := os.Stat(filepath.Join(dir, "update", f)); err == nil {
			t.Fatalf("%s left behind", f)
		}
	}
	// a binary reporting another version is refused too
	other := fakeBinary("v0.3.9")
	f2 := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-arm64": other})
	if err := u.Start(UpdateRequest{Version: "v0.4.0", URL: f2.srv.URL + "/dl/veilon-linux-arm64", SHA256: sum(other)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	st = waitStatus(t, dir, true, "")
	if st.Phase != UpdFailed || !strings.Contains(st.Error, "expected v0.4.0") || l.n() != 0 {
		t.Fatalf("wrong version binary: %+v", st)
	}
}

func TestAgentUpdateRefusesDowngradeAndBadInput(t *testing.T) {
	u := &SelfUpdater{Dir: t.TempDir(), BinPath: "/nonexistent", Version: "v0.3.5", Launch: (&launches{}).launch}
	good := strings.Repeat("a", 64)
	for _, c := range []UpdateRequest{
		{Version: "v0.3.5", URL: "https://x.example/b", SHA256: good}, // same
		{Version: "v0.3.4", URL: "https://x.example/b", SHA256: good}, // older
		{Version: "dev", URL: "https://x.example/b", SHA256: good},
		{Version: "v0.4.0", URL: "file:///etc/passwd", SHA256: good},
		{Version: "v0.4.0", URL: "https://x.example/b", SHA256: "abc"},
	} {
		if err := u.Start(c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	if ReadUpdateStatus(u.Dir) != nil {
		t.Fatal("a refused request wrote a status")
	}
	noLaunch := &SelfUpdater{Dir: t.TempDir(), Version: "v0.3.5"}
	if err := noLaunch.Start(UpdateRequest{Version: "v0.4.0", URL: "https://x.example/b", SHA256: good}); err == nil || !strings.Contains(err.Error(), "install command") {
		t.Fatalf("no launcher: %v", err)
	}
}

func TestAgentUpdateConcurrentBlocked(t *testing.T) {
	newBin := fakeBinary("v0.4.0")
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-arm64": newBin})
	f.block = make(chan struct{})
	u := &SelfUpdater{Dir: t.TempDir(), BinPath: installedBin(t, "v0.3.5"), Version: "v0.3.5", Launch: (&launches{}).launch}
	req := UpdateRequest{Version: "v0.4.0", URL: f.srv.URL + "/dl/veilon-linux-arm64", SHA256: sum(newBin)}
	if err := u.Start(req); err != nil {
		t.Fatal(err)
	}
	if err := u.Start(req); !errors.Is(err, ErrUpdateBusy) {
		t.Fatalf("second start: %v", err)
	}
	// a pending marker (a helper waiting for confirmation) blocks too
	close(f.block)
	waitStatus(t, u.Dir, false, UpdRestarting)
	for u.Busy() && ReadUpdateStatus(u.Dir).Phase != UpdRestarting {
		time.Sleep(10 * time.Millisecond)
	}
	if !u.Busy() {
		t.Fatal("restarting update not busy")
	}
}

// The full node-side path: stage, helper swaps and restarts, the new agent
// is reached by the master and confirms.
func TestAgentUpdateHelperSwapsAndConfirms(t *testing.T) {
	newBin := fakeBinary("v0.4.0")
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-arm64": newBin})
	dir := t.TempDir()
	bin := installedBin(t, "v0.3.5")
	l := &launches{}
	u := &SelfUpdater{Dir: dir, BinPath: bin, Version: "v0.3.5", Launch: l.launch, Timeout: 5 * time.Second}
	if err := u.Start(UpdateRequest{Version: "v0.4.0", URL: f.srv.URL + "/dl/veilon-linux-arm64", SHA256: sum(newBin)}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, dir, false, UpdRestarting)
	for l.n() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if c := l.calls[0]; c[0] != bin || strings.Join(c[1:], " ") != "node apply-update --dir "+dir {
		t.Fatalf("helper launch %v", c)
	}
	restarts := 0
	h := &Helper{Dir: dir, Poll: 20 * time.Millisecond, Restart: func(context.Context) error {
		restarts++
		// the restarted agent runs the new version; the master reaches it
		b, _ := os.ReadFile(bin)
		if string(b) != string(newBin) {
			t.Error("restart before the swap")
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			(&SelfUpdater{Dir: dir, Version: "v0.4.0"}).Confirm()
		}()
		return nil
	}}
	if err := h.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := ReadUpdateStatus(dir)
	if st.Phase != UpdDone || st.FinishedAt == nil || st.From != "v0.3.5" || st.Target != "v0.4.0" || restarts != 1 {
		t.Fatalf("status %+v restarts %d", st, restarts)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "update", "backup", "veilon")); string(b) != string(fakeBinary("v0.3.5")) {
		t.Fatal("backup is not the previous binary")
	}
	if fi, _ := os.Stat(bin); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
	for _, n := range []string{"pending.json", "confirmed.json", "plan.json", "new"} {
		if _, err := os.Stat(filepath.Join(dir, "update", n)); err == nil {
			t.Fatalf("%s left behind", n)
		}
	}
	// the old agent (same dir, old version) never confirms someone else's update
	(&SelfUpdater{Dir: dir, Version: "v0.3.5"}).Confirm()
}

// A new binary that never gets confirmed (crashes, cannot reach the
// master) is rolled back by the helper.
func TestAgentUpdateRollsBack(t *testing.T) {
	newBin := fakeBinary("v0.4.0")
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-arm64": newBin})
	dir := t.TempDir()
	bin := installedBin(t, "v0.3.5")
	u := &SelfUpdater{Dir: dir, BinPath: bin, Version: "v0.3.5", Launch: (&launches{}).launch, Timeout: time.Second}
	if err := u.Start(UpdateRequest{Version: "v0.4.0", URL: f.srv.URL + "/dl/veilon-linux-arm64", SHA256: sum(newBin)}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, dir, false, UpdRestarting)
	time.Sleep(50 * time.Millisecond)
	var restarted []string
	h := &Helper{Dir: dir, Poll: 20 * time.Millisecond, Restart: func(context.Context) error {
		b, _ := os.ReadFile(bin)
		restarted = append(restarted, string(b))
		// the old version (after rollback) must not take the marker
		(&SelfUpdater{Dir: dir, Version: "v0.3.5"}).Confirm()
		return nil
	}}
	err := h.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "did not reconnect") {
		t.Fatalf("apply: %v", err)
	}
	st := ReadUpdateStatus(dir)
	if st.Phase != UpdFailed || !st.RolledBack || !strings.Contains(st.Error, "previous version v0.3.5 was restored") {
		t.Fatalf("status %+v", st)
	}
	if b, _ := os.ReadFile(bin); string(b) != string(fakeBinary("v0.3.5")) {
		t.Fatal("previous binary not restored")
	}
	if len(restarted) != 2 || restarted[0] != string(newBin) || restarted[1] != string(fakeBinary("v0.3.5")) {
		t.Fatalf("restarts %d", len(restarted))
	}
	if _, err := os.Stat(filepath.Join(dir, "update", "pending.json")); err == nil {
		t.Fatal("pending marker left")
	}
	// a late confirmation after the rollback changes nothing
	(&SelfUpdater{Dir: dir, Version: "v0.4.0"}).Confirm()
	if _, err := os.Stat(filepath.Join(dir, "update", "confirmed.json")); err == nil {
		t.Fatal("late confirmation accepted")
	}
}

// --- master side ---------------------------------------------------------

// startUpdAgent serves an agent with a self-updater for node n.
func startUpdAgent(t *testing.T, r *rig, n *db.Node, dir, ver, arch string, l *launches) *Agent {
	t.Helper()
	a, _ := startAgentIn(t, r, n, dir)
	// startAgentIn serves a.Handler() already built; rebuild with the updater
	e := a.Enrollment
	a2 := &Agent{Enrollment: e, Dir: dir, Core: &fakeCore{running: true}, Version: ver, Arch: arch}
	if l != nil {
		a2.Updater = &SelfUpdater{Dir: dir, BinPath: installedBin(t, ver), Version: ver, Launch: l.launch}
	}
	ts := httptest.NewUnstartedServer(a2.Handler())
	tc, err := ServerTLS(e)
	if err != nil {
		t.Fatal(err)
	}
	ts.TLS = tc
	ts.StartTLS()
	t.Cleanup(ts.Close)
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	n.APIPort, _ = strconv.Atoi(port)
	r.gdb.Model(&db.Node{}).Where("id = ?", n.ID).Update("api_port", n.APIPort)
	r.m.Forget(n.ID)
	return a2
}

// restartAgent is the same node coming back (same enrollment and dir) as
// another version.
func restartAgent(t *testing.T, r *rig, n *db.Node, old *Agent, ver string) {
	t.Helper()
	a := &Agent{Enrollment: old.Enrollment, Dir: old.Dir, Core: &fakeCore{running: true}, Version: ver,
		Updater: &SelfUpdater{Dir: old.Dir, Version: ver}}
	ts := httptest.NewUnstartedServer(a.Handler())
	tc, _ := ServerTLS(old.Enrollment)
	ts.TLS = tc
	ts.StartTLS()
	t.Cleanup(ts.Close)
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	n.APIPort, _ = strconv.Atoi(port)
	r.gdb.Model(&db.Node{}).Where("id = ?", n.ID).Update("api_port", n.APIPort)
	r.m.Forget(n.ID)
}

func auditActions(t *testing.T, r *rig) []string {
	t.Helper()
	var rows []db.AuditLog
	r.gdb.Order("id").Find(&rows)
	out := []string{}
	for _, x := range rows {
		out = append(out, x.Action+" "+x.AdminName+" "+x.Target)
	}
	return out
}

func codeOf(err error) string {
	var ue *UpdateError
	if errors.As(err, &ue) {
		return ue.Code
	}
	if err == nil {
		return ""
	}
	return "other: " + err.Error()
}

func TestMasterNodeUpdateFlow(t *testing.T) {
	r := newRig(t)
	bins := map[string][]byte{"veilon-linux-amd64": fakeBinary("v0.4.0"), "veilon-linux-arm64": []byte("arm build")}
	f := newFeed(t, "v0.4.0", bins)
	r.m.Version, r.m.ReleaseAPI = "v0.4.0", f.srv.URL+"/releases"
	admin := &db.Admin{Username: "root"}
	r.gdb.Create(admin)
	ctx := context.Background()

	n := r.addNode(t, "DE")
	dir := t.TempDir()
	l := &launches{}
	a := startUpdAgent(t, r, n, dir, "v0.3.5", "amd64", l)
	r.m.SyncNode(ctx, n)
	v, _ := r.svc.Get(n.ID)
	if !v.State.CanUpdate || v.State.Arch != "amd64" || v.UpdateTo != "v0.4.0" || v.State.Update != nil {
		t.Fatalf("view %+v", v)
	}

	got, err := r.svc.StartUpdate(ctx, n.ID, admin, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if got.State.Update == nil || got.State.Update.Phase != UpdDownloading || got.State.Update.Target != "v0.4.0" || got.State.Update.By != "root" {
		t.Fatalf("started %+v", got.State.Update)
	}
	// only one update per node at a time
	if _, err := r.svc.StartUpdate(ctx, n.ID, admin, "203.0.113.9"); codeOf(err) != "in_progress" {
		t.Fatalf("second: %v", err)
	}
	waitStatus(t, dir, false, UpdRestarting)
	for l.n() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	// the agent was sent the amd64 asset and its checksum
	var plan UpdatePlan
	if err := readJSONFile(filepath.Join(dir, "update", "plan.json"), &plan); err != nil || plan.SHA256 != sum(bins["veilon-linux-amd64"]) || plan.Target != "v0.4.0" {
		t.Fatalf("plan %+v %v", plan, err)
	}
	r.m.SyncNode(ctx, n)
	if st := r.m.UpdateState(n.ID); st.Phase != UpdRestarting {
		t.Fatalf("phase %+v", st)
	}
	// the node comes back as v0.4.0
	restartAgent(t, r, n, a, "v0.4.0")
	r.m.SyncNode(ctx, n)
	v, _ = r.svc.Get(n.ID)
	if v.State.Update.Phase != UpdDone || v.State.Update.FinishedAt == nil || v.UpdateTo != "" || v.State.Version != "v0.4.0" {
		t.Fatalf("done %+v %+v", v.State.Update, v)
	}
	acts := auditActions(t, r)
	var first db.AuditLog
	r.gdb.Order("id").First(&first)
	if first.IP != "203.0.113.9" {
		t.Fatalf("audit ip %q", first.IP)
	}
	if len(acts) != 2 || acts[0] != "node.update.requested root DE: v0.3.5 → v0.4.0" || acts[1] != "node.update.ok root DE: v0.3.5 → v0.4.0" {
		t.Fatalf("audit %q", acts)
	}
	// up to date now; a node newer than the main server is never downgraded
	r.m.ups.lastReq = map[int64]time.Time{}
	if _, err := r.svc.StartUpdate(ctx, n.ID, admin, "203.0.113.9"); codeOf(err) != "up_to_date" {
		t.Fatalf("up to date: %v", err)
	}
	r.m.Version = "v0.3.9"
	r.m.ups.lastReq = map[int64]time.Time{}
	if _, err := r.svc.StartUpdate(ctx, n.ID, admin, "203.0.113.9"); codeOf(err) != "downgrade" {
		t.Fatalf("downgrade: %v", err)
	}
	r.m.Version = "dev"
	r.m.ups.lastReq = map[int64]time.Time{}
	if _, err := r.svc.StartUpdate(ctx, n.ID, admin, "203.0.113.9"); codeOf(err) != "not_release" {
		t.Fatalf("dev main: %v", err)
	}
	if f.hits.Load() != 1 {
		t.Fatalf("release list fetched %d times (cached)", f.hits.Load())
	}
}

func TestMasterNodeUpdateRefusals(t *testing.T) {
	r := newRig(t)
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-amd64": fakeBinary("v0.4.0")})
	r.m.Version, r.m.ReleaseAPI = "v0.4.0", f.srv.URL+"/releases"
	ctx := context.Background()

	// an agent before this release: no capability -> manual install hint
	old := r.addNode(t, "Old")
	startAgent(t, r, old) // plain agent, Version "test", no updater
	r.m.SyncNode(ctx, old)
	if v, _ := r.svc.Get(old.ID); v.State.CanUpdate {
		t.Fatal("old agent reported as updatable")
	}
	if _, err := r.svc.StartUpdate(ctx, old.ID, nil, "203.0.113.9"); codeOf(err) != "agent_unsupported" {
		t.Fatalf("old agent: %v", err)
	}

	// unknown architecture
	odd := r.addNode(t, "Odd")
	startUpdAgent(t, r, odd, t.TempDir(), "v0.3.5", "mips", &launches{})
	if _, err := r.svc.StartUpdate(ctx, odd.ID, nil, "203.0.113.9"); codeOf(err) != "arch_unknown" {
		t.Fatalf("arch: %v", err)
	}
	// rate limit: asked moments ago
	if _, err := r.svc.StartUpdate(ctx, odd.ID, nil, "203.0.113.9"); codeOf(err) != "rate_limited" {
		t.Fatalf("rate: %v", err)
	}

	// release without this node's asset
	arm := r.addNode(t, "Arm")
	startUpdAgent(t, r, arm, t.TempDir(), "v0.3.5", "arm64", &launches{})
	if _, err := r.svc.StartUpdate(ctx, arm.ID, nil, "203.0.113.9"); codeOf(err) != "release_unavailable" {
		t.Fatalf("no asset: %v", err)
	}

	// offline node
	gone := r.addNode(t, "Gone")
	if _, err := r.svc.StartUpdate(ctx, gone.ID, nil, "203.0.113.9"); codeOf(err) != "offline" {
		t.Fatalf("offline: %v", err)
	}
	if _, err := r.svc.StartUpdate(ctx, 999, nil, "203.0.113.9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// none of the refusals reached the audit log
	if a := auditActions(t, r); len(a) != 0 {
		t.Fatalf("audit %q", a)
	}
}

func TestMasterNodeUpdateFailureIsReported(t *testing.T) {
	r := newRig(t)
	bin := fakeBinary("v0.4.0")
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-amd64": bin})
	r.m.Version, r.m.ReleaseAPI = "v0.4.0", f.srv.URL+"/releases"
	ctx := context.Background()
	n := r.addNode(t, "DE")
	dir := t.TempDir()
	l := &launches{}
	a := startUpdAgent(t, r, n, dir, "v0.3.5", "amd64", l)
	if _, err := r.svc.StartUpdate(ctx, n.ID, &db.Admin{Username: "root"}, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, dir, false, UpdRestarting)
	// the helper rolls back (as in TestAgentUpdateRollsBack)
	h := &Helper{Dir: dir, Poll: 10 * time.Millisecond, Restart: func(context.Context) error { return nil }}
	var plan UpdatePlan
	_ = readJSONFile(filepath.Join(dir, "update", "plan.json"), &plan)
	plan.TimeoutSec = 1
	_ = writeJSONFile(filepath.Join(dir, "update", "plan.json"), plan)
	if err := h.Apply(ctx); err == nil {
		t.Fatal("expected rollback")
	}
	// the restored old agent reports the outcome
	restartAgent(t, r, n, a, "v0.3.5")
	r.m.SyncNode(ctx, n)
	st := r.m.UpdateState(n.ID)
	if st.Phase != UpdFailed || !st.RolledBack || !strings.Contains(st.Error, "did not reconnect") {
		t.Fatalf("state %+v", st)
	}
	acts := auditActions(t, r)
	if len(acts) != 2 || !strings.HasPrefix(acts[1], "node.update.failed root DE: v0.3.5 → v0.4.0: v0.4.0 did not reconnect") {
		t.Fatalf("audit %q", acts)
	}
	// a second round does not log it again
	r.m.SyncNode(ctx, n)
	if len(auditActions(t, r)) != 2 {
		t.Fatal("outcome logged twice")
	}
}

func TestUpdateOutdated(t *testing.T) {
	r := newRig(t)
	f := newFeed(t, "v0.4.0", map[string][]byte{"veilon-linux-amd64": fakeBinary("v0.4.0")})
	r.m.Version, r.m.ReleaseAPI = "v0.4.0", f.srv.URL+"/releases"
	ctx := context.Background()
	a := r.addNode(t, "A")
	startUpdAgent(t, r, a, t.TempDir(), "v0.3.5", "amd64", &launches{})
	b := r.addNode(t, "B") // already current
	startUpdAgent(t, r, b, t.TempDir(), "v0.4.0", "amd64", &launches{})
	c := r.addNode(t, "C") // old agent without the capability
	cAg := startUpdAgent(t, r, c, t.TempDir(), "v0.3.4", "amd64", nil)
	_ = cAg
	for _, n := range []*db.Node{a, b, c} {
		r.m.SyncNode(ctx, n)
	}
	res, err := r.m.UpdateOutdated(ctx, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || !res[0].OK || res[0].Name != "A" || res[1].OK || res[1].Code != "agent_unsupported" {
		t.Fatalf("results %+v", res)
	}
}
