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
	if TCScript("eth0", []Shape{{UserID: 7, UpKbps: 1000}}) != "" {
		t.Fatal("an upload-only plan needs no tc tree")
	}
	s := TCScript("eth0", []Shape{{UserID: 7, DownKbps: 20000, UpKbps: 10000, IPs: []string{"10.0.0.2", "fd00::2"}}})
	for _, want := range []string{
		"qdisc add dev eth0 root handle 1: htb default 1\n",
		"class add dev eth0 parent 1: classid 1:1 htb rate 10gbit quantum 60000\n",
		"class add dev eth0 parent 1: classid 1:10 htb rate 20000kbit ceil 20000kbit burst 125000 cburst 125000 quantum 60000\n",
		"filter add dev eth0 parent 1: protocol all prio 1 handle 0x5e000007 fw flowid 1:10\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	// Never by client IP: other users behind the same address must not match.
	for _, bad := range []string{" del ", "u32", "10.0.0.2", "ingress", "police"} {
		if strings.Contains(s, bad) {
			t.Errorf("unexpected %q in\n%s", bad, s)
		}
	}
	if policeBurst(1000) != 65536 || htbBurst(100) != 16000 {
		t.Fatal("burst floors")
	}
}

func TestShapeScript(t *testing.T) {
	s := ShapeScript([]Shape{{UserID: 7, DownKbps: 20000, UpKbps: 10000}, {UserID: 9, DownKbps: 500}})
	for _, want := range []string{
		"add table inet veilon_shape\ndelete table inet veilon_shape\n",
		"map conn4 { type ipv4_addr . inet_service : mark; flags timeout; timeout 60s; }",
		"ct mark 0 meta l4proto tcp ct mark set ip saddr . tcp sport map @conn4",
		"ct mark 0 meta l4proto udp ct mark set ip6 daddr . udp dport map @conn6",
		"ct mark 0x5e000007 limit rate over 1250000 bytes/second burst 250000 bytes drop",
		"meta mark 0 ct mark and 0xff000000 == 0x5e000000 meta mark set ct mark",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "0x5e000009 limit") {
		t.Error("user 9 has no upload limit")
	}
	if _, ok := ShapeMark(1 << 24); ok {
		t.Error("ids above 24 bits must not get a mark")
	}
}

func TestConnElemScript(t *testing.T) {
	got := ConnElemScript([]Conn{{7, "1.2.3.4", 5000}, {7, "1.2.3.4", 5000}, {9, "::ffff:1.2.3.4", 5001}, {7, "2001:db8::1", 6000}, {7, "bogus", 1}, {7, "1.1.1.1", 0}})
	want := "add element inet veilon_shape conn4 { 1.2.3.4 . 5000 : 0x5e000007, 1.2.3.4 . 5001 : 0x5e000009 }\n" +
		"delete element inet veilon_shape conn4 { 1.2.3.4 . 5000, 1.2.3.4 . 5001 }\n" +
		"add element inet veilon_shape conn4 { 1.2.3.4 . 5000 : 0x5e000007, 1.2.3.4 . 5001 : 0x5e000009 }\n" +
		"add element inet veilon_shape conn6 { 2001:db8::1 . 6000 : 0x5e000007 }\n" +
		"delete element inet veilon_shape conn6 { 2001:db8::1 . 6000 }\n" +
		"add element inet veilon_shape conn6 { 2001:db8::1 . 6000 : 0x5e000007 }\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
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
	if len(calls) != 3 || calls[0].cmd != "tc -force -batch -" || calls[0].stdin != TCDeleteScript("eth0") ||
		calls[1].cmd != "nft -f -" || calls[1].stdin != ShapeScript(shapes) ||
		calls[2].cmd != "tc -batch -" || calls[2].stdin != TCScript("eth0", shapes) {
		t.Fatalf("calls %+v", calls)
	}
	g.applyTC(shapes) // unchanged: nothing to do
	if len(calls) != 3 {
		t.Fatalf("re-applied an unchanged plan: %+v", calls)
	}
	// New live IPs of the same user do not rebuild anything.
	g.applyTC([]Shape{{UserID: 7, DownKbps: 1000, IPs: []string{"1.1.1.1", "2.2.2.2"}}})
	if len(calls) != 3 {
		t.Fatalf("rebuilt for an IP change: %+v", calls)
	}
	g.applyTC(nil) // limit gone: only delete
	if len(calls) != 5 || calls[3].stdin != TCDeleteScript("eth0") || calls[4].stdin != ShapeDeleteScript() {
		t.Fatalf("calls %+v", calls)
	}
}

func TestApplyTCFailureRetries(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, func(c call) bool {
		return c.cmd == "nft -f -" && strings.Contains(c.stdin, "table inet veilon_shape {")
	})
	shapes := []Shape{{UserID: 7, UpKbps: 1000, IPs: []string{"1.1.1.1"}}}
	g.applyTC(shapes)
	// delete, failed build, cleanup deletes (tc + nft)
	if len(calls) != 4 || calls[2].stdin != TCDeleteScript("eth0") || calls[3].stdin != ShapeDeleteScript() {
		t.Fatalf("calls %+v", calls)
	}
	g.ObserveLine(accessLine("1.1.1.1", 4000, "7.a"))
	g.FlushMarks() // table is not there: nothing to mark
	if len(calls) != 4 {
		t.Fatalf("marked without a table: %+v", calls)
	}
	g.applyTC(shapes)
	if len(calls) != 8 {
		t.Fatalf("a failed plan must be retried, calls %+v", calls)
	}
}

// A user whose limit starts while connected gets the connections the
// access log already showed; a later connection is marked within
// MarkDelay without rebuilding the rules.
func TestShapingMarksKnownAndNewConnections(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, nil)
	g.MarkDelay = time.Hour
	g.ObserveLine(accessLine("1.1.1.1", 4000, "7.a"))
	g.ObserveLine(accessLine("1.1.1.1", 4001, "8.b"))
	g.applyTC([]Shape{{UserID: 7, DownKbps: 1000}})
	if len(calls) != 3 || !strings.Contains(calls[1].stdin, "1.1.1.1 . 4000 : 0x5e000007") || strings.Contains(calls[1].stdin, "4001") {
		t.Fatalf("known connections not marked: %+v", calls)
	}
	g.ObserveLine(accessLine("1.1.1.1", 4002, "7.a"))
	g.ObserveLine(accessLine("1.1.1.1", 4003, "8.b"))
	g.FlushMarks()
	if len(calls) != 4 || calls[3].stdin != ConnElemScript([]Conn{{7, "1.1.1.1", 4002}}) {
		t.Fatalf("new connection not marked alone: %+v", calls)
	}
	g.ObserveLine(accessLine("1.1.1.1", 4002, "7.a")) // same connection again (mux stream)
	g.FlushMarks()
	if len(calls) != 4 {
		t.Fatalf("re-marked a fresh element: %+v", calls)
	}
}

// A node agent marks connections from its own access log.
func TestObserveConnLineNode(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, nil)
	g.MarkDelay = time.Hour
	g.ApplyShapes([]Shape{{UserID: 3, UpKbps: 2000, IPs: []string{"9.9.9.9"}}})
	g.ObserveConnLine("2026/11/12 07:00:00 from tcp:[2001:db8::5]:7000 accepted tcp:x.com:443 [inbound-1 >> direct] email: 3.c")
	g.ObserveConnLine("2026/11/12 07:00:00 from 9.9.9.9:7001 accepted tcp:x.com:443 [inbound-1 >> direct] email: 4.d")
	g.FlushMarks()
	last := calls[len(calls)-1]
	if last.stdin != ConnElemScript([]Conn{{3, "2001:db8::5", 7000}}) {
		t.Fatalf("calls %+v", calls)
	}
	if len(g.Tracker.Users(g.now())) != 0 {
		t.Fatal("a node must not grow the IP tracker")
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

func TestParseAccessConnPort(t *testing.T) {
	for line, want := range map[string]int{
		"2026/11/08 17:34:41.990991 from 10.231.0.2:56408 accepted tcp:10.231.0.1:18080 [inbound-9 >> direct] email: 7.alice":                             56408,
		"2026/11/08 17:34:41.990991 from tcp:[2001:db8::1]:50000 accepted udp:1.1.1.1:53 [inbound-1 -> direct] email: 12.bob":                             50000,
		"2026/11/10 17:22:41.990991 from [fe80::10b0:acff:fe10:cc5%eth0]:55832 accepted tcp:speed.cloudflare.com:80 [inbound-1 >> direct] email: 1.alice": 55832,
		"2026/11/08 17:34:41.990991 from 5.6.7.8 accepted tcp:x.com:443 [inbound-1 >> direct] email: 3.c":                                                 0,
	} {
		if _, _, port, ok := ParseAccessConn(line); !ok || port != want {
			t.Errorf("%q: port %d ok %v, want %d", line, port, ok, want)
		}
	}
}
