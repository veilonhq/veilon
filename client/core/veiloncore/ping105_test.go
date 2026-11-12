package veiloncore

// Run 105: VLESS Reality / plain VLESS latency through the real ping path
// (URLTestError, the API the app calls) against local stand-in servers.

import (
	"bufio"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
)

// pingStandIn is a local VLESS server whose TCP traffic all lands on a 204
// HTTP server. reality=true: VLESS + Reality (handshake target = a local
// TLS 1.3 server for "srv.test"); otherwise plain VLESS over TCP.
type pingStandIn struct {
	port     int
	pbk, sid string
	stop     func()
}

func newPingStandIn(t *testing.T, listen string, reality bool) *pingStandIn {
	t.Helper()
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	go hs.Serve(hl)
	s := &pingStandIn{}
	in := obj{"type": "vless", "tag": "vin", "listen": listen,
		"users": []any{obj{"uuid": ntUUID, "flow": map[bool]string{true: "xtls-rprx-vision"}[reality]}}}
	var target *httptest.Server
	if reality {
		target = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
		target.StartTLS()
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		s.pbk = base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
		s.sid = "a1b2c3d4"
		tp := target.Listener.Addr().(*net.TCPAddr).Port
		in["tls"] = obj{"enabled": true, "server_name": "srv.test",
			"reality": obj{"enabled": true, "handshake": obj{"server": "127.0.0.1", "server_port": tp},
				"private_key": base64.RawURLEncoding.EncodeToString(k.Bytes()), "short_id": []any{s.sid}}}
	}
	s.port, _ = freePort()
	in["listen_port"] = s.port
	cfg := obj{
		"log":       obj{"level": "warn"},
		"inbounds":  []any{in},
		"outbounds": []any{obj{"type": "direct", "tag": "direct"}},
		"route": obj{"rules": []any{
			obj{"network": "tcp", "action": "route-options", "override_address": "127.0.0.1",
				"override_port": hl.Addr().(*net.TCPAddr).Port},
		}, "final": "direct"},
	}
	ctx := boxContext(t.Context())
	opts, err := parseBoxOptions(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	s.stop = func() {
		srv.Close()
		hs.Close()
		if target != nil {
			target.Close()
		}
	}
	return s
}

func (s *pingStandIn) link(host string) string {
	if s.pbk != "" {
		return fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=tcp&security=reality&flow=xtls-rprx-vision&sni=srv.test&fp=chrome&pbk=%s&sid=%s#reality",
			ntUUID, host, s.port, s.pbk, s.sid)
	}
	return fmt.Sprintf("vless://%s@%s:%d?encryption=none&type=tcp&security=none#plain", ntUUID, host, s.port)
}

// appSettingsJSON is what the app sends (Settings.toCoreJson) for a ping.
func appSettingsJSON(t *testing.T, core string) string {
	st := DefaultSettings()
	st.Core = core
	st.AppPackage = "app.veilon"
	dir := t.TempDir()
	st.DataDir, st.TempDir = dir, dir
	b, _ := json.Marshal(st)
	return string(b)
}

// pingViaAPI pings through URLTestError (Kotlin "urlTest" → this).
func pingViaAPI(t *testing.T, link, core string) (int, string) {
	t.Helper()
	p, err := ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	pj, _ := json.Marshal(p)
	r := URLTestError(string(pj), appSettingsJSON(t, core), "http://ping.test/generate_204", 9000)
	var out struct {
		Delay int    `json:"delay"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(r), &out)
	if out.Delay == 0 && out.Error == "" {
		out.Error = "raw: " + r
	}
	return out.Delay, out.Error
}

func pingMatrix(t *testing.T, host, listen string) {
	for _, reality := range []bool{true, false} {
		s := newPingStandIn(t, listen, reality)
		defer s.stop()
		pingMatrixOn(t, s, host)
	}
}

func pingMatrixOn(t *testing.T, s *pingStandIn, host string) {
	reality := s.pbk != ""
	{
		kind := map[bool]string{true: "reality", false: "plain"}[reality]
		for _, core := range []string{"sing-box", "xray"} {
			t.Run(kind+"/"+core, func(t *testing.T) {
				d, e := pingViaAPI(t, s.link(host), core)
				if !reality && core == "xray" && host != "127.0.0.1" {
					// By design: Xray refuses cleartext VLESS to a public IP.
					if e != ErrXrayCleartextPublicIP.Error() {
						t.Errorf("plain/xray to a public IP: %d ms err %q, want %s", d, e, ErrXrayCleartextPublicIP)
					}
					return
				}
				t.Logf("RESULT ping %s/%s: %d ms err=%q", kind, core, d, e)
				if d <= 0 {
					t.Errorf("ping %s/%s: no response (%s)", kind, core, e)
				}
			})
		}
	}
}

// TestPingVlessRealityDisconnected: no VPN session.
func TestPingVlessRealityDisconnected(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	pingMatrix(t, "127.0.0.1", "127.0.0.1")
}

// --- nettest: stand-ins behind a veth at a public-looking address ---

const (
	npNetns  = "vping"
	npHostIP = "192.0.2.1"
	npSrvIP  = "192.0.2.2"
)

// TestNetPingServerHelper runs the Reality + plain stand-ins inside the
// netns and prints their parameters.
func TestNetPingServerHelper(t *testing.T) {
	if os.Getenv("VEILON_NETPINGSERVER") != "1" {
		t.Skip("helper")
	}
	r := newPingStandIn(t, "0.0.0.0", true)
	defer r.stop()
	p := newPingStandIn(t, "0.0.0.0", false)
	defer p.stop()
	fmt.Printf("PING_SERVERS %d %s %s %d\n", r.port, r.pbk, r.sid, p.port)
	io.Copy(io.Discard, os.Stdin)
}

func npNetnsServers(t *testing.T) (reality, plain *pingStandIn) {
	t.Helper()
	ntRun(t, "ip", "netns", "del", npNetns)
	ntRun(t, "ip", "link", "del", "vph0")
	for _, c := range [][]string{
		{"ip", "netns", "add", npNetns},
		{"ip", "link", "add", "vph0", "type", "veth", "peer", "name", "vps0"},
		{"ip", "link", "set", "vps0", "netns", npNetns},
		{"ip", "addr", "add", npHostIP + "/24", "dev", "vph0"},
		{"ip", "link", "set", "vph0", "up"},
		{"ip", "netns", "exec", npNetns, "ip", "addr", "add", npSrvIP + "/24", "dev", "vps0"},
		{"ip", "netns", "exec", npNetns, "ip", "link", "set", "vps0", "up"},
		{"ip", "netns", "exec", npNetns, "ip", "link", "set", "lo", "up"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", c, err, out)
		}
	}
	cmd := exec.Command("ip", "netns", "exec", npNetns, os.Args[0], "-test.run", "^TestNetPingServerHelper$", "-test.v", "-test.timeout", "30m")
	cmd.Env = append(os.Environ(), "VEILON_NETPINGSERVER=1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
		ntRun(t, "ip", "netns", "del", npNetns)
		ntRun(t, "ip", "link", "del", "vph0")
	})
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		r, p := &pingStandIn{stop: func() {}}, &pingStandIn{stop: func() {}}
		if _, err := fmt.Sscanf(sc.Text(), "PING_SERVERS %d %s %s %d", &r.port, &r.pbk, &r.sid, &p.port); err == nil {
			go io.Copy(io.Discard, stdout)
			return r, p
		}
	}
	t.Fatal("ping server helper did not report")
	return nil, nil
}

// TestTunPingWhileConnected: the app's ping path (URLTestError with the
// app's settings, default interface reported like the Kotlin service does)
// for VLESS Reality and plain VLESS, before and while a gVisor VPN session
// runs (Android model of tunroute_test.go: the app is excluded).
func TestTunPingWhileConnected(t *testing.T) {
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	r, p := npNetnsServers(t)
	if ifi, err := net.InterfaceByName("vph0"); err == nil {
		SetDefaultInterface(ifi.Name, int32(ifi.Index))
		defer SetDefaultInterface("", 0)
	}
	t.Run("disconnected", func(t *testing.T) {
		pingMatrixOn(t, r, npSrvIP)
		pingMatrixOn(t, p, npSrvIP)
	})
	for _, core := range []string{"sing-box", "xray"} {
		t.Run("connected-gvisor-"+core, func(t *testing.T) {
			sp, err := ParseLink(r.link(npSrvIP))
			if err != nil {
				t.Fatal(err)
			}
			st := DefaultSettings()
			st.Core = core
			st.AppPackage = "app.veilon"
			dir := t.TempDir()
			st.DataDir, st.TempDir = dir, dir
			st.normalizePaths()
			plat := &androidLikePlatform{t: t}
			defer plat.cleanup()
			if err := startGlobal(sp, st, plat); err != nil {
				t.Fatalf("start: %v", err)
			}
			defer stopGlobal()
			t.Logf("session stack %s, app inside VPN: %v", runningStack(), plat.selfIn)
			pingMatrixOn(t, r, npSrvIP)
			pingMatrixOn(t, p, npSrvIP)
		})
	}
}

var _ = strings.TrimSpace
