package api

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// The per-user firewall through the API: the user's "firewall" field on
// create / PATCH, and GET/PUT/POST/DELETE /users/{id}/firewall; bad
// entries are 400 naming the entry and the reason; every change is
// audited.
func TestAPIUserFirewall(t *testing.T) {
	c, srv := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)
	code, m, _ := c.do("POST", api+"/users", map[string]any{"username": "alice", "firewall": []string{"Example.com", "1.2.3.4/32", "example.com"}})
	if code != 201 || !reflect.DeepEqual(m["firewall"], []any{"example.com", "1.2.3.4"}) {
		t.Fatalf("create %d %v", code, m)
	}
	id := strconv.Itoa(int(m["id"].(float64)))
	base := api + "/users/" + id + "/firewall"
	code, m, _ = c.do("GET", base, nil)
	es := m["entries"].([]any)
	if code != 200 || len(es) != 2 || m["max"].(float64) != 200 {
		t.Fatalf("get %d %v", code, m)
	}
	if e := es[0].(map[string]any); e["kind"] != "domain" || e["amneziawg"] != false {
		t.Fatalf("entry %v", e)
	}
	if e := es[1].(map[string]any); e["kind"] != "ip" || e["amneziawg"] != true {
		t.Fatalf("entry %v", e)
	}
	code, m, _ = c.do("POST", base, map[string]any{"entries": []string{"full:api.x.org", "1.2.3.4", "10.0.0.0/8"}})
	if code != 200 || len(m["entries"].([]any)) != 4 {
		t.Fatalf("add %d %v", code, m)
	}
	code, m, _ = c.do("POST", base, map[string]any{"entries": []string{"geosite:youtube"}})
	er, _ := m["error"].(map[string]any)
	if code != 400 || er["field"] != "firewall" || er["reason"] != "unsupported" || er["entry"] != "geosite:youtube" {
		t.Fatalf("bad entry %d %v", code, m)
	}
	code, m, _ = c.do("PATCH", api+"/users/"+id, map[string]any{"firewall": []string{"0.0.0.0/0"}})
	if er, _ := m["error"].(map[string]any); code != 400 || er["reason"] != "too_broad" {
		t.Fatalf("patch too broad %d %v", code, m)
	}
	code, m, _ = c.do("DELETE", base+"?entry="+url.QueryEscape("10.1.2.3/8"), nil)
	if code != 200 || len(m["entries"].([]any)) != 3 {
		t.Fatalf("delete one %d %v", code, m)
	}
	code, _, _ = c.do("DELETE", base+"?entry=nothere.com", nil)
	if code != 404 {
		t.Fatalf("delete missing %d", code)
	}
	code, m, _ = c.do("PUT", base, map[string]any{"entries": []string{"b.com", "a.com"}})
	if code != 200 || len(m["entries"].([]any)) != 2 {
		t.Fatalf("put %d %v", code, m)
	}
	// PATCH without the field keeps the list.
	code, m, _ = c.do("PATCH", api+"/users/"+id, map[string]any{"note": "x"})
	if code != 200 || !reflect.DeepEqual(m["firewall"], []any{"b.com", "a.com"}) {
		t.Fatalf("patch keeps %d %v", code, m)
	}
	code, m, _ = c.do("DELETE", base, nil)
	if code != 200 || len(m["entries"].([]any)) != 0 {
		t.Fatalf("clear %d %v", code, m)
	}
	code, m, _ = c.do("GET", api+"/users/"+id, nil)
	if code != 200 || m["firewall"] == nil || len(m["firewall"].([]any)) != 0 {
		t.Fatalf("user view %d %v", code, m["firewall"])
	}
	var logs []db.AuditLog
	srv.DB.Where("action = ?", "user.firewall").Order("id").Find(&logs)
	if len(logs) != 5 { // create, add, delete one, put, clear (failed calls and no-op PATCH are not logged)
		t.Fatalf("audit %d %+v", len(logs), logs)
	}
	if !strings.Contains(logs[1].Target, "+full:api.x.org,10.0.0.0/8") || !strings.Contains(logs[2].Target, "-10.0.0.0/8") {
		t.Fatalf("audit targets %q / %q", logs[1].Target, logs[2].Target)
	}
	code, _, _ = c.do("GET", api+"/users/999/firewall", nil)
	if code != 404 {
		t.Fatalf("missing user %d", code)
	}
}
