package api

import (
	"strings"
	"testing"
)

// Both texts start empty, survive create, patch, get and list, and the
// description is limited to 1000 characters.
func TestUserNoteAndDescription(t *testing.T) {
	c, _ := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)

	code, m, _ := c.do("POST", api+"/users", map[string]any{"username": "plain"})
	if code != 201 || m["note"] != "" || m["description"] != "" {
		t.Fatalf("defaults not empty: %d %q %q", code, m["note"], m["description"])
	}
	code, m, _ = c.do("POST", api+"/users", map[string]any{"username": "texts", "note": "private\nline 2", "description": "خط اول\r\nline 2"})
	if code != 201 || m["note"] != "private\nline 2" || m["description"] != "خط اول\nline 2" {
		t.Fatalf("create: %d %q %q", code, m["note"], m["description"])
	}
	path := api + "/users/" + itoa(int(m["id"].(float64)))
	code, m, _ = c.do("PATCH", path, map[string]any{"description": "کانال پشتیبانی"})
	if code != 200 || m["description"] != "کانال پشتیبانی" || m["note"] != "private\nline 2" {
		t.Fatalf("patch: %d %v", code, m)
	}
	c.do("PATCH", path, map[string]any{"data_limit": 1 << 30})
	_, m, _ = c.do("GET", path, nil)
	if m["description"] != "کانال پشتیبانی" || m["note"] != "private\nline 2" {
		t.Fatalf("get: %v", m)
	}
	_, m, _ = c.do("GET", api+"/users?search=texts", nil)
	found := false
	for _, k := range []string{"users", "items"} {
		if l, ok := m[k].([]any); ok && len(l) == 1 {
			r := l[0].(map[string]any)
			found = r["description"] == "کانال پشتیبانی" && r["note"] == "private\nline 2"
		}
	}
	if !found {
		t.Fatalf("list: %v", m)
	}
	if code, _, _ = c.do("PATCH", path, map[string]any{"description": strings.Repeat("ک", 1000)}); code != 200 {
		t.Fatalf("1000 chars refused: %d", code)
	}
	if code, _, _ = c.do("PATCH", path, map[string]any{"description": strings.Repeat("ک", 1001)}); code != 400 {
		t.Fatalf("1001 chars accepted: %d", code)
	}
	code, m, _ = c.do("PATCH", path, map[string]any{"description": "", "note": ""})
	if code != 200 || m["description"] != "" || m["note"] != "" {
		t.Fatalf("clear: %d %v", code, m)
	}
}
