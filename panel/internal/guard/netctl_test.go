package guard

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Lines captured from Xray 26.10.31 stdout with the panel's log config
// ({"loglevel": "warning"}, no "access" key, so the access log goes to
// stdout where the manager feeds it to the guard).
func TestParseAccessLineRealXray(t *testing.T) {
	cases := []struct{ line, email, ip string }{
		{"2026/11/08 17:34:41.990991 from 10.231.0.2:56408 accepted tcp:10.231.0.1:18080 [inbound-9 >> direct] email: 7.alice", "7.alice", "10.231.0.2"},
		{"2026/11/08 17:34:41.990991 from tcp:[2001:db8::1]:50000 accepted udp:1.1.1.1:53 [inbound-1 -> direct] email: 12.bob", "12.bob", "2001:db8::1"},
		{"2026/11/08 17:34:41.990991 from [::ffff:5.6.7.8]:443 accepted tcp:x.com:443 [inbound-1 >> direct] email: 3.c", "3.c", "5.6.7.8"},
		// real Xray line for a link-local IPv6 client: the zone is dropped
		{"2026/11/10 17:22:41.990991 from [fe80::10b0:acff:fe10:cc5%eth0]:55832 accepted tcp:speed.cloudflare.com:80 [inbound-1 >> direct] email: 1.alice", "1.alice", "fe80::10b0:acff:fe10:cc5"},
	}
	for _, c := range cases {
		e, ip, ok := ParseAccessLine(c.line)
		if !ok || e != c.email || ip != c.ip {
			t.Errorf("%q: got %q %q %v", c.line, e, ip, ok)
		}
	}
	for _, bad := range []string{
		"2026/11/08 17:34:40.041507 [Warning] core: Xray 26.10.31 started",
		"2026/11/08 17:34:41.990991 from 127.0.0.1:5000 accepted tcp:a:1 [bridge >> direct] email: 7.alice",
		"2026/11/08 17:34:41.990991 from 1.2.3.4:5000 rejected  proxy/vless/encoding: invalid request user id",
	} {
		if _, _, ok := ParseAccessLine(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestTCScript(t *testing.T) {
	if TCScript("eth0", nil) != "" {
		t.Fatal("empty plan must build nothing")
	}
	s := TCScript("eth0", []Shape{{UserID: 7, DownKbps: 20000, UpKbps: 10000, IPs: []string{"10.0.0.2", "10.0.0.3", "fd00::2"}}})
	for _, want := range []string{
		"qdisc add dev eth0 root handle 1: htb default 1\n",
		"class add dev eth0 parent 1: classid 1:1 htb rate 10gbit quantum 60000\n",
		"class add dev eth0 parent 1: classid 1:10 htb rate 20000kbit ceil 20000kbit burst 125000 cburst 125000 quantum 60000\n",
		"filter add dev eth0 parent 1: protocol ip prio 1 u32 match ip dst 10.0.0.3/32 flowid 1:10\n",
		"filter add dev eth0 parent 1: protocol ipv6 prio 2 u32 match ip6 dst fd00::2/128 flowid 1:10\n",
		"qdisc add dev eth0 handle ffff: ingress\n",
		"u32 match ip src 10.0.0.2/32 action police rate 10000kbit burst 250000 mtu 64kb conform-exceed drop/ok index 16\n",
		"u32 match ip src 10.0.0.3/32 action police index 16\n",
		"u32 match ip6 src fd00::2/128 action police index 16\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, " del ") {
		t.Errorf("build script must not delete:\n%s", s)
	}
	if policeBurst(1000) != 65536 || htbBurst(100) != 16000 {
		t.Fatal("burst floors")
	}
}

func TestBanScriptRestartsTimeout(t *testing.T) {
	got := BanScript([]string{"1.2.3.4", "2001:db8::1", "bogus", "::ffff:5.6.7.8"}, 3*time.Minute)
	want := "add element inet veilon_guard ban4 { 1.2.3.4 timeout 180s, 5.6.7.8 timeout 180s }\n" +
		"delete element inet veilon_guard ban4 { 1.2.3.4, 5.6.7.8 }\n" +
		"add element inet veilon_guard ban4 { 1.2.3.4 timeout 180s, 5.6.7.8 timeout 180s }\n" +
		"add element inet veilon_guard ban6 { 2001:db8::1 timeout 180s }\n" +
		"delete element inet veilon_guard ban6 { 2001:db8::1 }\n" +
		"add element inet veilon_guard ban6 { 2001:db8::1 timeout 180s }\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if BanScript(nil, time.Minute) != "" {
		t.Fatal("no ips must produce no script")
	}
}

func TestFirewallScript(t *testing.T) {
	s := FirewallScript([]int{443, 8443, 443, 0})
	if !strings.HasPrefix(s, "add table inet veilon_guard\ndelete table inet veilon_guard\n") ||
		!strings.Contains(s, "add element inet veilon_guard ports { 443, 8443 }\n") ||
		!strings.Contains(s, "ip saddr @ban4 meta l4proto { tcp, udp } th dport @ports drop") {
		t.Fatalf("unexpected script:\n%s", s)
	}
}

type call struct{ cmd, stdin string }

func recordingGuard(calls *[]call, fail func(c call) bool) *Guard {
	lookPath = func(f string) (string, error) { return "/usr/sbin/" + f, nil }
	g := New(nil, "")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return now }
	g.Dev = "eth0"
	g.Run = func(stdin, name string, args ...string) (string, error) {
		c := call{name + " " + strings.Join(args, " "), stdin}
		*calls = append(*calls, c)
		if fail != nil && fail(c) {
			return "Command failed -:3", errors.New("exit status 1")
		}
		return "", nil
	}
	return g
}

func TestApplyTCDeletesThenBuilds(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, nil)
	shapes := []Shape{{UserID: 7, DownKbps: 1000, IPs: []string{"1.1.1.1"}}}
	g.applyTC(shapes)
	if len(calls) != 2 || calls[0].cmd != "tc -force -batch -" || calls[0].stdin != TCDeleteScript("eth0") ||
		calls[1].cmd != "tc -batch -" || calls[1].stdin != TCScript("eth0", shapes) {
		t.Fatalf("calls %+v", calls)
	}
	g.applyTC(shapes) // unchanged: nothing to do
	if len(calls) != 2 {
		t.Fatalf("re-applied an unchanged plan: %+v", calls)
	}
	g.applyTC(nil) // limit gone: only delete
	if len(calls) != 3 || calls[2].stdin != TCDeleteScript("eth0") {
		t.Fatalf("calls %+v", calls)
	}
}

func TestApplyTCFailureRetries(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, func(c call) bool { return c.cmd == "tc -batch -" })
	shapes := []Shape{{UserID: 7, UpKbps: 1000, IPs: []string{"1.1.1.1"}}}
	g.applyTC(shapes)
	// delete, failed build, cleanup delete
	if len(calls) != 3 || calls[2].stdin != TCDeleteScript("eth0") {
		t.Fatalf("calls %+v", calls)
	}
	g.applyTC(shapes)
	if len(calls) != 6 {
		t.Fatalf("a failed plan must be retried, calls %+v", calls)
	}
}

func TestBanFailureRetries(t *testing.T) {
	var calls []call
	fail := true
	g := recordingGuard(&calls, func(c call) bool { return fail && strings.Contains(c.stdin, "add element inet veilon_guard ban4") })
	g.fwReady = true // table exists; skips the nft lookup
	g.applyBans([]string{"9.9.9.9"})
	if len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
	fail = false
	g.fwReady = true
	g.applyBans([]string{"9.9.9.9"}) // must not be considered banned
	if len(calls) != 2 || !strings.Contains(calls[1].stdin, "9.9.9.9") {
		t.Fatalf("ban not retried: %+v", calls)
	}
}
