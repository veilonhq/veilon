package cores

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func sleeper(t *testing.T, name string) *Supervisor {
	dir := t.TempDir()
	spec := Spec{
		Name: name,
		Bin:  func() string { return "/bin/sh" },
		Plan: func() ([]Instance, error) {
			return []Instance{{Key: "main", ConfigFile: filepath.Join(dir, "c"), Config: []byte("x"), Args: []string{"-c", "exec sleep 30"}}}, nil
		},
		Debounce: 50 * time.Millisecond,
	}
	return NewSupervisor(spec)
}

func waitUntil(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func runSup(s *Supervisor) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

// A switched-off engine stops, stays off through Restart and a new
// supervisor (panel restart), and comes back when switched on.
func TestSupervisorPowerPersists(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	gdb := testutil.DB(t)
	mk := func() *Supervisor {
		s := sleeper(t, "sing-box")
		s.spec.DB = gdb
		return s
	}
	s := mk()
	stop := runSup(s)
	waitUntil(t, "start", func() bool { return s.Status().Running })

	if err := s.SetPower(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Running || !st.Stopped || st.Instances != 0 {
		t.Fatalf("should be stopped: %+v", st)
	}
	if db.GetSetting(gdb, StoppedKey("sing-box")) != "1" {
		t.Fatal("stopped state not persisted")
	}
	s.Restart()
	time.Sleep(300 * time.Millisecond)
	if s.Status().Running {
		t.Fatal("restart must not start a switched-off engine")
	}
	stop()

	s = mk()
	stop = runSup(s)
	defer func() { stop() }()
	time.Sleep(300 * time.Millisecond)
	if st := s.Status(); st.Running || !st.Stopped {
		t.Fatalf("should stay stopped after panel restart: %+v", st)
	}
	if err := s.SetPower(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "start after switch on", func() bool { return s.Status().Running })
	if st := s.Status(); st.Stopped || db.GetSetting(gdb, StoppedKey("sing-box")) != "0" {
		t.Fatalf("should be on: %+v", st)
	}
}

type fakeEngine struct {
	*Supervisor
}

// Set.SetPower: "xray" touches only the primary engine, "all" every engine,
// and starting "all" brings back only what was stopped.
func TestSetPowerScopes(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	names := []string{"xray", "sing-box", "mtg", "amneziawg"}
	set := &Set{}
	for _, n := range names {
		set.Engines = append(set.Engines, fakeEngine{sleeper(t, n)})
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { set.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	allRunning := func() bool {
		for _, st := range set.Statuses() {
			if !st.Running {
				return false
			}
		}
		return true
	}
	waitUntil(t, "all running", allRunning)

	if _, err := set.SetPower(ctx, "bogus", false); err != ErrBadScope {
		t.Fatalf("want ErrBadScope, got %v", err)
	}
	ch, err := set.SetPower(ctx, ScopeXray, false)
	if err != nil || len(ch) != 1 || ch[0] != "xray" {
		t.Fatalf("xray stop: %v %v", ch, err)
	}
	if got := set.StoppedNames(); len(got) != 1 || got[0] != "xray" {
		t.Fatalf("stopped %v", got)
	}
	for _, st := range set.Statuses()[1:] {
		if !st.Running || st.Stopped {
			t.Fatalf("%s must keep running: %+v", st.Name, st)
		}
	}
	ch, err = set.SetPower(ctx, ScopeAll, false)
	if err != nil || len(ch) != 3 {
		t.Fatalf("all stop changed %v %v", ch, err)
	}
	if got := set.StoppedNames(); len(got) != 4 {
		t.Fatalf("stopped %v", got)
	}
	for _, st := range set.Statuses() {
		if st.Running {
			t.Fatalf("%s still running", st.Name)
		}
	}
	ch, err = set.SetPower(ctx, ScopeAll, true)
	if err != nil || len(ch) != 4 {
		t.Fatalf("all start changed %v %v", ch, err)
	}
	waitUntil(t, "all back", allRunning)
	if got := set.StoppedNames(); len(got) != 0 {
		t.Fatalf("stopped after start %v", got)
	}
}
