package nodes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
)

// A node agent with CapUserFW gets the address entries of users' own block
// lists and applies them to its AmneziaWG peers; an emptied list reaches it
// too (sent as [], never left out). An agent without the capability gets
// none and, when it runs AmneziaWG inbounds while a user has a list, the
// node is marked for an agent update.
func TestUserFirewallReachesNodes(t *testing.T) {
	r := newRig(t)
	r.engines = true
	lists := []guard.UserFW{{UserID: 5, CIDRs: []string{"93.184.215.14/32"}}}
	r.m.UserFirewalls = func() []guard.UserFW { return lists }
	r.m.Bans = func() []guard.Ban { return nil }
	ctx := context.Background()

	n := r.addNode(t, "de", 1)
	a, _ := startAgent(t, r, n)
	rec := &nftRec{}
	ng := guard.New(nil, "")
	ng.Run = rec.run
	ng.LookPath = func(string) (string, error) { return "/sbin/x", nil }
	ng.SetAWGPeers([]guard.AWGPeer{
		{Iface: "vawg7", Addr: "10.66.66.2/32", UserID: 5},
		{Iface: "vawg7", Addr: "10.66.66.3/32", UserID: 6},
	})
	a.Shaper = ng
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID)) // learns the caps
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if caps, _ := r.m.Caps(n.ID); !hasCap(caps, CapUserFW) {
		t.Fatalf("agent caps %v lack %s", caps, CapUserFW)
	}
	all := rec.all()
	if !strings.Contains(all, `iifname "vawg7" ip saddr 10.66.66.2 ip daddr @u5 reject`) || strings.Contains(all, "10.66.66.3 ip daddr") {
		t.Fatalf("node rules:\n%s", all)
	}
	lists = []guard.UserFW{}
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	scripts := rec.scripts
	if lastFW := scripts[len(scripts)-1]; strings.Contains(lastFW, "@u5") || !strings.Contains(lastFW, "delete table inet "+guard.UserFWTable) {
		t.Fatalf("emptied list did not reach the node:\n%s", rec.all())
	}

	// An older agent (shaper without ApplyUserFirewall).
	lists = []guard.UserFW{{UserID: 5, CIDRs: []string{"93.184.215.14/32"}}}
	awg, _ := engineInbounds(t, r)
	n2 := r.addNode(t, "fr", 1, awg)
	a2, _ := startAgent(t, r, n2)
	a2.Shaper = &limitsRec{}
	r.m.SyncNode(ctx, reload(t, r.gdb, n2.ID))
	r.m.SyncNode(ctx, reload(t, r.gdb, n2.ID))
	if caps, _ := r.m.Caps(n2.ID); hasCap(caps, CapUserFW) {
		t.Fatalf("old agent announced %s", CapUserFW)
	}
	if st := r.m.Status(n2); !st.UserFWNeedsUpdate || st.Status != StateOnline {
		t.Fatalf("old agent with AmneziaWG not marked: %+v", st)
	}
	if st := r.m.Status(reload(t, r.gdb, n.ID)); st.UserFWNeedsUpdate {
		t.Fatal("current agent marked")
	}
	// Nobody has a list: no mark.
	lists = []guard.UserFW{}
	r.m.SyncNode(ctx, reload(t, r.gdb, n2.ID))
	if st := r.m.Status(n2); st.UserFWNeedsUpdate {
		t.Fatal("marked without any list")
	}
	var _ = db.Node{}
}

// An emptied list is sent as [] (an agent reads a missing field as "no
// change"), for the AmneziaWG speed limits and the user firewall alike.
func TestBansPushKeepsEmptyLists(t *testing.T) {
	b, _ := json.Marshal(BansPush{Bans: []guard.Ban{}, Shapes: []guard.Shape{}, Limits: []guard.Shape{}, Firewall: []guard.UserFW{}})
	if !strings.Contains(string(b), `"limits":[]`) || !strings.Contains(string(b), `"firewall":[]`) {
		t.Fatalf("%s", b)
	}
	var p BansPush
	_ = json.Unmarshal([]byte(`{"bans":[],"shapes":[]}`), &p)
	if p.Limits != nil || p.Firewall != nil {
		t.Fatal("an older master's push must read as unchanged")
	}
}
