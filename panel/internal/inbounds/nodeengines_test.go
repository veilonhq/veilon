package inbounds

import (
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func onNodes(remark, proto string, port int, main bool, nodes ...int64) *View {
	v := placed(remark, port, main, nodes...)
	v.Protocol = proto
	return v
}

// AmneziaWG and MTProto inbounds can run on nodes, and their ports are
// unique per server like every other inbound's.
func TestEngineInboundsOnNodesPorts(t *testing.T) {
	s, n1, n2 := portSvc(t)
	if _, err := s.Create(placed("v-main", 443, true)); err != nil {
		t.Fatal(err)
	}
	awg, err := s.Create(onNodes("awg", AmneziaWG, 443, false, n1))
	if err != nil {
		t.Fatalf("awg node-only on 443 next to main 443: %v", err)
	}
	if awg.OnMain || len(awg.NodeIDs) != 1 {
		t.Fatalf("placement %+v", awg)
	}
	// Same node, same port: refused whatever the protocol.
	_, err = s.Create(onNodes("mt", MTProto, 443, false, n1))
	wantPortConflict(t, err, "Port 443 is already used on node Lightsail")
	_, err = s.Create(placed("v-n1", 443, false, n1))
	wantPortConflict(t, err, "Port 443 is already used on node Lightsail")
	// Also on the main server: refused there.
	_, err = s.Create(onNodes("mt-both", MTProto, 443, true, n2))
	wantPortConflict(t, err, "Port 443 is already used on the main server")
	// A node's agent port is taken.
	_, err = s.Create(onNodes("mt-agent", MTProto, 62050, false, n1))
	wantPortConflict(t, err, "Port 62050 is already used on node Lightsail")
	if _, err := s.Create(onNodes("mt-n2", MTProto, 443, false, n2)); err != nil {
		t.Fatal(err)
	}
	// TUIC still cannot run on a node.
	if _, err := s.Create(onNodes("tuic", TUIC, 9443, false, n1)); err == nil {
		t.Fatal("tuic accepted on a node")
	}
	// Moving the node-only AmneziaWG inbound back to main: 443 is taken.
	if err := s.CheckMoveToMain([]int64{awg.ID}); err == nil {
		t.Fatal("move to main over a taken port accepted")
	}
}

// A node whose agent is known to lack the capability is refused for new
// AmneziaWG / MTProto placements; unknown nodes, Xray inbounds and nodes
// the inbound already ran on are not.
func TestEngineInboundsNeedAgentCapability(t *testing.T) {
	s, n1, n2 := portSvc(t)
	caps := map[int64][]string{n1: {"update", "online"}} // n1: a v0.3.7 agent; n2: never reached
	s.NodeCaps = func(id int64) ([]string, bool) { c, ok := caps[id]; return c, ok }
	_, err := s.Create(onNodes("awg", AmneziaWG, 5000, false, n1))
	var na *NodeAgentError
	if !errors.As(err, &na) || na.NodeID != n1 || na.NodeName != "Lightsail" || na.Protocol != AmneziaWG {
		t.Fatalf("want node agent error, got %v", err)
	}
	if _, err := s.Create(onNodes("mt", MTProto, 5001, false, n1)); !errors.As(err, &na) {
		t.Fatalf("mtproto: %v", err)
	}
	if _, err := s.Create(placed("vless", 5002, false, n1)); err != nil {
		t.Fatalf("xray inbound on an old agent: %v", err)
	}
	v, err := s.Create(onNodes("awg2", AmneziaWG, 5003, false, n2))
	if err != nil {
		t.Fatalf("unknown node: %v", err)
	}
	// n2 turns out old: the inbound stays editable on it.
	caps[n2] = nil
	v.Remark = "awg2-renamed"
	if _, err := s.Update(v); err != nil {
		t.Fatalf("edit on a node it already runs on: %v", err)
	}
	// ...but cannot be added to n1.
	v.NodeIDs = []int64{n1, n2}
	if _, err := s.Update(v); !errors.As(err, &na) || na.NodeID != n1 {
		t.Fatalf("adding an old node: %v", err)
	}
	// Once n1 is updated it is accepted.
	caps[n1] = []string{CapAWG, CapMTProto}
	if _, err := s.Update(v); err != nil {
		t.Fatal(err)
	}
	var rows []db.NodeInbound
	s.DB.Where("inbound_id = ?", v.ID).Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("placement rows %v", rows)
	}
}
