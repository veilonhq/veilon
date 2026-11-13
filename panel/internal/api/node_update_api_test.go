package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/nodes"
)

func TestNodeUpdateAPI(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}, Version: "v0.4.0", RequestTimeout: time.Second}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)
	_, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "DE", "address": "127.0.0.1", "api_port": 1})
	id := int(r["node"].(map[string]any)["id"].(float64))
	code, r, _ := c.do("POST", api+fmt.Sprintf("/nodes/%d/update", id), nil)
	if e, _ := r["error"].(map[string]any); code != 409 || e["code"] != "offline" {
		t.Fatalf("offline node: %d %v", code, r)
	}
	if code, r, _ := c.do("POST", api+"/nodes/999/update", nil); code != 404 {
		t.Fatalf("missing node: %d %v", code, r)
	}
	// nothing outdated is known yet (the node was never reached)
	code, r, _ = c.do("POST", api+"/nodes/update-outdated", nil)
	if code != 200 || r["started"] != float64(0) || len(r["results"].([]any)) != 0 {
		t.Fatalf("update-outdated: %d %v", code, r)
	}
	// sudo only
	if code, _, _ := c.do("POST", api+"/admins", map[string]any{"username": "operator", "password": "password123", "is_sudo": false}); code != 201 {
		t.Fatalf("create admin %d", code)
	}
	hc := &client{t: t, ts: c.ts}
	_, r, _ = hc.do("POST", api+"/auth/login", map[string]string{"username": "operator", "password": "password123"})
	hc.tok = r["token"].(string)
	c = hc
	for _, p := range []string{fmt.Sprintf("/nodes/%d/update", id), "/nodes/update-outdated"} {
		if code, _, _ := c.do("POST", api+p, nil); code != 403 {
			t.Fatalf("%s as non-sudo: %d", p, code)
		}
	}
}

func TestNodeXrayAPI(t *testing.T) {
	c, srv := setup(t)
	m := &nodes.Manager{DB: srv.DB, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}, Version: "v0.4.0", RequestTimeout: time.Second}
	srv.Nodes = &nodes.Service{DB: srv.DB, PKI: m.PKI, Manager: m}
	const api = "/secret/api/v1"
	_, r, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = r["token"].(string)
	_, r, _ = c.do("POST", api+"/nodes", map[string]any{"name": "DE", "address": "127.0.0.1", "api_port": 1})
	id := int(r["node"].(map[string]any)["id"].(float64))
	p := fmt.Sprintf("/nodes/%d/xray", id)
	code, r, _ := c.do("POST", api+p, map[string]any{"version": "latest"})
	if e, _ := r["error"].(map[string]any); code != 400 || e["code"] != "bad_version" {
		t.Fatalf("bad version: %d %v", code, r)
	}
	code, r, _ = c.do("POST", api+p, map[string]any{"version": "26.10.31"})
	if e, _ := r["error"].(map[string]any); code != 409 || e["code"] != "offline" {
		t.Fatalf("offline node: %d %v", code, r)
	}
	if code, r, _ := c.do("POST", api+"/nodes/999/xray", map[string]any{"version": "v26.10.31"}); code != 404 {
		t.Fatalf("missing node: %d %v", code, r)
	}
	if code, _, _ := c.do("POST", api+"/admins", map[string]any{"username": "operator", "password": "password123", "is_sudo": false}); code != 201 {
		t.Fatalf("create admin %d", code)
	}
	hc := &client{t: t, ts: c.ts}
	_, r, _ = hc.do("POST", api+"/auth/login", map[string]string{"username": "operator", "password": "password123"})
	hc.tok = r["token"].(string)
	if code, _, _ := hc.do("POST", api+p, map[string]any{"version": "v26.10.31"}); code != 403 {
		t.Fatalf("as non-sudo: %d", code)
	}
}
