package nodes

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/guard"
)

// limitsRec is a Shaper of an older agent: no ApplyAWGLimits.
type limitsRec struct {
	mu sync.Mutex
	n  int
}

func (s *limitsRec) ApplyShapes([]guard.Shape) { s.mu.Lock(); s.n++; s.mu.Unlock() }

// A node agent with CapAWGShape gets every user's speed limit and shapes
// its AmneziaWG peers by tunnel address: of two users on the same
// inbound only the limited one is shaped. An agent without the capability
// gets no limits (and keeps working).
func TestAWGSpeedLimitsReachNodes(t *testing.T) {
	r := newRig(t)
	r.engines = true
	g := guard.New(r.gdb, "")
	g.Run = (&nftRec{}).run
	g.LookPath = func(string) (string, error) { return "/sbin/x", nil }
	g.Dev = "eth0"
	g.Limit(5, 0, 2048, 1024)
	r.m.OnIPs, r.m.Bans, r.m.Shapes, r.m.Limits = g.ObserveRemote, g.Bans, g.Shapes, g.SpeedLimits
	ctx := context.Background()

	n := r.addNode(t, "de", 1)
	a, _ := startAgent(t, r, n)
	rec := &nftRec{}
	ng := guard.New(nil, "")
	ng.Run = rec.run
	ng.LookPath = func(string) (string, error) { return "/sbin/x", nil }
	ng.IfIndex = func(d string) int {
		if d == "vawg7" {
			return 12
		}
		return 0
	}
	ng.SetAWGPeers([]guard.AWGPeer{
		{Iface: "vawg7", Addr: "10.66.66.2/32", UserID: 5}, // limited
		{Iface: "vawg7", Addr: "10.66.66.3/32", UserID: 6}, // no limit
	})
	a.Shaper = ng
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // learns the caps
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if caps, _ := r.m.Caps(n.ID); !hasCap(caps, CapAWGShape) {
		t.Fatalf("agent caps %v lack %s", caps, CapAWGShape)
	}
	all := rec.all()
	if !strings.Contains(all, "u32 match ip dst 10.66.66.2/32 flowid 7a:10") || !strings.Contains(all, `ip saddr 10.66.66.2 limit name "up5" drop`) {
		t.Fatalf("limited AmneziaWG user not shaped on the node:\n%s", all)
	}
	if strings.Contains(all, "10.66.66.3") {
		t.Fatalf("user without a limit shaped on the node:\n%s", all)
	}

	// An agent whose shaper cannot shape AmneziaWG announces no cap and
	// gets no limits; bans and the Xray plan still arrive.
	n2 := r.addNode(t, "fr", 1)
	a2, _ := startAgent(t, r, n2)
	old := &limitsRec{}
	a2.Shaper = old
	r.m.SyncNode(ctx, reload(t, r.gdb, n2.ID))
	r.m.SyncNode(ctx, reload(t, r.gdb, n2.ID))
	if caps, _ := r.m.Caps(n2.ID); hasCap(caps, CapAWGShape) {
		t.Fatalf("old shaper announced %s", CapAWGShape)
	}
}
