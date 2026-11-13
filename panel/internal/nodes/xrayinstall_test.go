package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// fakeXray is a script standing in for an Xray binary: "version" prints
// ver, "run -test" passes, "run" stays up (good) or exits at once (bad).
func fakeXray(ver string, good bool) []byte {
	run := "exec sleep 60"
	if !good {
		run = "echo 'Failed to start: boom' >&2; exit 23"
	}
	return []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo \"Xray " + ver + " (Xray, Penetrates Everything.) Custom (go1.25 linux/arm64)\"; exit 0; fi\n" +
		"if [ \"$2\" = -test ]; then exit 0; fi\n" + run + "\n")
}

func writeExec(t *testing.T, p string, b []byte) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// ProcCore.SwapBinary: a new binary that stays up is kept; one that dies is
// replaced by the previous binary, which runs again.
func TestProcCoreSwapBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray", "xray")
	writeExec(t, bin, fakeXray("25.6.8", true))
	if err := os.WriteFile(filepath.Join(dir, "xray.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &ProcCore{Bin: bin, Dir: dir}
	defer c.Stop()
	if err := c.start(); err != nil {
		t.Fatal(err)
	}
	if run, ver, _ := c.Status(); !run || ver != "25.6.8" {
		t.Fatalf("start: running=%v ver=%q", run, ver)
	}
	ctx := context.Background()

	// good
	nb := filepath.Join(dir, "stage1", "xray")
	writeExec(t, nb, fakeXray("26.10.31", true))
	if err := c.TestWith(ctx, nb); err != nil {
		t.Fatal(err)
	}
	rb, err := c.SwapBinary(ctx, nb, 300*time.Millisecond)
	if err != nil || rb {
		t.Fatalf("good swap: rb=%v err=%v", rb, err)
	}
	if run, ver, _ := c.Status(); !run || ver != "26.10.31" {
		t.Fatalf("after good swap: running=%v ver=%q", run, ver)
	}
	if b, _ := os.ReadFile(bin + ".bak"); !bytes.Equal(b, fakeXray("25.6.8", true)) {
		t.Fatal("previous binary not kept as xray.bak")
	}

	// bad: dies at start -> rolled back to 26.10.31
	nb2 := filepath.Join(dir, "stage2", "xray")
	writeExec(t, nb2, fakeXray("26.11.1", false))
	rb, err = c.SwapBinary(ctx, nb2, 500*time.Millisecond)
	if err == nil || !rb {
		t.Fatalf("bad swap: rb=%v err=%v", rb, err)
	}
	if !strings.Contains(err.Error(), "previous version was restored") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("bad swap error: %v", err)
	}
	if run, ver, _ := c.Status(); !run || ver != "26.10.31" {
		t.Fatalf("after rollback: running=%v ver=%q", run, ver)
	}
	if b, _ := os.ReadFile(bin); !bytes.Equal(b, fakeXray("26.10.31", true)) {
		t.Fatal("binary not restored")
	}
	// it stays up (no late restart of the broken binary)
	time.Sleep(600 * time.Millisecond)
	if run, ver, _ := c.Status(); !run || ver != "26.10.31" {
		t.Fatalf("later: running=%v ver=%q", run, ver)
	}
}

// binCore is a fakeCore whose binary can be swapped (an agent with
// CapXrayInstall).
type binCore struct {
	*fakeCore
	dir      string
	mu       sync.Mutex
	testErr  error
	swapFail bool
	swaps    int
}

func (b *binCore) swapCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.swaps
}
func (b *binCore) BinPath() string { return filepath.Join(b.dir, "xray", "xray") }
func (b *binCore) TestWith(context.Context, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.testErr
}
func (b *binCore) SwapBinary(_ context.Context, newBin string, _ time.Duration) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.swaps++
	if b.swapFail {
		return true, errors.New("the new xray did not stay up (xray exited: exit status 23); the previous version was restored")
	}
	v := xrayVersion(newBin)
	b.fakeCore.mu.Lock()
	b.fakeCore.version = v
	b.fakeCore.mu.Unlock()
	return false, nil
}

func waitXray(t *testing.T, m *Manager, id int64) *UpdateStatus {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if st := m.XrayUpdateState(id); st != nil && st.Final() {
			return st
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("xray install did not finish: %+v", m.XrayUpdateState(id))
	return nil
}

func TestNodeXrayInstall(t *testing.T) {
	old := XrayFollowEvery
	XrayFollowEvery = 40 * time.Millisecond
	defer func() { XrayFollowEvery = old }()

	r := newRig(t)
	ml, err := inbounds.GenerateMLDSA65("")
	if err != nil {
		t.Fatal(err)
	}
	svc := &inbounds.Service{DB: r.gdb}
	v := inbounds.Defaults()
	v.Remark, v.Protocol, v.Port, v.Security = "pq", "vless", 4443, "reality"
	v.Reality = &inbounds.Reality{Dest: "dl.google.com"}
	v.Advanced = &inbounds.Advanced{Reality: &inbounds.RealityExtra{MLDSA65Seed: ml.Server}}
	pq, err := svc.Create(&v)
	if err != nil {
		t.Fatal(err)
	}
	var bc *binCore
	r.wrap = func(f *fakeCore) Core {
		f.version = "25.6.8"
		bc = &binCore{fakeCore: f, dir: t.TempDir()}
		return bc
	}
	n := r.addNode(t, "n1", 1, pq.ID)
	served := "26.10.31"
	var dlMu sync.Mutex
	r.agentHook = func(a *Agent) {
		a.XrayDownload = func(_ context.Context, dir, version string) error {
			dlMu.Lock()
			defer dlMu.Unlock()
			writeExec(t, filepath.Join(dir, "xray"), fakeXray(served, true))
			return nil
		}
	}
	_, core := startAgent(t, r, n)
	ctx := context.Background()
	admin := &db.Admin{Username: "root"}
	r.gdb.Create(admin)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st := r.m.Status(n)
	if !st.CanUpdateXray || len(st.XrayTooOld) != 1 {
		t.Fatalf("before: can=%v too_old=%+v", st.CanUpdateXray, st.XrayTooOld)
	}
	pushed := func() []int64 {
		var c struct {
			Inbounds []int64 `json:"inbounds"`
		}
		core.mu.Lock()
		defer core.mu.Unlock()
		_ = json.Unmarshal(core.applied[len(core.applied)-1], &c)
		return c.Inbounds
	}
	if p := pushed(); len(p) != 1 {
		t.Fatalf("old node pushed %v", p)
	}

	code := func(err error) string {
		var ue *UpdateError
		if errors.As(err, &ue) {
			return ue.Code
		}
		return ""
	}
	if _, err := r.m.StartXrayUpdate(ctx, n, "latest", admin, ""); code(err) != "bad_version" {
		t.Fatalf("bad version: %v", err)
	}
	if _, err := r.m.StartXrayUpdate(ctx, n, "25.6.8", admin, ""); code(err) != "up_to_date" {
		t.Fatalf("same version: %v", err)
	}
	r.m.upd().mu.Lock()
	delete(r.m.ups.xlast, n.ID)
	r.m.upd().mu.Unlock()

	// success: 25.6.8 -> 26.10.31, the left-out inbound comes back
	got, err := r.m.StartXrayUpdate(ctx, n, "26.10.31", admin, "1.2.3.4")
	if err != nil || got.Target != "v26.10.31" || got.From != "25.6.8" {
		t.Fatalf("start: %+v %v", got, err)
	}
	if _, err := r.m.StartXrayUpdate(ctx, n, "v26.10.31", admin, ""); code(err) != "in_progress" && code(err) != "rate_limited" {
		t.Fatalf("second start: %v", err)
	}
	fin := waitXray(t, r.m, n.ID)
	if fin.Phase != UpdDone || fin.Error != "" || fin.By != "root" {
		t.Fatalf("final %+v", fin)
	}
	// "done" is published with the round that saw the new version
	if st := r.m.Status(n); st.XrayVersion != "26.10.31" || len(st.XrayTooOld) != 0 {
		t.Fatalf("done published before the new version: %+v", st)
	}
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st = r.m.Status(n)
	if st.XrayVersion != "26.10.31" || len(st.XrayTooOld) != 0 || st.XrayUpdate == nil || st.XrayUpdate.Phase != UpdDone {
		t.Fatalf("after: %+v", st)
	}
	if p := pushed(); len(p) != 2 {
		t.Fatalf("inbound did not come back: %v", p)
	}
	var logs []db.AuditLog
	r.gdb.Where("action LIKE ?", "node.xray.%").Order("id").Find(&logs)
	if len(logs) != 2 || logs[0].Action != "node.xray.requested" || logs[1].Action != "node.xray.ok" || !strings.Contains(logs[1].Target, "25.6.8 → v26.10.31") {
		t.Fatalf("audit %+v", logs)
	}

	clear := func() {
		r.m.upd().mu.Lock()
		delete(r.m.ups.xlast, n.ID)
		r.m.upd().mu.Unlock()
	}
	// wrong version in the download: refused before the swap
	clear()
	dlMu.Lock()
	served = "26.9.6"
	dlMu.Unlock()
	if _, err := r.m.StartXrayUpdate(ctx, n, "v26.11.1", admin, ""); err != nil {
		t.Fatal(err)
	}
	fin = waitXray(t, r.m, n.ID)
	if fin.Phase != UpdFailed || !strings.Contains(fin.Error, "expected 26.11.1") || fin.RolledBack || bc.swapCount() != 1 {
		t.Fatalf("wrong version: %+v swaps=%d", fin, bc.swapCount())
	}
	// the current config is rejected by the new binary: no swap
	clear()
	dlMu.Lock()
	served = "26.11.1"
	dlMu.Unlock()
	bc.mu.Lock()
	bc.testErr = errors.New("unknown field")
	bc.mu.Unlock()
	if _, err := r.m.StartXrayUpdate(ctx, n, "v26.11.1", admin, ""); err != nil {
		t.Fatal(err)
	}
	fin = waitXray(t, r.m, n.ID)
	if fin.Phase != UpdFailed || !strings.Contains(fin.Error, "rejects the node's current config") || bc.swapCount() != 1 {
		t.Fatalf("config test: %+v swaps=%d", fin, bc.swapCount())
	}
	// it does not stay up: rolled back
	clear()
	bc.mu.Lock()
	bc.testErr, bc.swapFail = nil, true
	bc.mu.Unlock()
	if _, err := r.m.StartXrayUpdate(ctx, n, "v26.11.1", admin, ""); err != nil {
		t.Fatal(err)
	}
	fin = waitXray(t, r.m, n.ID)
	if fin.Phase != UpdFailed || !fin.RolledBack || bc.swapCount() != 2 {
		t.Fatalf("rollback: %+v swaps=%d", fin, bc.swapCount())
	}
	if _, ver, _ := core.Status(); ver != "26.10.31" {
		t.Fatalf("version after rollback %q", ver)
	}
	var last db.AuditLog
	r.gdb.Where("action = ?", "node.xray.failed").Order("id desc").Limit(1).Find(&last)
	if !strings.Contains(last.Target, "(rolled back)") {
		t.Fatalf("failed audit %+v", last)
	}
}

// An agent without the capability is told apart (needs an agent update).
func TestNodeXrayInstallOldAgent(t *testing.T) {
	r := newRig(t)
	n := r.addNode(t, "old", 1)
	startAgent(t, r, n)
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if st := r.m.Status(n); st.CanUpdateXray {
		t.Fatal("old agent announced xray installs")
	}
	_, err := r.m.StartXrayUpdate(ctx, n, "v26.10.31", nil, "")
	var ue *UpdateError
	if !errors.As(err, &ue) || ue.Code != "agent_unsupported" {
		t.Fatalf("err %v", err)
	}
}
