package sub

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/nodes"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

func names(ps []Proxy) map[string]string {
	out := map[string]string{}
	for _, p := range ps {
		out[p.Name] = p.Server
	}
	return out
}

// Placement end to end: where an inbound runs decides what the main
// server's Xray listens on, what each node runs and which links users get.
func TestInboundPlacement(t *testing.T) {
	g := testutil.DB(t)
	dir := t.TempDir()
	svc := &inbounds.Service{DB: g}
	now := time.Now().UTC()
	n1 := db.Node{Name: "Frankfurt", Address: "10.0.0.1", APIPort: 62050, Enabled: true, CreatedAt: now, UpdatedAt: now}
	n2 := db.Node{Name: "Paris", Address: "10.0.0.2", APIPort: 62050, Enabled: true, CreatedAt: now, UpdatedAt: now}
	g.Create(&n1)
	g.Create(&n2)
	reality := func(remark string, port int) *inbounds.View {
		v := inbounds.Defaults()
		v.Remark, v.Protocol, v.Port, v.Security, v.Flow = remark, "vless", port, "reality", "xtls-rprx-vision"
		v.Reality = &inbounds.Reality{Dest: "www.example.com"}
		return &v
	}
	mainIB, err := svc.Create(reality("Main", 4431))
	if err != nil {
		t.Fatal(err)
	}
	v := reality("Edge", 4432)
	v.NodeIDs, v.ServeMain = []int64{n2.ID, n1.ID, n1.ID}, false
	edge, err := svc.Create(v)
	if err != nil {
		t.Fatal(err)
	}
	if edge.OnMain || len(edge.NodeIDs) != 2 || edge.NodeIDs[0] != n1.ID || !mainIB.OnMain || len(mainIB.NodeIDs) != 0 {
		t.Fatalf("placement views: main %+v edge %+v", mainIB, edge)
	}
	// An inbound assigned to a node before placement existed (serve_main
	// true, the migration default) is still served by the main server, but
	// its links point at its node only.
	legacy, err := svc.Create(reality("Legacy", 4433))
	if err != nil {
		t.Fatal(err)
	}
	g.Create(&db.NodeInbound{NodeID: n1.ID, InboundID: legacy.ID})

	u := db.User{Username: "alice", Status: db.StatusActive, UUID: "8c5f8a2e-7a3c-4d5e-9f00-112233445566", Password: "pw", SubToken: "tok", CreatedAt: now, UpdatedAt: now}
	g.Create(&u)
	for _, id := range []int64{mainIB.ID, edge.ID, legacy.ID} {
		g.Create(&db.UserInbound{UserID: u.ID, InboundID: id})
	}

	in, err := xray.LoadMainInput(g, dir, 62000)
	if err != nil {
		t.Fatal(err)
	}
	onMain := map[int64]bool{}
	for _, ib := range in.Inbounds {
		onMain[ib.ID] = true
	}
	if !onMain[mainIB.ID] || onMain[edge.ID] || !onMain[legacy.ID] {
		t.Fatalf("main server inbounds: %v", onMain)
	}
	if _, _, err := xray.NodeConfig(g, dir, []int64{edge.ID}, 62051); err != nil {
		t.Fatalf("node config: %v", err)
	}

	s := &Service{DB: g}
	_, ibs, err := s.Resolve("tok")
	if err != nil {
		t.Fatal(err)
	}
	got := names(BuildProxiesX(&u, ibs, db.Settings{ServerAddress: "main.example.com"}, "", s.Extras(&u, ibs)))
	want := map[string]string{
		"Main": "main.example.com", "Edge · Frankfurt": "10.0.0.1", "Edge · Paris": "10.0.0.2",
		"Legacy · Frankfurt": "10.0.0.1",
	}
	if len(got) != len(want) {
		t.Fatalf("links %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("link %q -> %q, want %q (all: %v)", k, got[k], v, got)
		}
	}

	// Moving an inbound from nodes back to the main server.
	e, _ := svc.Get(edge.ID)
	e.NodeIDs = []int64{}
	if e, err = svc.Update(e); err != nil || !e.OnMain || !e.ServeMain {
		t.Fatalf("back to main: %+v %v", e, err)
	}
	// ... and to a node again.
	e.NodeIDs, e.ServeMain = []int64{n1.ID}, false
	if e, err = svc.Update(e); err != nil || e.OnMain {
		t.Fatalf("to node: %+v %v", e, err)
	}

	// Validation: unknown node, protocol a node cannot run.
	bad := reality("Bad", 4434)
	bad.NodeIDs = []int64{999}
	if _, err := svc.Create(bad); err == nil {
		t.Fatal("unknown node accepted")
	}
	tuic := inbounds.Defaults()
	tuic.Remark, tuic.Protocol, tuic.Port, tuic.NodeIDs = "T", inbounds.TUIC, 4435, []int64{n1.ID}
	if _, err := svc.Create(&tuic); err == nil {
		t.Fatal("tuic on a node accepted")
	}

	// Deleting the only node of a node-only inbound puts it back on the
	// main server.
	pki := &nodes.PKI{Dir: t.TempDir()}
	ns := &nodes.Service{DB: g, PKI: pki, Manager: &nodes.Manager{DB: g, PKI: pki, RequestTimeout: time.Second}}
	if err := ns.Delete(n1.ID); err != nil {
		t.Fatal(err)
	}
	e, _ = svc.Get(edge.ID)
	if !e.OnMain || len(e.NodeIDs) != 0 {
		t.Fatalf("after node delete: %+v", e)
	}
}
