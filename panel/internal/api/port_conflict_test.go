package api

import (
	"testing"

	"github.com/veilonhq/veilon/panel/internal/nodes"
)

// A port conflict answers 409 naming the server that holds the port.
func TestInboundPortConflictAPI(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)
	code, r, _ := c.do("POST", api+"/nodes", map[string]any{"name": "Lightsail", "address": "ls.example.com"})
	if code != 201 {
		t.Fatalf("node %d %v", code, r)
	}
	nid := r["node"].(map[string]any)["id"].(float64)
	ib := func(remark string, extra map[string]any) (int, map[string]any) {
		b := map[string]any{"remark": remark, "protocol": "vless", "port": 443}
		for k, v := range extra {
			b[k] = v
		}
		code, r, _ := c.do("POST", api+"/inbounds", b)
		return code, r
	}
	if code, r := ib("M", nil); code != 201 {
		t.Fatalf("main: %d %v", code, r)
	}
	if code, r := ib("N", map[string]any{"node_ids": []float64{nid}}); code != 201 {
		t.Fatalf("node: %d %v", code, r)
	}
	code, r = ib("M2", nil)
	e := r["error"].(map[string]any)
	if code != 409 || e["server"] != "main" || e["port"] != float64(443) || e["message"] != "Port 443 is already used on the main server" {
		t.Fatalf("main conflict: %d %v", code, r)
	}
	code, r = ib("N2", map[string]any{"node_ids": []float64{nid}})
	e = r["error"].(map[string]any)
	if code != 409 || e["server"] != "node" || e["node_name"] != "Lightsail" || e["node_id"] != nid || e["inbound"] != "N" ||
		e["message"] != "Port 443 is already used on node Lightsail" {
		t.Fatalf("node conflict: %d %v", code, r)
	}
}
