package cores

import (
	"testing"

	"github.com/veilonhq/veilon/panel/internal/awgd"
	"github.com/veilonhq/veilon/panel/internal/guard"
)

// The AmneziaWG peers reach the speed limits with their interface, tunnel
// address and user, on the main server and from a node's spec.
func TestAWGPeerListAndNodeSpec(t *testing.T) {
	it := awgd.Iface{Name: "vawg4", Peers: []awgd.Peer{
		{PublicKey: "A", AllowedIP: "10.66.66.2/32"},
		{PublicKey: "B", AllowedIP: "10.66.66.3/32"},
		{PublicKey: "C", AllowedIP: "10.66.66.4/32"}, // no owner
	}}
	got := AWGPeerList([]awgd.Iface{it}, map[string]int64{"vawg4/A": 5, "vawg4/B": 6})
	want := []guard.AWGPeer{{Iface: "vawg4", Addr: "10.66.66.2", UserID: 5}, {Iface: "vawg4", Addr: "10.66.66.3", UserID: 6}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("peers %+v", got)
	}
	var fromNode []guard.AWGPeer
	e := &NodeEngines{Dir: t.TempDir(), Self: "/nonexistent", MTGBin: "/nonexistent", AWGPeers: func(p []guard.AWGPeer) { fromNode = p }}
	e.Set(NodeSpec{AWG: []NodeAWG{{InboundID: 4, Iface: it, Owners: map[string]int64{"A": 5, "B": 6}}}})
	if len(fromNode) != 2 || fromNode[0] != want[0] || fromNode[1] != want[1] {
		t.Fatalf("node peers %+v", fromNode)
	}
}
