package api

import (
	"testing"

	"github.com/veilonhq/veilon/panel/internal/nodes"
)

// Choosing nodes in an inbound request without "serve_main" runs the
// inbound on those nodes only; an explicit serve_main is kept (API clients
// that want both); no nodes always means the main server.
func TestInboundPlacementAPI(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)
	code, r, _ := c.do("POST", api+"/nodes", map[string]any{"name": "DE", "address": "de.example.com"})
	if code != 201 {
		t.Fatalf("node %d %v", code, r)
	}
	nid := r["node"].(map[string]any)["id"].(float64)
	vl := func(remark string, port int, extra map[string]any) map[string]any {
		b := map[string]any{"remark": remark, "protocol": "vless", "port": port, "security": "reality",
			"flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	code, r, _ = c.do("POST", api+"/inbounds", vl("A", 4441, map[string]any{"node_ids": []float64{nid}}))
	if code != 201 || r["on_main"] != false || r["serve_main"] != false {
		t.Fatalf("nodes only: %d %v", code, r)
	}
	code, r, _ = c.do("POST", api+"/inbounds", vl("B", 4442, map[string]any{"node_ids": []float64{nid}, "serve_main": true}))
	if code != 201 || r["on_main"] != true {
		t.Fatalf("both: %d %v", code, r)
	}
	code, r, _ = c.do("POST", api+"/inbounds", vl("C", 4443, nil))
	if code != 201 || r["on_main"] != true || len(r["node_ids"].([]any)) != 0 {
		t.Fatalf("main: %d %v", code, r)
	}
	// A PATCH that does not touch node_ids keeps the placement.
	code, r, _ = c.do("PATCH", api+"/inbounds/1", map[string]any{"remark": "A2"})
	if code != 200 || r["on_main"] != false || len(r["node_ids"].([]any)) != 1 {
		t.Fatalf("patch keeps placement: %d %v", code, r)
	}
	// Moving B to the node only (node_ids without serve_main).
	code, r, _ = c.do("PATCH", api+"/inbounds/2", map[string]any{"node_ids": []float64{nid}})
	if code != 200 || r["on_main"] != false {
		t.Fatalf("patch to nodes: %d %v", code, r)
	}
	// TUIC cannot run on a node.
	code, r, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "T", "protocol": "tuic", "port": 4450, "node_ids": []float64{nid}})
	if code != 400 {
		t.Fatalf("tuic on node: %d %v", code, r)
	}
}
