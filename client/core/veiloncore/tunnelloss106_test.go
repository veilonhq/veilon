package veiloncore

// 1.0.6: kill switch OFF must not cut the phone's internet when the tunnel
// drops. Until 1.0.6 nothing noticed a dead tunnel: the core session kept
// running (IsRunning true, so the app's health check never fired), the tun
// kept capturing every app's traffic and nothing came back - with the kill
// switch off the phone behaved as if it were on. ProxyStalledMs is the
// signal the app now watches: it stays 0 while the server answers and grows
// once connections through the proxy get no answer.

import (
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyStalledMsCounter(t *testing.T) {
	var now atomic.Int64
	now.Store(1_000_000_000_000)
	old := nowNano
	nowNano = func() int64 { return now.Load() }
	t.Cleanup(func() { nowNano = old; resetTraffic() })
	adv := func(d time.Duration) { now.Add(int64(d)) }

	resetTraffic()
	if got := ProxyStalledMs(); got != 0 {
		t.Fatalf("fresh session: %d, want 0", got)
	}
	// Direct connections never count.
	d := &connCounter{viaProxy: false}
	adv(10 * time.Second)
	d.up(100)
	if got := ProxyStalledMs(); got != 0 {
		t.Fatalf("direct only: %d, want 0", got)
	}
	// Healthy: a proxied connection opens and is answered → back to 0.
	newProxied := func() *connCounter {
		c := &connCounter{viaProxy: true}
		proxyWaitSince.CompareAndSwap(0, nowNano())
		return c
	}
	a := newProxied()
	adv(300 * time.Millisecond)
	if got := ProxyStalledMs(); got != 300 {
		t.Fatalf("waiting 300 ms: %d", got)
	}
	a.up(50)
	a.down(10)
	if got := ProxyStalledMs(); got != 0 {
		t.Fatalf("answered: %d, want 0", got)
	}
	// Tunnel dies: new connections never get an answer; the age is measured
	// from the OLDEST waiting one and keeps growing.
	b := newProxied()
	adv(4 * time.Second)
	_ = newProxied()
	adv(4 * time.Second)
	_ = newProxied()
	if got := ProxyStalledMs(); got != 8000 {
		t.Fatalf("dead tunnel: %d, want 8000", got)
	}
	// One answer (tunnel back) clears it.
	b.down(1)
	if got := ProxyStalledMs(); got != 0 {
		t.Fatalf("recovered: %d, want 0", got)
	}
	// A new session starts from 0.
	_ = newProxied()
	adv(time.Second)
	resetTraffic()
	if got := ProxyStalledMs(); got != 0 {
		t.Fatalf("reset: %d, want 0", got)
	}
}

// TestTunTunnelLossDetected: a working session (real tun, other app's
// traffic answered) then the server's link goes down mid-session (like the
// server-side per-IP DROP seen in Run 106). The session stays "running"
// (that is why the app never noticed), but ProxyStalledMs must grow; when
// the server comes back it returns to 0.
func TestTunTunnelLossDetected(t *testing.T) {
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	live := ntNetnsServer(t)
	link := func(state string) {
		if out, err := exec.Command("ip", "netns", "exec", ntNetns, "ip", "link", "set", "vs0", state).CombinedOutput(); err != nil {
			t.Fatalf("link %s: %v: %s", state, err, out)
		}
	}
	for _, core := range []string{"sing-box", "xray"} {
		t.Run(core, func(t *testing.T) {
			link("up")
			p, err := ParseLink(fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=ws&security=tls&path=%%2Fws&host=srv.test&sni=srv.test&allowInsecure=1#live", ntUUID, ntSrvIP, live))
			if err != nil {
				t.Fatal(err)
			}
			st := DefaultSettings()
			st.Core = core
			st.RemoteDNS = "udp://" + ntDNSIP
			st.DirectDNS = "127.0.0.1"
			st.AppPackage = "app.veilon"
			dir := t.TempDir()
			st.DataDir, st.TempDir = dir, dir
			st.normalizePaths()
			plat := &androidLikePlatform{t: t}
			defer plat.cleanup()
			if err := startGlobal(p, st, plat); err != nil {
				t.Fatalf("start: %v", err)
			}
			defer stopGlobal()

			if out, err := ntOtherApp(t); err != nil {
				t.Fatalf("other app before the drop: %v\n%s", err, out)
			}
			healthy := ProxyStalledMs()
			t.Logf("RESULT %s healthy: stalled %d ms, proxy %q, down %d B", core, healthy, ProxyStatus(), TrafficDown())
			if healthy > 1500 {
				t.Errorf("healthy session: stalled %d ms, want ~0", healthy)
			}

			link("down") // tunnel lost
			go ntPush(t) // apps keep trying
			deadline := time.Now().Add(20 * time.Second)
			var lost int64
			for time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				if lost = ProxyStalledMs(); lost >= 6000 {
					break
				}
			}
			running := func() bool { runMu.Lock(); defer runMu.Unlock(); return running != nil }()
			t.Logf("RESULT %s after drop: stalled %d ms, session still running %v", core, lost, running)
			if lost < 6000 {
				t.Errorf("tunnel lost: stalled %d ms, want >= 6000", lost)
			}

			link("up") // server back
			var back int64 = -1
			deadline = time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				out, err := ntOtherApp(t)
				back = ProxyStalledMs()
				if err == nil && back < 1500 {
					break
				}
				t.Logf("not yet back (stalled %d): %v %s", back, err, lastLine(out))
				time.Sleep(time.Second)
			}
			t.Logf("RESULT %s after recovery: stalled %d ms", core, back)
			if back < 0 || back >= 1500 {
				t.Errorf("recovered: stalled %d ms, want ~0", back)
			}
		})
	}
}

func lastLine(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return s[i+1:]
		}
	}
	return s
}
