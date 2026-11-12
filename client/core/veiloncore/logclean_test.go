package veiloncore

import (
	"testing"

	"github.com/sagernet/sing-box/log"
)

func TestCleanCoreLog(t *testing.T) {
	in := "\x1b[31mERROR\x1b[0m[0190] [\x1b[38;5;84m934998419\x1b[0m 11.53s] connection: connection download closed: close tcp 172.19.0.1:43268->198.51.100.7:8443: endpoint not connected"
	l, m := cleanCoreLog(log.LevelError, in)
	if l != log.LevelDebug {
		t.Errorf("level %v, want debug", l)
	}
	if want := "ERROR[0190] [934998419 11.53s] connection: connection download closed: close tcp 172.19.0.1:43268->198.51.100.7:8443: endpoint not connected"; m != want {
		t.Errorf("got %q", m)
	}
	for _, s := range []string{"connection: connection upload closed: use of closed network connection", "connection: connection download closed: read tcp: connection reset by peer"} {
		if l, _ := cleanCoreLog(log.LevelError, s); l != log.LevelDebug {
			t.Errorf("%q not demoted", s)
		}
	}
	// Real errors stay errors.
	if l, _ := cleanCoreLog(log.LevelError, "outbound/vless[proxy]: dial tcp 198.51.100.7:443: i/o timeout"); l != log.LevelError {
		t.Errorf("dial error demoted")
	}
	if l, _ := cleanCoreLog(log.LevelError, "connection: open connection to 1.1.1.1:443 using outbound/socks[proxy]: connection reset by peer"); l != log.LevelError {
		t.Errorf("open-connection reset (server refused) must stay an error")
	}
}
