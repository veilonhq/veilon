package veiloncore

// Run 105: traffic counters must only count bytes the remote side actually
// exchanged. Connected to a dead server, other apps push data into the tun
// and the counters must stay ~0 (and ProxyStatus says "unreachable");
// connected to a working server they move (positive control).

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestNetPushHelper is "another app" that pushes data at a public IP
// through the tun, ignoring every error (the server may be dead).
func TestNetPushHelper(t *testing.T) {
	if os.Getenv("VEILON_NETPUSH") != "1" {
		t.Skip("helper")
	}
	payload := make([]byte, 64<<10)
	for i := 0; i < 4; i++ {
		c, err := net.DialTimeout("tcp", ntPubIP+":80", 3*time.Second)
		if err != nil {
			t.Logf("push %d: dial: %v", i, err)
			continue
		}
		c.SetDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprintf(c, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n\r\n", len(payload))
		n, werr := c.Write(payload)
		m, _ := io.Copy(io.Discard, c)
		c.Close()
		t.Logf("push %d: wrote %d (%v), read %d", i, n, werr, m)
	}
	// UDP too (DNS-less datagrams).
	if u, err := net.Dial("udp", ntPubIP+":7"); err == nil {
		for i := 0; i < 20; i++ {
			u.Write(make([]byte, 1000))
		}
		u.Close()
	}
}

func ntPush(t *testing.T) string {
	cmd := exec.Command(os.Args[0], "-test.run", "^TestNetPushHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "VEILON_NETPUSH=1")
	cmd.SysProcAttr = ntHelperAttr()
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// deadServer accepts TCP and swallows everything, never answering (the
// worst case: a TCP-reachable host that is not a working proxy).
func deadServer(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(io.Discard, c); c.Close() }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestTunTrafficCountsOnlyRemote(t *testing.T) {
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	dp := deadServer(t)
	const pbk = "nOgeQ91pGXuAOrULdAGVkC0sBrgLuKWXGq7tj0gqFEA"
	live := ntNetnsServer(t)
	cases := []struct {
		name, link, core string
		dead             bool
	}{
		{"dead-reality-xray", fmt.Sprintf("vless://%s@127.0.0.1:%d?encryption=none&type=tcp&security=reality&sni=www.example.com&fp=chrome&pbk=%s&sid=6ba85179#dead", ntUUID, dp, pbk), "auto", true},
		{"dead-vless-singbox", fmt.Sprintf("vless://%s@127.0.0.1:%d?encryption=none&type=tcp&security=none#dead", ntUUID, dp), "sing-box", true},
		{"dead-ws-xray", fmt.Sprintf("vless://%s@127.0.0.1:%d?encryption=none&type=ws&security=none&path=%%2Fws#dead", ntUUID, dp), "xray", true},
		{"live-ws-tls-singbox", fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=ws&security=tls&path=%%2Fws&host=srv.test&sni=srv.test&allowInsecure=1#live", ntUUID, ntSrvIP, live), "sing-box", false},
		{"live-ws-tls-xray", fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=ws&security=tls&path=%%2Fws&host=srv.test&sni=srv.test&allowInsecure=1#live", ntUUID, ntSrvIP, live), "xray", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseLink(c.link)
			if err != nil {
				t.Fatal(err)
			}
			st := DefaultSettings()
			st.Core = c.core
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
			var out string
			if c.dead {
				out = ntPush(t)
			} else {
				out, _ = ntOtherApp(t)
				out += ntPush(t)
			}
			deadline := time.Now().Add(25 * time.Second)
			for ProxyStatus() == "" && time.Now().Before(deadline) {
				time.Sleep(200 * time.Millisecond)
			}
			up, down, ps := TrafficUp(), TrafficDown(), ProxyStatus()
			pushed := strings.Count(out, "wrote 65536")
			t.Logf("RESULT %s: up %d B, down %d B, proxy %q (other app pushed 64 KiB on %d connections)", c.name, up, down, ps, pushed)
			if c.dead {
				if up+down > 512 {
					t.Errorf("dead server: counters moved (up %d, down %d), want ~0", up, down)
				}
				if ps != "unreachable" {
					t.Errorf("dead server: proxy status %q, want unreachable", ps)
				}
				if pushed == 0 {
					t.Logf("helper output:\n%s", out)
				}
			} else {
				if down == 0 || up == 0 {
					t.Errorf("working server: counters did not move (up %d, down %d)", up, down)
				}
				if ps != "ok" {
					t.Errorf("working server: proxy status %q, want ok", ps)
				}
			}
		})
	}
}
