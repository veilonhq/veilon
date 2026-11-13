package api

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// VLESS Encryption through the API: key generation, create with
// encryption on (keys made on save), PATCH merges the nested object, the
// user's link carries the client string (never the server key), and an
// Xray-core older than 25.9.5 is refused.
func TestAPIVLESSEncryption(t *testing.T) {
	c, srv := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)

	code, m, _ := c.do("POST", api+"/inbounds/vless-enc-keys", map[string]any{"auth": "mlkem768"})
	if code != 200 || m["auth"] != "mlkem768" || len(m["public_key"].(string)) < 1500 || m["source"] != "go" {
		t.Fatalf("keys %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/inbounds/vless-enc-keys", map[string]any{"auth": "rsa"}); code != 400 {
		t.Fatalf("bad auth %d", code)
	}
	if code, m, _ := c.do("POST", api+"/inbounds/vless-enc-keys", nil); code != 200 || m["auth"] != "x25519" {
		t.Fatalf("default auth %d %v", code, m)
	}

	code, m, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "ENC", "protocol": "vless", "port": 2443, "network": "ws",
		"flow": "xtls-rprx-vision", "vless_encryption": map[string]any{"enabled": true}})
	if code != 201 {
		t.Fatalf("create %d %v", code, m)
	}
	e := m["vless_encryption"].(map[string]any)
	priv, enc := e["private_key"].(string), e["encryption"].(string)
	if e["enabled"] != true || priv == "" || !strings.HasPrefix(enc, "mlkem768x25519plus.native.0rtt.") || m["flow"] != "xtls-rprx-vision" {
		t.Fatalf("created %v", m)
	}
	id := int(m["id"].(float64))

	code, m, _ = c.do("PATCH", api+"/inbounds/"+strconv.Itoa(id), map[string]any{"vless_encryption": map[string]any{"mode": "random", "rtt": "1rtt"}})
	e = m["vless_encryption"].(map[string]any)
	if code != 200 || e["private_key"] != priv || e["mode"] != "random" || e["ticket_seconds"].(float64) != 0 || !strings.HasPrefix(e["encryption"].(string), "mlkem768x25519plus.random.1rtt.") {
		t.Fatalf("patch %d %v", code, m)
	}
	// Off while Vision runs over ws: refused (Vision needs the encryption there).
	if code, _, _ := c.do("PATCH", api+"/inbounds/"+strconv.Itoa(id), map[string]any{"vless_encryption": map[string]any{"enabled": false}}); code != 400 {
		t.Fatalf("off with vision over ws: %d", code)
	}

	code, m, _ = c.do("POST", api+"/users", map[string]any{"username": "alice", "inbound_ids": []int{id}})
	if code != 201 {
		t.Fatalf("user %d %v", code, m)
	}
	_, m, _ = c.do("GET", api+"/users/"+strconv.Itoa(int(m["id"].(float64)))+"/links", nil)
	uri := m["links"].([]any)[0].(map[string]any)["uri"].(string)
	u, _ := url.Parse(uri)
	if q := u.Query().Get("encryption"); !strings.HasPrefix(q, "mlkem768x25519plus.random.1rtt.") || strings.Contains(uri, priv) {
		t.Fatalf("link %s", uri)
	}

	// An old Xray-core on the main server: encryption refused, plain VLESS fine.
	srv.Inbounds.XrayVersion = func(int64) string { return "25.8.3" }
	code, m, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "OLD", "protocol": "vless", "port": 2444, "vless_encryption": map[string]any{"enabled": true}})
	if code != 400 || !strings.Contains(m["error"].(map[string]any)["message"].(string), "25.9.5") {
		t.Fatalf("old xray %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/inbounds", map[string]any{"remark": "OLD", "protocol": "vless", "port": 2444}); code != 201 {
		t.Fatalf("plain vless on old xray %d", code)
	}
}
