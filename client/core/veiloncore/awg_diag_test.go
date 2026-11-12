package veiloncore

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// The v1.0.0 ping path for AmneziaWG: no handshake wait, the app's 9 s
// budget, two URL tests on one session. Kept as a regression check that the
// plain path works for every AWG version without the v1.0.1 handshake wait.
func TestAWGURLTestV100Path(t *testing.T) {
	for _, v := range awgVersions {
		t.Run(v.name, func(t *testing.T) {
			cli, srv, psk := newWGKey(t), newWGKey(t), newWGKey(t)
			port := startAWGServer(t, srv, cli, psk, v.server)
			p, err := ParseWGConf(clientConf(cli, srv, psk, fmt.Sprintf("127.0.0.1:%d", port), v.client), v.name)
			if err != nil {
				t.Fatal(err)
			}
			st, _ := parseSettings(`{"dataDir":"` + t.TempDir() + `"}`)
			st.MixedPort, st.LogLevel = 0, "error"
			t0 := time.Now()
			s, err := newSession(p, st, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ob, ok := s.box.Outbound().Outbound(tagProxy)
			if !ok {
				t.Fatal(errors.New("proxy outbound missing"))
			}
			d, err := measure(ob, "http://probe.test/generate_204", 9*time.Second, true)
			if err != nil || d <= 0 {
				t.Fatalf("%s: delay %d err %v after %v", v.name, d, err, time.Since(t0))
			}
			t.Logf("%s v1.0.0 path: %d ms, total %v", v.name, d, time.Since(t0))
		})
	}
}
