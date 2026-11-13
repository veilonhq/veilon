package api

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A SOCKS inbound and users with and without a custom proxy login through
// the API: links carry the login, a duplicate username is 409 on the
// proxy_username field, bad values are 400.
func TestAPISocksAndProxyLogin(t *testing.T) {
	c, _ := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)
	code, m, _ := c.do("POST", api+"/inbounds", map[string]any{"remark": "SK", "protocol": "socks", "port": 1080, "settings": map[string]any{"udp": true}})
	if code != 201 || m["protocol"] != "socks" || m["security"] != "none" {
		t.Fatalf("create socks %d %v", code, m)
	}
	ib := m["id"].(float64)
	code, m, _ = c.do("POST", api+"/users", map[string]any{"username": "alice", "inbound_ids": []float64{ib},
		"proxy_username": "ali_px", "proxy_password": "Secret-123"})
	if code != 201 || m["proxy_username"] != "ali_px" || m["proxy_password"] != "Secret-123" {
		t.Fatalf("create user %d %v", code, m)
	}
	id := int(m["id"].(float64))
	_, m, _ = c.do("GET", api+"/users/"+strconv.Itoa(id)+"/links", nil)
	links := m["links"].([]any)
	uri := links[0].(map[string]any)["uri"].(string)
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "socks" {
		t.Fatalf("link %s", uri)
	}
	ui, _ := base64.RawURLEncoding.DecodeString(u.User.Username())
	if string(ui) != "ali_px:Secret-123" {
		t.Fatalf("link login %s", ui)
	}
	code, m, _ = c.do("POST", api+"/users", map[string]any{"username": "bob", "proxy_username": "ali_px"})
	if code != 409 || m["error"].(map[string]any)["field"] != "proxy_username" {
		t.Fatalf("duplicate %d %v", code, m)
	}
	code, m, _ = c.do("POST", api+"/users", map[string]any{"username": "bob", "proxy_username": "bad name"})
	if code != 400 || !strings.Contains(m["error"].(map[string]any)["message"].(string), "proxy_username") {
		t.Fatalf("bad name %d %v", code, m)
	}
	code, m, _ = c.do("PATCH", api+"/users/"+strconv.Itoa(id), map[string]any{"proxy_username": ""})
	if code != 200 || m["proxy_username"] != "" || m["proxy_password"] != "Secret-123" {
		t.Fatalf("clear name %d %v", code, m)
	}
}
