package xray

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestReleasesCacheAndRateLimit(t *testing.T) {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	var hits atomic.Int32
	var limited atomic.Bool
	reset := time.Now().Add(30 * time.Minute).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if limited.Load() {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, `[
 {"tag_name":"v26.11.9","prerelease":false,"published_at":"2026-11-09T05:31:00Z","assets":[{"name":%[1]q}]},
 {"tag_name":"v26.11.1","draft":true,"assets":[{"name":%[1]q}]},
 {"tag_name":"v26.10.31","prerelease":true,"published_at":"2026-10-31T13:37:00Z","assets":[{"name":%[1]q}]},
 {"tag_name":"v26.10.20-rc1","assets":[{"name":%[1]q}]},
 {"tag_name":"v26.10.19-beta.2","assets":[{"name":%[1]q}]},
 {"tag_name":"v26.10.18-alpha","assets":[{"name":%[1]q}]},
 {"tag_name":"v26.10.17-pre","assets":[{"name":%[1]q}]},
 {"tag_name":"v26.10.1","assets":[{"name":"Other.zip"}]},
 {"tag_name":"nightly","assets":[{"name":%[1]q}]},
 {"tag_name":"v25.9.5","published_at":"2025-09-05T00:00:00Z","assets":[{"name":%[1]q}]}
]`, asset)
	}))
	defer srv.Close()
	var c releaseCache
	ctx := context.Background()
	now := time.Now()
	l, err := c.get(ctx, srv.Client(), srv.URL, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Releases) != 2 || l.Releases[0].Tag != "v26.11.9" || l.Releases[1].Tag != "v25.9.5" || l.Stale {
		t.Fatalf("bad list %+v", l)
	}
	if _, err := c.get(ctx, srv.Client(), srv.URL, false, now.Add(time.Minute)); err != nil || hits.Load() != 1 {
		t.Fatalf("expected cached answer, hits=%d err=%v", hits.Load(), err)
	}
	limited.Store(true)
	l, err = c.get(ctx, srv.Client(), srv.URL, true, now.Add(2*time.Minute))
	if err != nil || !l.Stale || l.RateLimitedUntil == nil || len(l.Releases) != 2 {
		t.Fatalf("rate limit should keep the stale list: %+v %v", l, err)
	}
	// While limited, GitHub is not asked again.
	h := hits.Load()
	if _, err := c.get(ctx, srv.Client(), srv.URL, true, now.Add(3*time.Minute)); err != nil || hits.Load() != h {
		t.Fatalf("should not refetch while limited (hits %d->%d) err=%v", h, hits.Load(), err)
	}
	var empty releaseCache
	if _, err := empty.get(ctx, srv.Client(), srv.URL, false, now); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func fakeXray(t *testing.T, path, version string, ok bool) {
	t.Helper()
	run := "exec sleep 60"
	if !ok {
		run = "echo boom; exit 1"
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = version ]; then echo \"Xray %s (fake)\"; exit 0; fi\nif [ \"$2\" = -test ]; then exit 0; fi\n%s\n", version, run)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPowerPersistsAndSwapRollsBack(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /bin/sh")
	}
	old := verifyWindow
	verifyWindow = 700 * time.Millisecond
	defer func() { verifyWindow = old }()

	gdb := testutil.DB(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "xray")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	fakeXray(t, bin, "1.0.0", true)
	opt := Options{DB: gdb, DataDir: dir, BinPath: bin, APIPort: 1, Debounce: 50 * time.Millisecond, StatsEvery: time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	m := NewManager(opt)
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	waitFor(t, "start", m.isRunning)

	if err := m.SetPower(ctx, false); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(); st.Running || !st.Stopped {
		t.Fatalf("should be stopped: %+v", st)
	}
	if db.GetSetting(gdb, KeyStopped) != "1" {
		t.Fatal("stopped state not persisted")
	}
	// A restart request keeps a switched-off core off.
	m.Restart()
	time.Sleep(200 * time.Millisecond)
	if m.isRunning() {
		t.Fatal("restart must not start a switched-off core")
	}
	cancel()
	<-done

	// A new supervisor (panel restart) keeps it off.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	m = NewManager(opt)
	done = make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	time.Sleep(300 * time.Millisecond)
	if st := m.Status(); st.Running || !st.Stopped || st.Version != "1.0.0" {
		t.Fatalf("should stay stopped after restart: %+v", st)
	}
	if err := m.SetPower(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "start after switch on", m.isRunning)

	// A broken binary is rolled back.
	bad := filepath.Join(dir, "bin", "stage-bad")
	fakeXray(t, bad, "2.0.0", false)
	rb, err := m.swapAndVerify(ctx, []swap{{src: bad, dst: bin}})
	if err == nil || !rb {
		t.Fatalf("expected rollback, rb=%v err=%v", rb, err)
	}
	waitFor(t, "running after rollback", m.isRunning)
	if v := detectVersion(bin); v != "1.0.0" {
		t.Fatalf("binary not restored, version %q", v)
	}

	// A good binary is kept.
	good := filepath.Join(dir, "bin", "stage-good")
	fakeXray(t, good, "3.0.0", true)
	if rb, err := m.swapAndVerify(ctx, []swap{{src: good, dst: bin}}); err != nil || rb {
		t.Fatalf("good swap failed: rb=%v err=%v", rb, err)
	}
	if st := m.Status(); !st.Running || st.Version != "3.0.0" {
		t.Fatalf("bad status after update %+v", st)
	}
	if v := detectVersion(bin + ".bak"); v != "1.0.0" {
		t.Fatalf("backup should hold the previous version, got %q", v)
	}
	if err := m.StartInstall("latest; rm -rf /"); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("want ErrBadVersion, got %v", err)
	}
	cancel()
	<-done
}
