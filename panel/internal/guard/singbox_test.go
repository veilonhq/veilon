package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

type hit struct{ user, ip string }

func parseAll(p *SingboxParser, lines []string) []hit {
	var out []hit
	for _, l := range lines {
		if u, ip, ok := p.Line(l); ok {
			out = append(out, hit{u, ip})
		}
	}
	return out
}

func captured(t *testing.T) []string {
	t.Helper()
	// Real output of sing-box 1.14.4 serving TUIC (colored, as written into
	// the supervisor's pipe): IPv4 from two addresses, IPv6, a UDP relay
	// and a client with a wrong password.
	b, err := os.ReadFile(filepath.Join("testdata", "singbox-1.14.4-tuic.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func TestSingboxParserRealLog(t *testing.T) {
	got := parseAll(NewSingboxParser(), captured(t))
	want := []hit{{"7.alice", "10.77.0.2"}, {"7.alice", "10.77.0.3"}, {"8.bob", "fd77::2"}, {"8.bob", "10.77.0.2"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSingboxParserInterleavedAndPlain(t *testing.T) {
	p := NewSingboxParser()
	lines := []string{
		"INFO [11 0ms] inbound/tuic[in-1]: inbound connection from 1.1.1.1:1000",
		"INFO [22 0ms] inbound/tuic[in-2]: inbound connection from [2001:db8::5]:2000",
		"INFO [22 1ms] inbound/tuic[in-2]: [9.carol] inbound connection to example.com:443",
		"INFO [11 1ms] inbound/tuic[in-1]: [7.alice] inbound packet connection to 8.8.8.8:53",
		// A second user line for the same id (none pending any more).
		"INFO [11 2ms] inbound/tuic[in-1]: [7.alice] inbound connection to example.com:443",
		// A user line without a source line.
		"INFO [33 0ms] inbound/tuic[in-1]: [7.alice] inbound connection to example.com:443",
		// IPv4-mapped IPv6 and loopback sources.
		"INFO [44 0ms] inbound/tuic[in-1]: inbound connection from [::ffff:5.6.7.8]:4000",
		"INFO [44 0ms] inbound/tuic[in-1]: [7.alice] inbound connection to a:1",
		"INFO [55 0ms] inbound/tuic[in-1]: inbound connection from 127.0.0.1:4000",
		"INFO [55 0ms] inbound/tuic[in-1]: [7.alice] inbound connection to a:1",
		// Outbound lines and router noise are ignored.
		"INFO [66 0ms] outbound/socks[u-7]: outbound connection to a:1",
		"INFO [66 0ms] inbound/tuic[in-1]: inbound connection from 9.9.9.9:1",
		"INFO [66 0ms] inbound/tuic[in-1]: [] inbound connection to a:1",
		"INFO [77 0ms] inbound/tuic[in-1]: inbound connection from 9.9.9.9:1",
		"INFO [77 0ms] inbound/tuic[in-1]: [7.alice] something else",
		"INFO [abc 0ms] inbound/tuic[in-1]: inbound connection from 9.9.9.9:1",
		"INFO [abc 0ms] inbound/tuic[in-1]: [7.alice] inbound connection to a:1",
	}
	got := parseAll(p, lines)
	want := []hit{{"9.carol", "2001:db8::5"}, {"7.alice", "1.1.1.1"}, {"7.alice", "5.6.7.8"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSingboxParserPendingIsBounded(t *testing.T) {
	p := NewSingboxParser()
	for i := 0; i < 5*maxPendingSources; i++ {
		p.Line(fmt.Sprintf("INFO [%d 0ms] inbound/tuic[x]: inbound connection from 1.2.3.4:%d", i, i%60000+1))
		if len(p.pending) > maxPendingSources {
			t.Fatalf("pending grew to %d", len(p.pending))
		}
	}
	// Still works after resets.
	p.Line("INFO [999999999 0ms] inbound/tuic[x]: inbound connection from 4.3.2.1:5")
	if u, ip, ok := p.Line("INFO [999999999 0ms] inbound/tuic[x]: [1.a] inbound connection to a:1"); !ok || u != "1.a" || ip != "4.3.2.1" {
		t.Fatal(u, ip, ok)
	}
}

func TestSingboxParserConcurrent(t *testing.T) {
	p := NewSingboxParser()
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				id := w*100000 + i
				p.Line(fmt.Sprintf("INFO [%d 0ms] inbound/tuic[x]: inbound connection from 10.0.%d.%d:1", id, w, i%250+1))
				if _, _, ok := p.Line(fmt.Sprintf("INFO [%d 0ms] inbound/tuic[x]: [%d.u] inbound connection to a:1", id, w+1)); ok {
					mu.Lock()
					n++
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()
	if n != 8*500 {
		t.Fatalf("paired %d", n)
	}
}

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"plain":                                 "plain",
		"\x1b[36mINFO\x1b[0m x":                 "INFO x",
		"[\x1b[38;5;175m3172046751\x1b[0m 0ms]": "[3172046751 0ms]",
		"cut \x1b[38;5":                         "cut ",
		"esc at end \x1b":                       "esc at end ",
		"two-byte \x1bMx":                       "two-byte x",
		"":                                      "",
	}
	for in, want := range cases {
		if got := StripANSI(in); got != want {
			t.Errorf("StripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzSingboxLine(f *testing.F) {
	for _, s := range []string{
		"INFO [1 0ms] inbound/tuic[x]: inbound connection from 1.2.3.4:5",
		"INFO [1 0ms] inbound/tuic[x]: [1.a] inbound connection to a:1",
		"] inbound/", "[ ] inbound/]: ", "[1] inbound/x]: [", "[1] inbound/x]: [a] ",
		"\x1b[", "\x1b", "[1 0ms] inbound/t[x]: inbound connection from [",
	} {
		f.Add(s)
	}
	p := NewSingboxParser()
	f.Fuzz(func(t *testing.T, s string) {
		p.Line(s)
		StripANSI(s)
	})
}

// TUIC connections feed the same pipeline as Xray's access log: the
// tracker, the IP limit (bans) and speed shaping.
func TestGuardObservesSingbox(t *testing.T) {
	var calls []string
	g := newTestGuard(&calls)
	for _, l := range captured(t) {
		g.ObserveSingboxLine(l)
	}
	g.ObserveLine("2026/11/07 10:00:00 from 1.2.3.4:50000 accepted tcp:example.com:443 [in-1 >> direct] email: 7.alice")
	now := g.now()
	if live := g.Tracker.Live(7, now); len(live) != 3 {
		t.Fatalf("alice live %v", live)
	}
	if live := g.Tracker.Live(8, now); fmt.Sprint(live) != "[10.77.0.2 fd77::2]" {
		t.Fatalf("bob live %v", live)
	}
	g.Limit(7, 1, 2000, 1000)
	bans, shapes := g.Plan()
	if len(bans) != 2 || len(shapes) != 1 || len(shapes[0].IPs) != 1 {
		t.Fatalf("bans %v shapes %+v", bans, shapes)
	}
}

// An IP the admin blocked for a user is refused by the cores; it must not
// take one of the user's IP-limit slots (and get the real IP banned).
func TestPlanSkipsBlockedIPs(t *testing.T) {
	var calls []string
	g := newTestGuard(&calls)
	t0 := g.now()
	g.Tracker.Observe(7, "6.6.6.6", t0.Add(-time.Minute)) // blocked, oldest
	g.Tracker.Observe(7, "1.1.1.1", t0)
	g.Limit(7, 1, 0, 500)
	if bans, _ := g.Plan(); fmt.Sprint(bans) != "[1.1.1.1]" {
		t.Fatalf("without block: %v", bans)
	}
	g.SetBlocked(map[int64][]string{7: {"6.6.6.6"}, 8: {"1.1.1.1"}})
	bans, shapes := g.Plan()
	if len(bans) != 0 || len(shapes) != 1 || fmt.Sprint(shapes[0].IPs) != "[1.1.1.1]" {
		t.Fatalf("bans %v shapes %+v", bans, shapes)
	}
}

// Refresh loads the TUIC inbound's port (the nft ban covers it, UDP too)
// and the blocked IPs; Flush writes TUIC client IPs to the connection log.
func TestRefreshAndFlushWithDB(t *testing.T) {
	gdb := testutil.DB(t)
	u := db.User{Username: "alice", Status: db.StatusActive, IPLimit: 1, UUID: "u", SubToken: "t1"}
	if err := gdb.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	for _, ib := range []db.Inbound{
		{Remark: "tuic", Protocol: "tuic", Port: 4433, Enabled: true},
		{Remark: "vless", Protocol: "vless", Port: 443, Enabled: true},
		{Remark: "off", Protocol: "vless", Port: 8443, Enabled: false},
	} {
		ib := ib
		if err := gdb.Create(&ib).Error; err != nil {
			t.Fatal(err)
		}
		// A false Enabled is a zero value gorm may leave to the column
		// default; set it explicitly.
		gdb.Model(&db.Inbound{}).Where("id = ?", ib.ID).Update("enabled", ib.Remark != "off")
	}
	if err := gdb.Create(&db.UserIP{UserID: u.ID, IP: "6.6.6.6", Blocked: true, FirstSeen: time.Now(), LastSeen: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	geo := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(geo, EncodeGeoIP(map[string][]string{"se": {"10.77.0.0/16"}}), 0o600); err != nil {
		t.Fatal(err)
	}
	var scripts []string
	g := New(gdb, geo)
	g.Dev = "eth0"
	g.Run = func(stdin, name string, args ...string) (string, error) {
		scripts = append(scripts, stdin)
		return "", nil
	}
	old := lookPath
	lookPath = func(string) (string, error) { return "/bin/true", nil }
	defer func() { lookPath = old }()
	g.Refresh()
	email := fmt.Sprintf("%d.alice", u.ID)
	feed := func(id int, ip string) {
		g.ObserveSingboxLine(fmt.Sprintf("+0000 2026-11-08 18:17:04 INFO [%d 0ms] inbound/tuic[inbound-tuic]: inbound connection from %s:5000", id, ip))
		g.ObserveSingboxLine(fmt.Sprintf("+0000 2026-11-08 18:17:04 INFO [%d 0ms] inbound/tuic[inbound-tuic]: [%s] inbound connection to a:443", id, email))
	}
	feed(1, "6.6.6.6") // blocked: logged, but not counted
	feed(2, "10.77.0.2")
	feed(3, "10.77.0.3")
	g.Enforce()
	if len(scripts) < 2 || !strings.Contains(scripts[0], "ports { 443, 4433 }") || !strings.Contains(scripts[0], "meta l4proto { tcp, udp }") {
		t.Fatalf("firewall script %q", scripts)
	}
	if !strings.Contains(scripts[1], "10.77.0.3 timeout") || strings.Contains(scripts[1], "10.77.0.2") || strings.Contains(scripts[1], "6.6.6.6") {
		t.Fatalf("ban script %q", scripts[1])
	}
	g.Flush()
	var rows []db.UserIP
	gdb.Where("user_id = ?", u.ID).Order("ip").Find(&rows)
	if len(rows) != 3 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].IP != "10.77.0.2" || rows[0].Country != "SE" || rows[0].Hits != 1 || rows[1].IP != "10.77.0.3" ||
		rows[2].IP != "6.6.6.6" || !rows[2].Blocked || rows[2].Hits != 1 {
		t.Fatalf("rows %+v", rows)
	}
}

// TUIC connections carry their client port too, so their speed limit is
// per connection like Xray's.
func TestSingboxParserPorts(t *testing.T) {
	p := NewSingboxParser()
	var got []string
	for _, l := range captured(t) {
		if u, ip, port, ok := p.LineConn(l); ok {
			got = append(got, fmt.Sprintf("%s %s %d", u, ip, port))
		}
	}
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if strings.HasSuffix(g, " 0") {
			t.Fatalf("no port in %v", got)
		}
	}
}

func TestSingboxParserNaive(t *testing.T) {
	p := NewSingboxParser()
	u, ip, port, ok := p.LineConn("INFO[0129] [3445973830 0ms] inbound/naive[inbound-3]: [7.alice] inbound connection from 10.0.0.9:39998")
	if !ok || u != "7.alice" || ip != "10.0.0.9" || port != 39998 {
		t.Fatalf("naive from-line: %q %q %d %v", u, ip, port, ok)
	}
	if _, _, _, ok := p.LineConn("INFO[0129] [3445973830 0ms] inbound/naive[inbound-3]: [7.alice] inbound connection to example.com:80"); ok {
		t.Fatal("the to-line of a naive connection must not count twice")
	}
}

func TestSingboxParserShadowTLS(t *testing.T) {
	p := NewSingboxParser()
	now := time.Unix(1_800_000_000, 0)
	p.now = func() time.Time { return now }
	line := func(s string) (string, string, bool) {
		u, ip, _, ok := p.LineConn(s)
		return u, ip, ok
	}
	// The ShadowTLS user line has a new id: the oldest queued source pairs.
	line("INFO[0001] [657807017 0ms] inbound/shadowtls[inbound-1]: inbound connection from 10.0.0.1:55150")
	line("INFO[0001] [657807018 0ms] inbound/shadowtls[inbound-1]: inbound connection from 10.0.0.2:55151")
	if u, ip, ok := line("INFO[0001] [3403315555 51ms] inbound/shadowtls[inbound-1]: [7.alice] inbound connection to :0"); !ok || u != "7.alice" || ip != "10.0.0.1" {
		t.Fatalf("first pair: %q %q %v", u, ip, ok)
	}
	// The inner Shadowsocks line of the same connection does not count again.
	if _, _, ok := line("INFO[0001] [3403315555 51ms] inbound/shadowsocks[inbound-1-ss]: [7.alice] inbound connection to example.com:80"); ok {
		t.Fatal("inner line counted")
	}
	// A failed handshake (its id shows up again) leaves the queue.
	line("ERROR[0002] [657807018 3ms] inbound/shadowtls[inbound-1]: process connection from 10.0.0.2:55151: hmac mismatch")
	if _, _, ok := line("INFO[0002] [999 1ms] inbound/shadowtls[inbound-1]: [8.bob] inbound connection to :0"); ok {
		t.Fatal("a failed handshake's source was paired")
	}
	// Sources older than the pairing age are dropped; other inbounds have their own queue.
	line("INFO[0003] [1000 0ms] inbound/shadowtls[inbound-1]: inbound connection from 10.0.0.3:1")
	line("INFO[0003] [1001 0ms] inbound/shadowtls[inbound-2]: inbound connection from 10.0.0.4:1")
	now = now.Add(shadowTLSPairAge + time.Second)
	if _, _, ok := line("INFO[0020] [1002 1ms] inbound/shadowtls[inbound-1]: [8.bob] inbound connection to :0"); ok {
		t.Fatal("stale source paired")
	}
	line("INFO[0020] [1003 0ms] inbound/shadowtls[inbound-2]: inbound connection from 10.0.0.5:1")
	if u, ip, ok := line("INFO[0020] [1004 1ms] inbound/shadowtls[inbound-2]: [8.bob] inbound connection to :0"); !ok || u != "8.bob" || ip != "10.0.0.5" {
		t.Fatalf("second inbound: %q %q %v", u, ip, ok)
	}
}
