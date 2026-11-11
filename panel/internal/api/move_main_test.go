package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/nodes"
)

// Inbounds that ran only on a node go back to the main server when their
// last node is deleted or drops them; a port already taken on the main
// server refuses that with a 409 naming the port and the inbounds.
func TestNodeRemovalChecksMainPorts(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m, CheckMoveToMain: srv.Inbounds.CheckMoveToMain}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)
	node := func(name string) float64 {
		code, r, _ := c.do("POST", api+"/nodes", map[string]any{"name": name, "address": strings.ToLower(name) + ".example.com"})
		if code != 201 {
			t.Fatalf("node %d %v", code, r)
		}
		return r["node"].(map[string]any)["id"].(float64)
	}
	de, fr := node("DE"), node("FR")
	ib := func(remark string, port int, ids ...float64) float64 {
		b := map[string]any{"remark": remark, "protocol": "vless", "port": port, "security": "reality",
			"flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}, "node_ids": ids}
		code, r, _ := c.do("POST", api+"/inbounds", b)
		if code != 201 {
			t.Fatalf("inbound %s %d %v", remark, code, r)
		}
		return r["id"].(float64)
	}
	ib("A", 4441)               // main server
	b := ib("B", 4441, de)      // node only, same port: fine while on DE
	cc := ib("C", 4442, de, fr) // keeps FR
	d := ib("D", 9000, de)      // the panel port on the main server
	ib("E", 4443, fr)           // FR only, untouched
	conflict := func(code int, r map[string]any, port float64, moving, other string) {
		t.Helper()
		e, _ := r["error"].(map[string]any)
		if code != 409 || e["code"] != "main_port_conflict" || e["port"] != port || e["server"] != "main" ||
			e["moving_inbound"] != moving || e["inbound"] != other || !strings.Contains(e["message"].(string), fmt.Sprintf("port %d", int(port))) {
			t.Fatalf("want conflict on %v (%s vs %q): %d %v", port, moving, other, code, r)
		}
	}
	// deleting DE would put B on 4441 next to A
	code, r, _ := c.do("DELETE", api+fmt.Sprintf("/nodes/%d", int(de)), nil)
	conflict(code, r, 4441, "B", "A")
	// the refused delete changed nothing: DE still runs B
	var n int64
	srv.DB.Table("node_inbounds").Where("node_id = ? AND inbound_id = ?", int64(de), int64(b)).Count(&n)
	if n != 1 {
		t.Fatal("refused delete dropped B from DE")
	}
	// the API way of dropping B from DE is refused the same way
	code, r, _ = c.do("PATCH", api+fmt.Sprintf("/nodes/%d", int(de)), map[string]any{"inbound_ids": []float64{cc, d}})
	conflict(code, r, 4441, "B", "A")
	// dropping C is fine: FR still runs it
	if code, r, _ := c.do("PATCH", api+fmt.Sprintf("/nodes/%d", int(de)), map[string]any{"inbound_ids": []float64{b, d}}); code != 200 {
		t.Fatalf("drop C: %d %v", code, r)
	}
	// B moves to a free port; D still holds the main server's panel port
	if code, r, _ := c.do("PATCH", api+fmt.Sprintf("/inbounds/%d", int(b)), map[string]any{"port": 4451}); code != 200 {
		t.Fatalf("B port: %d %v", code, r)
	}
	code, r, _ = c.do("DELETE", api+fmt.Sprintf("/nodes/%d", int(de)), nil)
	conflict(code, r, 9000, "D", "")
	if !strings.Contains(r["error"].(map[string]any)["message"].(string), "reserved") {
		t.Fatalf("reserved message: %v", r)
	}
	if code, r, _ := c.do("DELETE", api+fmt.Sprintf("/inbounds/%d", int(d)), nil); code != 204 && code != 200 {
		t.Fatalf("delete D: %d %v", code, r)
	}
	if code, r, _ := c.do("DELETE", api+fmt.Sprintf("/nodes/%d", int(de)), nil); code != 204 {
		t.Fatalf("delete DE: %d %v", code, r)
	}
	code, r, _ = c.do("GET", api+fmt.Sprintf("/inbounds/%d", int(b)), nil)
	if code != 200 || r["on_main"] != true {
		t.Fatalf("B back on the main server: %d %v", code, r)
	}
	code, r, _ = c.do("GET", api+fmt.Sprintf("/inbounds/%d", int(cc)), nil)
	if code != 200 || r["on_main"] != false || len(r["node_ids"].([]any)) != 1 {
		t.Fatalf("C stays on FR: %d %v", code, r)
	}
}

// Two node-only inbounds on the same port cannot both move to the main
// server.
func TestNodeRemovalMovingInboundsClash(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m, CheckMoveToMain: srv.Inbounds.CheckMoveToMain}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)
	_, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "DE", "address": "de.example.com"})
	de := r["node"].(map[string]any)["id"].(float64)
	_, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "FR", "address": "fr.example.com"})
	fr := r["node"].(map[string]any)["id"].(float64)
	for _, x := range []struct {
		name string
		node float64
	}{{"X", de}, {"Y", fr}} {
		code, r, _ := c.do("POST", api+"/inbounds", map[string]any{"remark": x.name, "protocol": "vless", "port": 4470, "security": "reality",
			"flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}, "node_ids": []float64{x.node}})
		if code != 201 {
			t.Fatalf("%s %d %v", x.name, code, r)
		}
	}
	// deleting DE moves X alone: fine
	if code, r, _ := c.do("DELETE", api+fmt.Sprintf("/nodes/%d", int(de)), nil); code != 204 {
		t.Fatalf("delete DE: %d %v", code, r)
	}
	// now Y would land on 4470 next to X
	code, r, _ := c.do("DELETE", api+fmt.Sprintf("/nodes/%d", int(fr)), nil)
	if e, _ := r["error"].(map[string]any); code != 409 || e["moving_inbound"] != "Y" || e["inbound"] != "X" {
		t.Fatalf("Y vs X: %d %v", code, r)
	}
}
