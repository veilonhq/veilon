package nodes

import (
	"context"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/presence"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// onlineCore is the core of an agent that lists online users (v0.3.6+).
type onlineCore struct {
	*fakeCore
	ids []int64
	err error
}

func (c *onlineCore) Online(context.Context) ([]int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.ids...), c.err
}

func (c *onlineCore) set(ids []int64, err error) {
	c.mu.Lock()
	c.ids, c.err = ids, err
	c.mu.Unlock()
}

// A node whose agent lists online users: the master asks every few seconds
// and a user shows online as soon as the node lists them, offline once it
// stops.
func TestNodeReportsOnlineUsers(t *testing.T) {
	r := newRig(t)
	tr := presence.New()
	tr.OfflineGrace = 50 * time.Millisecond
	r.m.Presence = tr
	n := r.addNode(t, "n1", 1)
	var oc *onlineCore
	r.wrap = func(fc *fakeCore) Core { oc = &onlineCore{fakeCore: fc}; return oc }
	ag, fc := startAgent(t, r, n)
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	r.m.mu.Lock()
	live := r.m.states[n.ID].liveOnline
	r.m.mu.Unlock()
	if !live {
		t.Fatal("agent with an online list did not announce it")
	}
	oc.set([]int64{1}, nil)
	r.m.PollOnline(ctx, reload(t, r.gdb, n.ID))
	if !tr.Online(1, time.Now().UTC()) || tr.Online(2, time.Now().UTC()) {
		t.Fatal("node's online user not online on the master")
	}
	oc.set(nil, nil)
	time.Sleep(60 * time.Millisecond)
	r.m.PollOnline(ctx, reload(t, r.gdb, n.ID))
	if tr.Online(1, time.Now().UTC()) {
		t.Fatal("user still online after the node stopped listing them")
	}
	// traffic counted by the node only updates the last online time
	fc.mu.Lock()
	fc.traffic = xray.Traffic{Users: map[int64][2]int64{1: {10, 20}}}
	fc.mu.Unlock()
	ag.collect(ctx)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if tr.Online(1, time.Now().UTC()) {
		t.Fatal("late node traffic held a disconnected user online")
	}
	// OnlineRound runs the same poll in the background
	oc.set([]int64{2}, nil)
	r.m.OnlineRound(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for !tr.Online(2, time.Now().UTC()) {
		if time.Now().After(deadline) {
			t.Fatal("OnlineRound did not report user 2")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An agent whose Xray cannot list online users answers 501: the master
// stops asking for a while and keeps the traffic rule for that node.
func TestNodeOnlineUnsupported(t *testing.T) {
	r := newRig(t)
	tr := presence.New()
	r.m.Presence = tr
	n := r.addNode(t, "n1", 1)
	r.wrap = func(fc *fakeCore) Core { return &onlineCore{fakeCore: fc, err: ErrOnlineUnsupported} }
	ag, fc := startAgent(t, r, n)
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	r.m.PollOnline(ctx, reload(t, r.gdb, n.ID))
	r.m.mu.Lock()
	_, marked := r.m.noOnline[n.ID]
	r.m.mu.Unlock()
	if !marked {
		t.Fatal("unsupported answer not remembered")
	}
	fc.mu.Lock()
	fc.traffic = xray.Traffic{Users: map[int64][2]int64{1: {10, 20}}}
	fc.mu.Unlock()
	ag.collect(ctx)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if !tr.Online(1, time.Now().UTC()) {
		t.Fatal("traffic rule not applied")
	}
}

// Agents up to v0.3.5 do not list online users: the master never asks them
// and keeps the earlier rule (uplink within a minute).
func TestOldAgentKeepsTrafficRule(t *testing.T) {
	r := newRig(t)
	tr := presence.New()
	r.m.Presence = tr
	n := r.addNode(t, "n1", 1)
	ag, fc := startAgent(t, r, n) // fakeCore: no Online method
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if in := ag.info(); in.HasCap(CapOnline) {
		t.Fatal("old core announced the online list")
	}
	r.m.OnlineRound(ctx)
	r.m.mu.Lock()
	asked := len(r.m.onlineIn)
	r.m.mu.Unlock()
	if asked != 0 {
		t.Fatal("old agent was asked for online users")
	}
	fc.mu.Lock()
	fc.traffic = xray.Traffic{Users: map[int64][2]int64{1: {10, 20}, 2: {0, 30}}}
	fc.mu.Unlock()
	ag.collect(ctx)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	now := time.Now().UTC()
	if !tr.Online(1, now) || tr.Online(2, now) {
		t.Fatal("old agent: uplink must mark online, downlink alone must not")
	}
	if tr.Online(1, now.Add(61*time.Second)) {
		t.Fatal("old agent: online after the 60 s window")
	}
}
