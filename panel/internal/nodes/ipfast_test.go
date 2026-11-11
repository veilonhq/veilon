package nodes

import (
	"context"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/presence"
)

func ipRow(t *testing.T, r *rig, uid int64, ip string) (row db.UserIP, ok bool) {
	t.Helper()
	res := r.gdb.Where("user_id = ? AND ip = ?", uid, ip).Limit(1).Find(&row)
	return row, res.Error == nil && res.RowsAffected == 1
}

// A new IP on the main server is written by FlushNew (the few-second tick)
// as one row; more hits from it do not write again until the minute Flush,
// which then adds them without counting anything twice.
func TestNewIPWrittenFastUpdatesThrottled(t *testing.T) {
	r := newRig(t)
	clock := time.Now().UTC().Truncate(time.Second)
	g := guard.New(r.gdb, "")
	g.Now = func() time.Time { return clock }
	g.ObserveLine(accessLine("198.51.100.7", 1, "alice"))
	if _, ok := ipRow(t, r, 1, "198.51.100.7"); ok {
		t.Fatal("row before any flush")
	}
	g.FlushNew()
	row, ok := ipRow(t, r, 1, "198.51.100.7")
	if !ok || row.Hits != 1 || !row.LastSeen.Equal(clock) {
		t.Fatalf("new IP not written by FlushNew: %+v", row)
	}
	clock = clock.Add(20 * time.Second)
	g.ObserveLine(accessLine("198.51.100.7", 1, "alice"))
	g.ObserveLine(accessLine("198.51.100.7", 1, "alice"))
	g.FlushNew()
	if row, _ := ipRow(t, r, 1, "198.51.100.7"); row.Hits != 1 || !row.LastSeen.Equal(clock.Add(-20*time.Second)) {
		t.Fatalf("known IP written by FlushNew: %+v", row)
	}
	g.Flush()
	if row, _ := ipRow(t, r, 1, "198.51.100.7"); row.Hits != 3 || !row.LastSeen.Equal(clock) {
		t.Fatalf("minute flush: %+v", row)
	}
}

// A node agent with the online list sends new IPs with the 5 s online
// poll, before the 10 s stats round; the stats round then counts only
// later hits.
func TestNodeIPsViaOnlinePoll(t *testing.T) {
	r := newRig(t)
	r.m.Presence = presence.New()
	g := guard.New(r.gdb, "")
	r.m.OnIPs = g.ObserveRemote
	n := r.addNode(t, "n1", 1)
	var oc *onlineCore
	r.wrap = func(fc *fakeCore) Core { oc = &onlineCore{fakeCore: fc}; return oc }
	ag, _ := startAgent(t, r, n)
	ag.IPs = &guard.IPCollector{}
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))

	ag.IPs.ObserveLine(accessLine("203.0.113.5", 1, "alice"))
	oc.set([]int64{1}, nil)
	r.m.PollOnline(ctx, reload(t, r.gdb, n.ID))
	if live := g.Tracker.Live(1, time.Now().UTC()); len(live) != 1 || live[0] != "203.0.113.5" {
		t.Fatalf("node IP not on the master after the online poll: %v", live)
	}
	g.FlushNew()
	if row, ok := ipRow(t, r, 1, "203.0.113.5"); !ok || row.Hits != 1 {
		t.Fatalf("node IP not written: %+v", row)
	}
	// the stats round brings no duplicate hit
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	g.Flush()
	if row, _ := ipRow(t, r, 1, "203.0.113.5"); row.Hits != 1 {
		t.Fatalf("hits counted twice: %d", row.Hits)
	}
	// a later hit comes with the stats round as before
	ag.IPs.ObserveLine(accessLine("203.0.113.5", 1, "alice"))
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // ack
	g.Flush()
	if row, _ := ipRow(t, r, 1, "203.0.113.5"); row.Hits != 2 {
		t.Fatalf("later hit: %d", row.Hits)
	}
}

// Agents without the online list (v0.3.5 and older) are never polled: their
// IPs keep arriving with the stats round.
func TestOldAgentIPsViaStatsRound(t *testing.T) {
	r := newRig(t)
	r.m.Presence = presence.New()
	g := guard.New(r.gdb, "")
	r.m.OnIPs = g.ObserveRemote
	n := r.addNode(t, "n1", 1)
	ag, _ := startAgent(t, r, n) // fakeCore: no online list
	ag.IPs = &guard.IPCollector{}
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	ag.IPs.ObserveLine(accessLine("203.0.113.6", 1, "alice"))
	r.m.OnlineRound(ctx)
	if live := g.Tracker.Live(1, time.Now().UTC()); len(live) != 0 {
		t.Fatalf("old agent IP arrived without a stats round: %v", live)
	}
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	g.FlushNew()
	if row, ok := ipRow(t, r, 1, "203.0.113.6"); !ok || row.Hits != 1 {
		t.Fatalf("old agent IP not written: %+v", row)
	}
}
