package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// Advanced inbound settings through the API: key generation, create,
// PATCH replaces "advanced" whole (a removed header is gone), validation
// errors are 400, the reverse bridge config downloads.
func TestAPIAdvanced(t *testing.T) {
	c, _ := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)

	code, m, _ := c.do("POST", api+"/inbounds/mldsa65-keys", nil)
	if code != 200 || len(m["server"].(string)) != 43 || len(m["client"].(string)) < 2600 {
		t.Fatalf("mldsa65 %d %v", code, m)
	}
	seed := m["server"].(string)
	code, m, _ = c.do("POST", api+"/inbounds/ech-keys", map[string]any{"server_name": "outer.example.com"})
	if code != 200 || m["server"] == "" || m["client"] == "" {
		t.Fatalf("ech %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/inbounds/ech-keys", map[string]any{"server_name": "bad name"}); code != 400 {
		t.Fatalf("bad ech name %d", code)
	}

	code, m, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "R", "protocol": "vless", "port": 2444, "security": "reality",
		"reality": map[string]any{"dest": "www.example.com"}, "flow": "xtls-rprx-vision",
		"advanced": map[string]any{"reality": map[string]any{"mldsa65_seed": seed}, "fallbacks": []any{map[string]any{"dest": "8080"}},
			"reverse": map[string]any{"enabled": true, "route_self": true}}})
	if code != 201 {
		t.Fatalf("create %d %v", code, m)
	}
	adv := m["advanced"].(map[string]any)
	if adv["reality"].(map[string]any)["mldsa65_verify"] == "" || len(adv["reverse"].(map[string]any)["uuid"].(string)) != 36 {
		t.Fatalf("advanced %v", adv)
	}
	id := strconv.Itoa(int(m["id"].(float64)))

	// Bridge config.
	code, m, hd := c.do("GET", api+"/inbounds/"+id+"/reverse-bridge", nil)
	raw, _ := json.Marshal(m)
	if code != 200 || !strings.Contains(string(raw), `"reverse"`) || !strings.Contains(string(raw), adv["reverse"].(map[string]any)["uuid"].(string)) || !strings.Contains(hd.Get("Content-Disposition"), "veilon-bridge-") {
		t.Fatalf("bridge %d %s", code, raw)
	}

	code, m, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "W", "protocol": "vmess", "port": 2445, "network": "ws",
		"transport": map[string]any{"path": "/w"}, "advanced": map[string]any{"ws": map[string]any{"headers": map[string]any{"X-A": "1", "X-B": "2"}}}})
	if code != 201 {
		t.Fatalf("create ws %d %v", code, m)
	}
	wid := strconv.Itoa(int(m["id"].(float64)))
	code, m, _ = c.do("PATCH", api+"/inbounds/"+wid, map[string]any{"advanced": map[string]any{"ws": map[string]any{"headers": map[string]any{"X-B": "3"}}}})
	h, _ := json.Marshal(m["advanced"])
	if code != 200 || strings.Contains(string(h), "X-A") || !strings.Contains(string(h), `"X-B":"3"`) {
		t.Fatalf("patch replace %d %s", code, h)
	}
	// Other fields patched alone keep advanced.
	code, m, _ = c.do("PATCH", api+"/inbounds/"+wid, map[string]any{"remark": "W2"})
	if code != 200 || m["advanced"] == nil {
		t.Fatalf("patch keep %d %v", code, m)
	}
	// Clear.
	code, m, _ = c.do("PATCH", api+"/inbounds/"+wid, map[string]any{"advanced": nil})
	if code != 200 || m["advanced"] != nil {
		t.Fatalf("patch clear %d %v", code, m)
	}
	if code, _, _ := c.do("GET", api+"/inbounds/"+wid+"/reverse-bridge", nil); code != 409 {
		t.Fatalf("bridge without reverse %d", code)
	}
	if code, _, _ := c.do("PATCH", api+"/inbounds/"+wid, map[string]any{"advanced": map[string]any{"fallbacks": []any{map[string]any{"dest": "80"}}}}); code != 400 {
		t.Fatalf("fallback on ws %d", code)
	}
}
