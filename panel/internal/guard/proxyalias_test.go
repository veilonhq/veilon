package guard

import (
	"testing"

	"github.com/veilonhq/veilon/panel/internal/userkey"
)

// Access log lines of HTTP/Mixed/SOCKS users with a custom proxy username
// name them by it: the IP log and the node collector map it to the user.
func TestCustomProxyUsernameAccessLog(t *testing.T) {
	t.Cleanup(func() { userkey.Set(nil) })
	userkey.Set(map[string]int64{"ali_px": 7})
	line := "2026/11/07 10:00:00.000000 from 1.2.3.4:50000 accepted tcp:example.com:443 [inbound-3 >> direct] email: ali_px"
	if userID("ali_px") != 7 || userID("7.alice") != 7 || userID("nobody") != 0 {
		t.Fatal("userID")
	}
	var c IPCollector
	c.ObserveLine(line)
	c.ObserveLine("2026/11/07 10:00:00.000000 from 5.6.7.8:1 accepted tcp:x:443 [inbound-3 >> direct] email: nobody")
	got := c.Drain()
	if len(got) != 1 || got[0].UserID != 7 || got[0].IP != "1.2.3.4" {
		t.Fatalf("collector %+v", got)
	}
}
