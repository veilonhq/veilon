package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/nodes"
)

func TestNodesAPI(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)

	code, r, _ := c.do("POST", api+"/inbounds", map[string]any{"remark": "VL", "protocol": "vless", "port": 4443, "security": "reality",
		"flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}})
	if code != 201 {
		t.Fatalf("inbound %d %v", code, r)
	}
	ib := r["id"].(float64)
	code, r, _ = c.do("POST", api+"/users", map[string]any{"username": "alice", "reset_strategy": "no_reset", "inbound_ids": []float64{ib}})
	if code != 201 {
		t.Fatalf("user %d %v", code, r)
	}

	code, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "DE", "address": "https://x"})
	if code != 400 {
		t.Fatalf("bad address accepted: %d %v", code, r)
	}
	code, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "DE", "address": "de.example.com", "inbound_ids": []float64{ib}})
	if code != 201 {
		t.Fatalf("create node %d %v", code, r)
	}
	inst := r["install"].(map[string]any)
	if !strings.Contains(inst["command"].(string), "node --token vn1.") {
		t.Fatalf("install %v", inst)
	}
	node := r["node"].(map[string]any)
	if node["state"].(map[string]any)["status"] != "waiting" || node["cert_sha256"] != nil {
		t.Fatalf("node %v", node)
	}
	id := int(node["id"].(float64))
	if code, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "de", "address": "1.2.3.4"}); code != 409 {
		t.Fatalf("duplicate %d %v", code, r)
	}

	// subscription links gain the node's address
	_, r, _ = c.do("GET", api+"/users/1/links", nil)
	links := r["links"].([]any)
	if len(links) != 2 || !strings.Contains(links[1].(map[string]any)["uri"].(string), "@de.example.com:4443") {
		t.Fatalf("links %v", links)
	}

	_, r, _ = c.do("GET", api+"/nodes", nil)
	var list []map[string]any
	_ = json.Unmarshal([]byte(r["_raw"].(string)), &list)
	if len(list) != 1 || list[0]["name"] != "DE" {
		t.Fatalf("list %v", r)
	}
	if code, r, _ = c.do("PATCH", api+"/nodes/"+itoa(id), map[string]any{"enabled": false}); code != 200 || r["enabled"] != false {
		t.Fatalf("patch %d %v", code, r)
	}
	_, r, _ = c.do("GET", api+"/users/1/links", nil)
	if n := len(r["links"].([]any)); n != 1 {
		t.Fatalf("disabled node still in links: %d", n)
	}
	if code, r, _ = c.do("POST", api+"/nodes/"+itoa(id)+"/install", nil); code != 200 || r["token"] == nil {
		t.Fatalf("reinstall %d %v", code, r)
	}

	// full-permission admins only
	c.do("POST", api+"/admins", map[string]any{"username": "helper", "password": "password123", "is_sudo": false})
	hc := &client{t: t, ts: c.ts}
	_, r, _ = hc.do("POST", api+"/auth/login", map[string]string{"username": "helper", "password": "password123"})
	hc.tok = r["token"].(string)
	for _, q := range [][2]string{{"GET", "/nodes"}, {"POST", "/nodes"}, {"DELETE", "/nodes/" + itoa(id)}} {
		if code, _, _ := hc.do(q[0], api+q[1], map[string]any{}); code != 403 {
			t.Fatalf("%s %s by a limited admin: %d", q[0], q[1], code)
		}
	}

	if code, _, _ := c.do("DELETE", api+"/nodes/"+itoa(id), nil); code != 204 {
		t.Fatalf("delete %d", code)
	}
	if code, _, _ := c.do("DELETE", api+"/nodes/"+itoa(id), nil); code != 404 {
		t.Fatalf("delete again %d", code)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
