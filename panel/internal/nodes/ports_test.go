package nodes

import (
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Assigning inbounds from the node side keeps ports unique on the node.
func TestNodeInboundPorts(t *testing.T) {
	r := newRig(t)
	mkIb := func(remark string, port int) int64 {
		ib := db.Inbound{Remark: remark, Protocol: "vless", Port: port, Network: "tcp", Security: "none", Enabled: true}
		if err := r.gdb.Create(&ib).Error; err != nil {
			t.Fatal(err)
		}
		return ib.ID
	}
	a := mkIb("a443", 443)
	b := mkIb("b443", 443)
	c := mkIb("c62050", 62050)
	n := r.addNode(t, "Lightsail")
	r.gdb.Model(&db.Node{}).Where("id = ?", n.ID).Update("api_port", 62050)
	conflict := func(err error) {
		t.Helper()
		var ce *ConflictError
		if !errors.As(err, &ce) {
			t.Fatalf("want conflict, got %v", err)
		}
	}
	ids := func(x ...int64) Input { return Input{InboundIDs: &x} }
	if _, err := r.svc.Update(n.ID, ids(a)); err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.Update(n.ID, ids(a, b))
	conflict(err)
	_, err = r.svc.Update(n.ID, ids(a, c))
	conflict(err)
	// Moving the agent port onto an inbound already on the node.
	p := 443
	_, err = r.svc.Update(n.ID, Input{APIPort: &p})
	conflict(err)
	// Saving the node unchanged still works.
	name := "Lightsail"
	if _, err := r.svc.Update(n.ID, Input{Name: &name}); err != nil {
		t.Fatal(err)
	}
}
