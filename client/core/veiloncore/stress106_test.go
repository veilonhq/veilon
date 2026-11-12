package veiloncore

// 1.0.6 crash hardening: rapid / concurrent Start-Stop, idempotent Stop,
// panics never cross the exported API (gomobile does not recover them;
// on Android such a panic kills the whole app).
//
// TestStartStopStress runs everywhere. Without root OpenTun cannot create a
// tun, so most Starts fail inside sing-box after the sidecars were opened
// (still exercises open / partial-close / Stop races). Under the root TUN
// harness (VEILON_NETTEST=1, see tunroute_test.go) the same loop runs with
// real tun devices: every Start brings a full gVisor session up.

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// stressPlatform behaves like VeilonVpnService: a fresh kernel-named tun per
// OpenTun, the previous fd closed afterwards (the core dup()s it).
type stressPlatform struct {
	mu       sync.Mutex
	last     *os.File
	opened   atomic.Int32
	failOpen atomic.Bool
	panicIn  atomic.Value // "", "open", "protect", "log"
}

func (p *stressPlatform) OpenTun(string) (int32, error) {
	if s, _ := p.panicIn.Load().(string); s == "open" {
		panic("test: OpenTun panicked")
	}
	if p.failOpen.Load() {
		return -1, errors.New("test: no tun")
	}
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return -1, err
	}
	var ifr [unix.IFNAMSIZ + 64]byte
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = unix.IFF_TUN | unix.IFF_NO_PI
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.TUNSETIFF, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		f.Close()
		return -1, errno
	}
	p.mu.Lock()
	old := p.last
	p.last = f
	p.mu.Unlock()
	if old != nil {
		old.Close()
	}
	p.opened.Add(1)
	return int32(f.Fd()), nil
}

func (p *stressPlatform) Protect(int32) bool {
	if s, _ := p.panicIn.Load().(string); s == "protect" {
		panic("test: Protect panicked")
	}
	return true
}

func (p *stressPlatform) OnLog(int32, string) {}

func (p *stressPlatform) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last != nil {
		p.last.Close()
		p.last = nil
	}
}

func stressProfiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, l := range []string{
		// sing-box only (no sidecar); server never dialed at start
		"socks5://u:p@127.0.0.1:9#sb",
		// Xray sidecar (TLS) chained into sing-box: exercises the closers
		"socks5://u:p@127.0.0.1:9?security=tls&sni=srv.test#xr",
	} {
		p, err := ParseLink(l)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(p)
		out = append(out, string(b))
	}
	return out
}

func stressSettings(t *testing.T) string {
	st := DefaultSettings()
	st.AppPackage = "app.veilon"
	st.BypassLAN = false
	dir := t.TempDir()
	st.DataDir, st.TempDir = dir, dir
	b, _ := json.Marshal(st)
	return string(b)
}

// fdKinds groups open fds by link target kind (diagnostics).
func fdKinds() map[string]int {
	m := map[string]int{}
	es, _ := os.ReadDir("/proc/self/fd")
	for _, e := range es {
		l, err := os.Readlink("/proc/self/fd/" + e.Name())
		if err != nil {
			continue
		}
		if i := strings.IndexAny(l, "["); i > 0 && !strings.HasPrefix(l, "anon_inode") {
			l = l[:i]
		}
		m[l]++
	}
	return m
}

func openFDs() int {
	es, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(es)
}

// withDeadline fails the test instead of hanging forever on a deadlock.
func withDeadline(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("deadlock: not finished within %v", d)
	}
}

func TestStartStopStress(t *testing.T) {
	defer func() { _ = Stop() }()
	profiles := stressProfiles(t)
	st := stressSettings(t)
	pl := &stressPlatform{}
	defer pl.closeAll()
	fd0 := openFDs()

	// 1) 60 rapid sequential toggles (the user's case), alternating profiles
	//    like a live switch, with Stop sometimes doubled.
	var started, failed int
	withDeadline(t, 4*time.Minute, func() {
		for i := 0; i < 60; i++ {
			if err := Start(profiles[i%2], st, pl); err != nil {
				failed++
				if strings.Contains(err.Error(), "internal error") {
					t.Errorf("toggle %d: panic surfaced: %v", i, err)
				}
			} else {
				started++
			}
			if i%3 == 0 {
				_ = Stop()
			}
			if err := Stop(); err != nil {
				t.Logf("toggle %d stop: %v", i, err)
			}
			if IsRunning() {
				t.Errorf("toggle %d: still running after Stop", i)
			}
		}
	})
	t.Logf("sequential: %d started, %d failed (tuns opened %d); fds now %d %v", started, failed, pl.opened.Load(), openFDs(), fdKinds())

	// 2) concurrent storm: Start / Stop / IsRunning / NetworkChanged /
	//    SetDefaultInterface from several goroutines at once.
	withDeadline(t, 4*time.Minute, func() {
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < 15; i++ {
					switch (w + i) % 4 {
					case 0, 1:
						_ = Start(profiles[(w+i)%2], st, pl)
					case 2:
						_ = Stop()
					case 3:
						_ = IsRunning()
						NetworkChanged()
						SetDefaultInterface("eth0", 2)
						SetDefaultInterface("", 0)
					}
				}
			}(w)
		}
		wg.Wait()
	})
	withDeadline(t, time.Minute, func() {
		if err := Stop(); err != nil {
			t.Logf("final stop: %v", err)
		}
		_ = Stop() // idempotent
	})
	if IsRunning() {
		t.Fatal("running after final Stop")
	}
	pl.closeAll()
	// fd leak check (loose: the runtime and sing-box keep a few around).
	time.Sleep(300 * time.Millisecond)
	// Known, not ours: gVisor's fdbased dispatcher (sagernet/gvisor
	// pkg/tcpip/link/fdbased, stopfd.New) never closes its stop eventfd, so
	// each started gVisor session leaks one eventfd (~1 fd per connect; the
	// Android app limit is far higher). Everything else must be closed -
	// in particular every tun fd the core dup()ed.
	kinds := fdKinds()
	other := 0
	for k, n := range kinds {
		if k != "anon_inode:[eventfd]" {
			other += n
		}
	}
	t.Logf("fds before %d, after %d: %v", fd0, openFDs(), kinds)
	if fd0 > 0 && other-fd0 > 10 {
		t.Errorf("fd leak (excluding gVisor stop eventfds): %d before, %d after: %v", fd0, other, kinds)
	}
	if kinds["/dev/net/tun"] > 0 {
		t.Errorf("tun fds still open after Stop: %v", kinds)
	}
}

// TestAPIPanicsAreRecovered: a panic inside Start (here: in the platform's
// OpenTun / Protect, called on the Start path) must come back as an error,
// and the core must stay usable.
func TestAPIPanicsAreRecovered(t *testing.T) {
	defer func() { _ = Stop() }()
	dir := t.TempDir()
	InitCrashLog(dir)
	st := stressSettings(t)
	pl := &stressPlatform{}
	defer pl.closeAll()
	pl.panicIn.Store("open")
	var err error
	withDeadline(t, time.Minute, func() { err = Start(stressProfiles(t)[0], st, pl) })
	if err == nil || !strings.Contains(err.Error(), "panic") && !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("Start with panicking OpenTun: err=%v (want a recovered panic)", err)
	}
	if IsRunning() {
		t.Fatal("running after a panicked Start")
	}
	if err := Stop(); err != nil {
		t.Fatalf("Stop after panic: %v", err)
	}
	if !strings.Contains(LastCrash(), "OpenTun panicked") {
		t.Errorf("crash file does not have the recovered panic")
	}
	// Still usable afterwards.
	pl.panicIn.Store("")
	pl.failOpen.Store(true)
	if err := Start(stressProfiles(t)[0], st, pl); err == nil {
		_ = Stop()
	} else if strings.Contains(err.Error(), "internal error") {
		t.Errorf("second Start: %v", err)
	}
	// Exported functions with bad input never panic.
	for _, f := range []func(){
		func() { _ = ParseText("\x00\xff") },
		func() { _ = ParseConf("[Interface]\nPrivateKey=", "x") },
		func() { _ = NormalizeProfile("null") },
		func() { _ = ProfileToLink("{}") },
		func() { _ = CoreFor("") },
		func() { _ = ResolveCoreFor("{", "{") },
		func() { _ = CheckProfile("{}", "{") },
		func() { _ = BuildConfig("[]", "") },
		func() { _ = URLTestError("{}", "", "", 1) },
		func() { _ = GetLogs(-1) },
		func() { _ = Start("", "", nil) },
	} {
		f()
	}
}

// TestSessionCloseIdempotent: closing a session twice closes each part
// once, and a panicking part does not stop the others from closing.
func TestSessionCloseIdempotent(t *testing.T) {
	InitCrashLog(t.TempDir())
	var a, b int
	s := &session{closers: []io.Closer{
		closerFunc(func() error { a++; return nil }),
		closerFunc(func() error { panic("test: closer panicked") }),
		closerFunc(func() error { b++; return nil }),
	}}
	if err := s.Close(); err == nil || !strings.Contains(err.Error(), "panic") {
		t.Errorf("first Close: %v (want the recovered panic as error)", err)
	}
	_ = s.Close()
	if a != 1 || b != 1 {
		t.Errorf("closers ran a=%d b=%d, want 1 and 1", a, b)
	}
	var nilSession *session
	if err := nilSession.Close(); err != nil {
		t.Errorf("nil session Close: %v", err)
	}
}
