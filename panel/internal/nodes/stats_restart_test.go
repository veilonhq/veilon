package nodes

import (
	"context"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// A node restarting (an update) between two master rounds loses no
// traffic and counts none twice: the unacknowledged batch keeps its id and
// the counters not yet batched are kept too.
func TestNodeRestartKeepsPendingTraffic(t *testing.T) {
	r := newRig(t)
	n := r.addNode(t, "de", 1)
	a, core := startAgent(t, r, n)
	ctx := context.Background()
	core.mu.Lock()
	core.running = true
	core.traffic = xray.Traffic{Users: map[int64][2]int64{1: {100, 1000}}, SysUp: 100, SysDown: 1000}
	core.mu.Unlock()
	a.collect(ctx)
	// the node builds batch 1 for a request whose answer is lost
	a.Handler()
	a.mu.Lock()
	a.batch++
	b := a.pending
	b.Seq = a.boot + ":1"
	a.unacked = &b
	a.pending = StatsReply{Users: map[int64][2]int64{}}
	a.mu.Unlock()
	// more traffic, not batched yet
	core.mu.Lock()
	core.traffic = xray.Traffic{Users: map[int64][2]int64{1: {5, 50}}, SysUp: 5, SysDown: 50}
	core.mu.Unlock()
	a.collect(ctx)
	a.saveStats()

	// the restarted agent (same directory, new boot)
	b2, _ := startAgentIn(t, r, n, a.Dir)
	b2.Restore(ctx)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // stores the old batch
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // acks it, gets the pending traffic
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // acks again: nothing new
	var u db.User
	r.gdb.First(&u, 1)
	if u.Up != 105 || u.Down != 1050 {
		t.Fatalf("user counters %d/%d, want 105/1050", u.Up, u.Down)
	}
	var nd db.Node
	r.gdb.First(&nd, n.ID)
	if nd.Up != 105 || nd.Down != 1050 {
		t.Fatalf("node totals %d/%d", nd.Up, nd.Down)
	}
}
