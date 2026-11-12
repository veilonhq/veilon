package veiloncore

// Live AmneziaWG through the app's real path: Start (startGlobal) + TUN in
// the Android model (gVisor, app excluded), then "another app" (uid 65534)
// resolves and fetches through the tun. Root + env gated; the conf is read
// from $VEILON_AWG_CONF and never logged (read logs through redact2.sh).
//
//	sudo VEILON_NETTEST=1 VEILON_AWG_CONF=/path/to/awg.conf ./veiloncore.test -test.run TestTunLiveAWG -test.v

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestTunLiveAWG(t *testing.T) {
	path := os.Getenv("VEILON_AWG_CONF")
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 || path == "" {
		t.Skip("set VEILON_NETTEST=1, VEILON_AWG_CONF and run as root")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseWGConf(string(b), "live-awg")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	st := DefaultSettings()
	st.LogLevel = "info"
	st.AppPackage = "app.veilon"
	dir := t.TempDir()
	st.DataDir, st.TempDir = dir, dir
	st.normalizePaths()

	start := time.Now()
	d, perr := urlTest(p, st, "https://www.gstatic.com/generate_204", 15*time.Second)
	t.Logf("PING awg: %d ms err %v (%.1fs)", d, perr, time.Since(start).Seconds())

	plat := &androidLikePlatform{t: t}
	defer plat.cleanup()
	before := ntLogSeq()
	if err := startGlobal(p, st, plat); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopGlobal()
	t.Logf("started, stack %s, app inside VPN: %v, tun mtu %d dns %s routes4 %d routes6 %d",
		runningStack(), plat.selfIn, plat.req.MTU, plat.req.DNSServer, len(plat.req.Inet4Routes), len(plat.req.Inet6Routes))
	var tun *awgTunnel
	runMu.Lock()
	if running != nil {
		for _, c := range running.closers {
			if x, ok := c.(*awgTunnel); ok {
				tun = x
			}
		}
	}
	runMu.Unlock()
	if tun == nil {
		t.Fatal("no AmneziaWG tunnel in the session")
	}
	hctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	herr := tun.waitHandshake(hctx)
	cancel()
	if herr != nil {
		t.Logf("HANDSHAKE: none within 15 s")
	} else {
		t.Logf("HANDSHAKE: ok (%.1fs after start)", time.Since(start).Seconds())
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestNetLiveClientHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "VEILON_NETLIVECLIENT=1")
	cmd.SysProcAttr = ntHelperAttr()
	ob, err := cmd.CombinedOutput()
	out := string(ob)
	t.Logf("other app:\n%s", strings.TrimSpace(out))
	t.Logf("core log:\n%s", ntLogsSince(before))
	if herr != nil {
		t.Errorf("RESULT awg: no handshake with the server")
	}
	if err != nil {
		t.Errorf("RESULT awg: other apps have NO internet (%v)", err)
	} else {
		t.Logf("RESULT awg: other app DNS + HTTP ok")
	}
}

// TestNetLiveClientHelper is "another app" (uid 65534) on the live
// internet: a DNS query to the tun DNS, then an HTTP GET to the answer.
func TestNetLiveClientHelper(t *testing.T) {
	if os.Getenv("VEILON_NETLIVECLIENT") != "1" {
		t.Skip("helper")
	}
	c := &dns.Client{Net: "udp", Timeout: 10 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("connectivitycheck.gstatic.com.", dns.TypeA)
	var ip string
	start := time.Now()
	for i := 0; i < 3 && ip == ""; i++ {
		r, _, err := c.Exchange(m, "172.19.0.2:53")
		if err != nil {
			t.Logf("dns try %d: %v", i, err)
			continue
		}
		for _, a := range r.Answer {
			if x, ok := a.(*dns.A); ok {
				ip = x.A.String()
			}
		}
	}
	if ip == "" {
		t.Fatal("DNS through tun failed")
	}
	t.Logf("DNS ok in %.1fs", time.Since(start).Seconds())
	start = time.Now()
	hc := &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{Proxy: nil}}
	req, _ := http.NewRequest("GET", "http://"+ip+"/generate_204", nil)
	req.Host = "connectivitycheck.gstatic.com"
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatalf("HTTP through tun failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("HTTP status %d", resp.StatusCode)
	}
	t.Logf("HTTP 204 ok in %.1fs", time.Since(start).Seconds())
}
