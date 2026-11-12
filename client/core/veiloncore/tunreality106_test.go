package veiloncore

// Run 106: VLESS REALITY through the full TUN path on the auto core (=
// Xray, prefersXray), Android model (gVisor, app excluded, other apps =
// uid 65534 helper). Offline against a local REALITY stand-in, and live
// against the user's subscription (env-gated; URL read from
// $VEILON_SUB_URL_FILE, never logged; read logs through redact2.sh).
//
//	sudo VEILON_NETTEST=1 ./veiloncore.test -test.run 'TestTunReality' -test.v

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ntRealityProfile starts a sing-box VLESS+REALITY server in front of the
// stand-in world and returns the matching client profile.
func ntRealityProfile(t *testing.T, w *ntWorld, flow string) *Profile {
	t.Helper()
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.StartTLS()
	t.Cleanup(target.Close)
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := freePort()
	ntServerBox(t, w, []any{obj{"type": "vless", "tag": "vin", "listen": "127.0.0.1", "listen_port": port,
		"users": []any{obj{"uuid": ntUUID, "flow": flow}},
		"tls": obj{"enabled": true, "server_name": "srv.test",
			"reality": obj{"enabled": true,
				"handshake":   obj{"server": "127.0.0.1", "server_port": target.Listener.Addr().(*net.TCPAddr).Port},
				"private_key": base64.RawURLEncoding.EncodeToString(k.Bytes()), "short_id": []any{"a1b2c3d4e5f60718"}}}}})
	link := fmt.Sprintf("vless://%s@127.0.0.1:%d?encryption=none&type=tcp&headerType=none&security=reality&flow=%s&sni=srv.test&fp=chrome&pbk=%s&sid=a1b2c3d4e5f60718&spx=%%2F#reality",
		ntUUID, port, flow, base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()))
	p, err := ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTunRealityXray(t *testing.T) {
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set VEILON_NETTEST=1 and run as root")
	}
	for _, flow := range []string{"", "xtls-rprx-vision"} {
		t.Run("flow="+flow, func(t *testing.T) {
			w := ntStartWorld(t)
			p := ntRealityProfile(t, w, flow)
			if c, err := ResolveCore(p, DefaultSettings()); err != nil || c != CoreXray {
				t.Fatalf("auto core for REALITY = %s %v, want xray", c, err)
			}
			ntRunSession(t, p, "")
		})
	}
}

// TestTunLiveReality connects to each REALITY profile of the real
// subscription with the app's settings (auto core) and lets "another app"
// resolve + fetch through the tun.
func TestTunLiveReality(t *testing.T) {
	path := os.Getenv("VEILON_SUB_URL_FILE")
	if os.Getenv("VEILON_NETTEST") != "1" || os.Geteuid() != 0 || path == "" {
		t.Skip("set VEILON_NETTEST=1, VEILON_SUB_URL_FILE and run as root")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	_ = json.Unmarshal([]byte(FetchSubscription(strings.TrimSpace(string(b)), `{"package":"app.veilon"}`, "", 20000)), &r)
	n := 0
	for _, raw := range r.Profiles {
		pj := appStoredProfile(t, raw)
		p, _ := decodeProfile(pj)
		if p.Security != "reality" {
			continue
		}
		n++
		t.Run(p.Name, func(t *testing.T) {
			st, err := parseSettings(appCoreSettingsJSON(t, "auto", false))
			if err != nil {
				t.Fatal(err)
			}
			st.normalizePaths()
			st.LogLevel = "info"
			c, _ := ResolveCore(p, st)
			res := URLTestError(pj, appCoreSettingsJSON(t, "auto", false), appPingURL, 9000)
			t.Logf("PING %q core %s: %s", p.Name, c, redactIPs(res))
			plat := &androidLikePlatform{t: t}
			defer plat.cleanup()
			before := ntLogSeq()
			start := time.Now()
			if err := startGlobal(p, st, plat); err != nil {
				t.Fatalf("start: %v", err)
			}
			defer stopGlobal()
			t.Logf("started core %s stack %s app inside VPN %v (%.1fs)", c, runningStack(), plat.selfIn, time.Since(start).Seconds())
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run", "^TestNetLiveClientHelper$", "-test.v")
			cmd.Env = append(os.Environ(), "VEILON_NETLIVECLIENT=1")
			cmd.SysProcAttr = ntHelperAttr()
			ob, err := cmd.CombinedOutput()
			time.Sleep(500 * time.Millisecond)
			t.Logf("other app:\n%s", strings.TrimSpace(string(ob)))
			t.Logf("proxy status %q, traffic up %d down %d", ProxyStatus(), TrafficUp(), TrafficDown())
			t.Logf("core log:\n%s", redactIPs(ntLogsSince(before)))
			if err != nil {
				t.Errorf("RESULT %q: other apps have NO internet (%v)", p.Name, err)
			} else {
				t.Logf("RESULT %q: other app DNS + HTTP ok", p.Name)
			}
		})
	}
	if n == 0 {
		t.Fatal("no REALITY profiles in the subscription")
	}
}
