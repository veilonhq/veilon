package veiloncore

// Ping accuracy against a link with injected latency.
//
// Topology (all local, no root needed):
//
//	urlTest(client sing-box / Xray) -> delayProxy (+50 ms each way = 100 ms RTT)
//	  -> VLESS+WS+TLS server (sing-box) -> HTTP server answering 204
//
// The new ping (median of 3 HTTP round trips over one kept-alive proxy
// connection) must read the link RTT (100 ms, +-15 ms). The full-handshake
// ping (old behaviour) pays TCP + TLS + WS + VLESS on every sample and must
// read >= 300 ms. The delay only exists in this test.

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
)

// delayProxy forwards TCP to target and delays every chunk by oneWay in each
// direction, preserving order (like netem delay on both ends).
type delayProxy struct {
	ln     net.Listener
	target string
	oneWay time.Duration
	wg     sync.WaitGroup
}

func newDelayProxy(t *testing.T, target string, oneWay time.Duration) *delayProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &delayProxy{ln: ln, target: target, oneWay: oneWay}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(c)
		}
	}()
	return p
}

func (p *delayProxy) port() int { return p.ln.Addr().(*net.TCPAddr).Port }

func (p *delayProxy) handle(c net.Conn) {
	// The TCP connect itself also costs one RTT.
	time.Sleep(2 * p.oneWay)
	u, err := net.Dial("tcp", p.target)
	if err != nil {
		c.Close()
		return
	}
	pipe := func(dst, src net.Conn) {
		type chunk struct {
			b  []byte
			at time.Time
		}
		q := make(chan chunk, 1024)
		go func() {
			for ch := range q {
				if d := time.Until(ch.at); d > 0 {
					time.Sleep(d)
				}
				if _, err := dst.Write(ch.b); err != nil {
					break
				}
			}
			dst.Close()
		}()
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				q <- chunk{append([]byte(nil), buf[:n]...), time.Now().Add(p.oneWay)}
			}
			if err != nil {
				close(q)
				return
			}
		}
	}
	go pipe(u, c)
	go pipe(c, u)
}

func (p *delayProxy) Close() { p.ln.Close() }

// pingServers starts a 204 HTTP server and a VLESS+WS+TLS server routing
// every TCP connection to it. Returns the VLESS port.
func pingServers(t *testing.T) (int, func()) {
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	go hs.Serve(hl)
	vp, _ := freePort()
	cert, key := ntSelfSigned(t)
	cfg := obj{
		"log": obj{"level": "error"},
		"inbounds": []any{obj{"type": "vless", "tag": "vin", "listen": "127.0.0.1", "listen_port": vp,
			"users":     []any{obj{"uuid": ntUUID}},
			"transport": obj{"type": "ws", "path": "/ws"},
			"tls": obj{"enabled": true, "server_name": "srv.test",
				"certificate": strings.Split(strings.TrimSpace(cert), "\n"), "key": strings.Split(strings.TrimSpace(key), "\n")}}},
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
	return vp, func() { srv.Close(); hs.Close() }
}

func TestPingRTTInjectedLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	vp, stop := pingServers(t)
	defer stop()
	const oneWay = 50 * time.Millisecond // 100 ms RTT
	dp := newDelayProxy(t, fmt.Sprintf("127.0.0.1:%d", vp), oneWay)
	defer dp.Close()
	link := fmt.Sprintf("vless://%s@127.0.0.1:%d?encryption=none&type=ws&security=tls&path=%%2Fws&host=srv.test&sni=srv.test&allowInsecure=1#ping", ntUUID, dp.port())
	// http:// test URL so the target needs no real CA; the proxy hop is TLS.
	const testURL = "http://ping.test/generate_204"
	for _, core := range []string{"sing-box", "xray"} {
		t.Run(core, func(t *testing.T) {
			p, err := ParseLink(link)
			if err != nil {
				t.Fatal(err)
			}
			st := DefaultSettings()
			st.Core = core
			dir := t.TempDir()
			st.DataDir, st.TempDir = dir, dir
			st.normalizePaths()
			newMs, err := urlTest(p, st, testURL, 15*time.Second)
			if err != nil {
				t.Fatalf("new ping: %v", err)
			}
			st.FullHandshakePing = true
			oldMs, err := urlTest(p, st, testURL, 15*time.Second)
			if err != nil {
				t.Fatalf("full-handshake ping: %v", err)
			}
			t.Logf("RESULT %s: link RTT 100 ms -> new ping %d ms, full-handshake ping %d ms", core, newMs, oldMs)
			if newMs < 85 || newMs > 115 {
				t.Errorf("new ping %d ms, want 100+-15", newMs)
			}
			// sing-box reads ~1 RTT for both (see below): allow scheduler
			// jitter of a few ms between the two samples.
			if oldMs+5 < newMs {
				t.Errorf("full-handshake ping %d ms < new ping %d ms", oldMs, newMs)
			}
			// Through Xray (SOCKS sidecar) every full-handshake sample pays
			// TCP + TLS + WS + VLESS: >= 3 RTT. sing-box's own URLTest
			// already restarts its clock after the dial, so over an http://
			// test URL it reads ~1 RTT there (an https:// URL adds the
			// target TLS handshake).
			if core == "xray" && oldMs < 300 {
				t.Errorf("full-handshake ping %d ms, want >= 300", oldMs)
			}
		})
	}
}

func TestMedianDuration(t *testing.T) {
	ms := time.Millisecond
	if got := medianDuration([]time.Duration{130 * ms, 98 * ms, 101 * ms}); got != 101*ms {
		t.Fatalf("median = %v", got)
	}
	if got := medianDuration([]time.Duration{4 * ms, 2 * ms}); got != 3*ms {
		t.Fatalf("median = %v", got)
	}
	_ = io.Discard
}
