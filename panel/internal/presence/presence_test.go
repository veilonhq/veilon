package presence

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

var t0 = time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)

func at(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

func up(ids ...int64) map[int64][2]int64 {
	m := map[int64][2]int64{}
	for _, id := range ids {
		m[id] = [2]int64{100, 1000}
	}
	return m
}

// A live source (Xray listing open connections, polled every 5 s): online
// at the first poll after connecting, offline at the second poll missing
// the user, and the last online time is the last poll that saw them.
func TestLiveConnectAndDisconnect(t *testing.T) {
	tr := New()
	tr.ReportLive(MainXray, nil, at(0))
	if tr.Online(1, at(0)) {
		t.Fatal("online before connecting")
	}
	tr.ReportLive(MainXray, []int64{1}, at(5)) // connected at ~3 s
	if !tr.Online(1, at(5)) {
		t.Fatal("not online right after the poll that lists it")
	}
	for s := 10.0; s <= 300; s += 5 {
		tr.ReportLive(MainXray, []int64{1}, at(s))
		if s == 60 || s == 120 { // traffic polls in between change nothing
			tr.Traffic(MainXray, up(1), at(s+2))
		}
	}
	// disconnected at ~302 s
	tr.ReportLive(MainXray, nil, at(305))
	if !tr.Online(1, at(305)) {
		t.Fatal("one missed poll must not flip it (grace)")
	}
	tr.ReportLive(MainXray, nil, at(310))
	if tr.Online(1, at(310)) {
		t.Fatal("still online 8 s after the last poll that saw it")
	}
	if l := tr.LastOnline(1, nil); l == nil || !l.Equal(at(300)) {
		t.Fatalf("last online %v, want %v", l, at(300))
	}
}

// The traffic poll after a disconnect still carries the bytes sent before
// it; that must not keep a live-tracked user online for a minute.
func TestLateTrafficDoesNotHoldLiveUserOnline(t *testing.T) {
	tr := New()
	tr.ReportLive(MainXray, []int64{1}, at(0))
	tr.ReportLive(MainXray, nil, at(5))
	tr.Traffic(MainXray, up(1), at(9)) // stats poll after the disconnect
	tr.ReportLive(MainXray, nil, at(10))
	if tr.Online(1, at(10)) {
		t.Fatal("late traffic kept a disconnected user online")
	}
	if l := tr.LastOnline(1, nil); !l.Equal(at(9)) {
		t.Fatalf("last online %v", l)
	}
}

// Users Xray's list cannot see (TUIC through the sing-box bridge, a local
// reverse proxy: the client address is loopback) keep the traffic rule.
func TestHiddenTrafficOnLiveSource(t *testing.T) {
	tr := New()
	tr.ReportLive(MainXray, nil, at(0))
	tr.Traffic(MainXray, up(2), at(10))
	tr.ReportLive(MainXray, nil, at(10))
	if !tr.Online(2, at(12)) {
		t.Fatal("bridged user not online on traffic")
	}
	tr.ReportLive(MainXray, nil, at(65))
	if !tr.Online(2, at(65)) {
		t.Fatal("traffic window is 60 s")
	}
	tr.ReportLive(MainXray, nil, at(75))
	if tr.Online(2, at(75)) {
		t.Fatal("still online 65 s after the last traffic")
	}
	// a user seen live long ago, now only through the bridge
	tr.ReportLive(MainXray, []int64{3}, at(100))
	for s := 105.0; s <= 200; s += 5 {
		tr.ReportLive(MainXray, nil, at(s))
	}
	tr.Traffic(MainXray, up(3), at(200))
	if !tr.Online(3, at(201)) {
		t.Fatal("hidden traffic long after the last live sighting not counted")
	}
}

// Sources without a connection list (older node agents, AmneziaWG, MTProto,
// an older Xray) keep the earlier rule: uplink within 60 s.
func TestTrafficOnlySource(t *testing.T) {
	tr := New()
	tr.Traffic("node:7", map[int64][2]int64{1: {0, 5000}}, at(0))
	if tr.Online(1, at(1)) {
		t.Fatal("downlink alone marked the user online")
	}
	tr.Traffic("node:7", up(1), at(10))
	if !tr.Online(1, at(10)) || !tr.Online(1, at(69)) {
		t.Fatal("traffic-only user not online within the window")
	}
	if tr.Online(1, at(71)) {
		t.Fatal("traffic-only user online after the window")
	}
}

// A live source that stops reporting (node unreachable, core stopped)
// stops vouching for its users; traffic it had already covered does not
// bring them back.
func TestStaleLiveSource(t *testing.T) {
	tr := New()
	tr.Traffic("node:1", up(1), at(1))
	tr.ReportLive("node:1", []int64{1}, at(2))
	if !tr.Online(1, at(5)) {
		t.Fatal("not online")
	}
	if tr.Online(1, at(25)) {
		t.Fatal("online 23 s after the node's last report")
	}
	// the core stopped: an empty report, then nothing
	tr.ReportLive(MainXray, []int64{2}, at(30))
	tr.Traffic(MainXray, up(2), at(31))
	tr.ReportLive(MainXray, nil, at(32))
	for _, s := range []float64{41, 45, 60, 80} {
		if tr.Online(2, at(s)) {
			t.Fatalf("user of a stopped core online at %v s", s)
		}
	}
	// the source is told it no longer lists connections: traffic rule
	tr.NotLive("node:1")
	tr.Traffic("node:1", up(1), at(100))
	if !tr.Online(1, at(110)) {
		t.Fatal("traffic rule after NotLive")
	}
}

// Any source seeing the user is enough (a user on the main server and a
// node at once).
func TestAnySource(t *testing.T) {
	tr := New()
	tr.ReportLive(MainXray, nil, at(0))
	tr.ReportLive("node:1", []int64{4}, at(0))
	ids := tr.OnlineIDs(at(1))
	if len(ids) != 1 || ids[0] != 4 {
		t.Fatalf("online ids %v", ids)
	}
	tr.Traffic(MainAWG, up(5), at(1))
	if ids := tr.OnlineIDs(at(2)); len(ids) != 2 {
		t.Fatalf("online ids %v", ids)
	}
}

// last_online_at is written when a user comes online and goes offline, and
// at most once a minute while they stay online, not on every poll.
func TestDueThrottlesWrites(t *testing.T) {
	tr := New()
	count := map[int64]int{}
	last := map[int64]time.Time{}
	flush := func(s float64) {
		for _, w := range tr.Due(at(s)) {
			for _, id := range w.IDs {
				count[id]++
				last[id] = w.At
			}
		}
	}
	tr.ReportLive(MainXray, []int64{1}, at(5))
	flush(5)
	if count[1] != 1 || !last[1].Equal(at(5)) {
		t.Fatalf("coming online not written at once: %d %v", count[1], last[1])
	}
	for s := 10.0; s <= 60; s += 5 {
		tr.ReportLive(MainXray, []int64{1}, at(s))
		flush(s)
	}
	if count[1] != 1 {
		t.Fatalf("written %d times within a minute", count[1])
	}
	tr.ReportLive(MainXray, []int64{1}, at(65))
	flush(65)
	if count[1] != 2 || !last[1].Equal(at(65)) {
		t.Fatalf("no write after a minute online: %d %v", count[1], last[1])
	}
	tr.ReportLive(MainXray, []int64{1}, at(70))
	flush(70)
	tr.ReportLive(MainXray, nil, at(75))
	flush(75)
	tr.ReportLive(MainXray, nil, at(80))
	flush(80)
	if count[1] != 3 || !last[1].Equal(at(70)) {
		t.Fatalf("going offline: %d writes, last %v (want the 70 s poll)", count[1], last[1])
	}
	for s := 85.0; s <= 400; s += 5 {
		tr.ReportLive(MainXray, nil, at(s))
		flush(s)
	}
	if count[1] != 3 {
		t.Fatalf("offline user written again: %d", count[1])
	}
	// a short connection between two polls: traffic only, written once
	tr.Traffic(MainXray, up(9), at(402))
	tr.ReportLive(MainXray, nil, at(405))
	flush(405)
	flush(410)
	if count[9] != 1 || !last[9].Equal(at(402)) {
		t.Fatalf("short connection: %d %v", count[9], last[9])
	}
}

// Store moves last_online_at forward only, in one statement per moment.
func TestStore(t *testing.T) {
	gdb, err := db.Open("sqlite", filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	later := at(100)
	for i, u := range []db.User{{ID: 1, LastOnlineAt: &later}, {ID: 2}, {ID: 3}} {
		u.Username, u.Status, u.UUID, u.Password, u.SubToken = string(rune('a'+i)), db.StatusActive, string(rune('a'+i)), "p", string(rune('a'+i))
		u.CreatedAt, u.UpdatedAt = now, now
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := Store(gdb, []Write{{At: at(50), IDs: []int64{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	var us []db.User
	gdb.Order("id").Find(&us)
	if !us[0].LastOnlineAt.Equal(later) || !us[1].LastOnlineAt.Equal(at(50)) || us[2].LastOnlineAt != nil {
		t.Fatalf("stored %v %v %v", us[0].LastOnlineAt, us[1].LastOnlineAt, us[2].LastOnlineAt)
	}
	// on-hold users start their period on the first write (users.Evaluate
	// reads last_online_at), so coming online is written without waiting
	tr := New()
	tr.ReportLive(MainXray, []int64{3}, at(200))
	if err := Store(gdb, tr.Due(at(201))); err != nil {
		t.Fatal(err)
	}
	var u3 db.User
	gdb.First(&u3, 3)
	if u3.LastOnlineAt == nil || !u3.LastOnlineAt.Equal(at(200)) {
		t.Fatalf("user 3: %v", u3.LastOnlineAt)
	}
	// shutdown stores the last moment seen
	tr.ReportLive(MainXray, []int64{3}, at(230))
	if err := Store(gdb, tr.Final()); err != nil {
		t.Fatal(err)
	}
	gdb.First(&u3, 3)
	if !u3.LastOnlineAt.Equal(at(230)) {
		t.Fatalf("final: %v", u3.LastOnlineAt)
	}
}

// Memory stays bounded by the users seen recently.
func TestPrune(t *testing.T) {
	tr := New()
	tr.Traffic("node:3", up(1, 2, 3), at(0))
	tr.Due(at(1))
	tr.Due(at(1000))
	if len(tr.sources) != 0 || len(tr.state) != 0 {
		t.Fatalf("kept %d sources, %d states", len(tr.sources), len(tr.state))
	}
}
