package inbounds

import (
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func portSvc(t *testing.T) (*Service, int64, int64) {
	t.Helper()
	gdb := testutil.DB(t)
	a := db.Node{Name: "Lightsail", Address: "1.2.3.4", APIPort: 62050, Enabled: true}
	b := db.Node{Name: "Hetzner", Address: "5.6.7.8", APIPort: 62051, Enabled: true}
	if err := gdb.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return &Service{DB: gdb, ReservedPorts: func() []int { return []int{2053, 10085} }}, a.ID, b.ID
}

// placed builds an inbound on nodes; main says whether the main server
// runs it too.
func placed(remark string, port int, main bool, nodes ...int64) *View {
	v := mk(remark, VLESS, port)
	v.NodeIDs = append([]int64{}, nodes...)
	v.ServeMain = main
	return v
}

func wantPortConflict(t *testing.T, err error, msg string) *ConflictError {
	t.Helper()
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Field != "port" {
		t.Fatalf("want port conflict %q, got %v", msg, err)
	}
	if ce.Msg != msg {
		t.Fatalf("message: got %q want %q", ce.Msg, msg)
	}
	return ce
}

func TestPortPerServer(t *testing.T) {
	s, n1, n2 := portSvc(t)
	if _, err := s.Create(placed("main443", 443, true)); err != nil {
		t.Fatal(err)
	}
	// Main 443 + node-only 443: different servers, allowed.
	if _, err := s.Create(placed("node443", 443, false, n1)); err != nil {
		t.Fatalf("main 443 and node 443: %v", err)
	}
	// A second inbound on main 443 is rejected, naming the main server.
	_, err := s.Create(placed("main443b", 443, true))
	ce := wantPortConflict(t, err, "Port 443 is already used on the main server")
	if ce.Server != "main" || ce.Other != "main443" || ce.Port != 443 {
		t.Fatalf("conflict fields %+v", ce)
	}
	// Two on the same node are rejected, naming the node.
	_, err = s.Create(placed("node443b", 443, false, n1))
	ce = wantPortConflict(t, err, "Port 443 is already used on node Lightsail")
	if ce.Server != "node" || ce.NodeID != n1 || ce.NodeName != "Lightsail" || ce.Other != "node443" {
		t.Fatalf("conflict fields %+v", ce)
	}
	// Nodes 1+2: node 1 already has 443.
	_, err = s.Create(placed("both", 443, false, n1, n2))
	wantPortConflict(t, err, "Port 443 is already used on node Lightsail")
	// Node 2 alone is free.
	if _, err := s.Create(placed("n2", 443, false, n2)); err != nil {
		t.Fatalf("node 2 443: %v", err)
	}
	// Nodes 1+2 on another port where only node 2 has it.
	if _, err := s.Create(placed("n2-8443", 8443, false, n2)); err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(placed("both8443", 8443, false, n1, n2))
	wantPortConflict(t, err, "Port 8443 is already used on node Hetzner")
	// Main + node (serve_main) conflicts with the main server's 443 too.
	_, err = s.Create(placed("legacy", 443, true, n2))
	wantPortConflict(t, err, "Port 443 is already used on the main server")
	// Another protocol on the same server still conflicts.
	sh := placed("ss", 443, false, n2)
	sh.Protocol = Shadowsocks
	_, err = s.Create(sh)
	wantPortConflict(t, err, "Port 443 is already used on node Hetzner")
}

func TestPortReservedPerServer(t *testing.T) {
	s, n1, n2 := portSvc(t)
	// Main server: panel port and the core API ports.
	for _, p := range []int{2053, 10085} {
		_, err := s.Create(placed("m", p, true))
		ce := wantPortConflict(t, err, "Port "+itoa(p)+" is already used on the main server")
		if ce.Other != "" {
			t.Fatalf("reserved port names an inbound: %+v", ce)
		}
	}
	// The panel port is free on a node-only inbound.
	if _, err := s.Create(placed("n-2053", 2053, false, n1)); err != nil {
		t.Fatalf("panel port on a node: %v", err)
	}
	// A node's agent port is reserved on that node only.
	_, err := s.Create(placed("agent", 62050, false, n1))
	wantPortConflict(t, err, "Port 62050 is already used on node Lightsail")
	_, err = s.Create(placed("agent12", 62050, false, n2, n1))
	wantPortConflict(t, err, "Port 62050 is already used on node Lightsail")
	if _, err := s.Create(placed("agent-on-2", 62050, false, n2)); err != nil {
		t.Fatalf("node 1 agent port on node 2: %v", err)
	}
	if _, err := s.Create(placed("agent-on-main", 62051, true)); err != nil {
		t.Fatalf("node agent port on main: %v", err)
	}
}

func TestPortRecheckOnEdit(t *testing.T) {
	s, n1, n2 := portSvc(t)
	if _, err := s.Create(placed("main443", 443, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(placed("n1-443", 443, false, n1)); err != nil {
		t.Fatal(err)
	}
	x, err := s.Create(placed("x", 8443, false, n2))
	if err != nil {
		t.Fatal(err)
	}
	// Port change onto a server that has it.
	x.Port = 443
	if _, err := s.Update(x); err != nil {
		t.Fatalf("443 on node 2 only: %v", err)
	}
	// Adding node 1 (same port) is re-checked.
	x.NodeIDs = []int64{n1, n2}
	_, err = s.Update(x)
	wantPortConflict(t, err, "Port 443 is already used on node Lightsail")
	// Turning on "also run on main server" is re-checked.
	x.NodeIDs = []int64{n2}
	x.ServeMain = true
	_, err = s.Update(x)
	wantPortConflict(t, err, "Port 443 is already used on the main server")
	// Moving to the main server only (no nodes) is re-checked.
	x.NodeIDs, x.ServeMain = []int64{}, false
	_, err = s.Update(x)
	wantPortConflict(t, err, "Port 443 is already used on the main server")
	// Moving onto node 1's agent port is re-checked.
	x.Port, x.NodeIDs, x.ServeMain = 62050, []int64{n2}, false
	if _, err := s.Update(x); err != nil {
		t.Fatalf("62050 on node 2: %v", err)
	}
	x.NodeIDs = []int64{n1}
	_, err = s.Update(x)
	wantPortConflict(t, err, "Port 62050 is already used on node Lightsail")
	// Saving without changes, and dropping a node, still work.
	x.Port, x.NodeIDs = 443, []int64{n2}
	got, err := s.Update(x)
	if err != nil {
		t.Fatal(err)
	}
	if got.OnMain || len(got.NodeIDs) != 1 || got.NodeIDs[0] != n2 {
		t.Fatalf("placement %+v", got)
	}
	if _, err := s.Update(got); err != nil {
		t.Fatalf("unchanged update: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
