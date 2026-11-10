package nodes

import (
	"context"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/jobs"
	"github.com/veilonhq/veilon/panel/internal/stats"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// A node's totals are the users' counted usage it carried (multipliers
// applied), the same figure as its share in "traffic by server" and in the
// users' counters, not the raw inbound counters of its Xray.
func TestNodeTotalsAreCountedUsage(t *testing.T) {
	r := newRig(t)
	r.gdb.Model(&db.User{}).Where("id = ?", 1).Update("traffic_multiplier", 2.0)
	r.gdb.Model(&db.User{}).Where("id = ?", 2).Update("traffic_multiplier", 1.0)
	n := r.addNode(t, "de", 1)
	a, core := startAgent(t, r, n)
	ctx := context.Background()
	usage.TakeAll() // clean meters
	core.mu.Lock()
	core.running = true
	// raw inbound counters carry more than the users (handshakes, unknown users)
	core.traffic = xray.Traffic{Users: map[int64][2]int64{1: {100, 1000}, 2: {7, 70}}, SysUp: 500, SysDown: 5000}
	core.mu.Unlock()
	a.collect(ctx)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // config
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // traffic
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // ack, nothing new

	var alice, bob db.User
	r.gdb.First(&alice, 1)
	r.gdb.First(&bob, 2)
	if alice.Up != 200 || alice.Down != 2000 || bob.Up != 7 || bob.Down != 70 {
		t.Fatalf("users %d/%d %d/%d", alice.Up, alice.Down, bob.Up, bob.Down)
	}
	nd := reload(t, r.gdb, n.ID)
	if nd.Up != 207 || nd.Down != 2070 {
		t.Fatalf("node totals %d/%d, want the counted 207/2070", nd.Up, nd.Down)
	}
	jr := &jobs.Runner{DB: r.gdb, TakeServers: usage.TakeAll, RestoreServers: usage.RestoreAll}
	jr.Tick()
	_, servers, err := stats.TrafficByServer(r.gdb, 24*time.Hour, time.Hour, time.Now(), time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range servers {
		if s.ID == n.ID && (s.Up != nd.Up || s.Down != nd.Down) {
			t.Fatalf("traffic by server %d/%d != node card %d/%d", s.Up, s.Down, nd.Up, nd.Down)
		}
		if s.ID == n.ID {
			return
		}
	}
	t.Fatal("node missing from traffic by server")
}
