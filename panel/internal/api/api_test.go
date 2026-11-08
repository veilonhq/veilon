package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/sub"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

type fakeCore struct{ restarts int }

func (f *fakeCore) Status() xray.Status                   { return xray.Status{Name: "xray"} }
func (f *fakeCore) Restart()                              { f.restarts++ }
func (f *fakeCore) Statuses() []xray.Status               { return []xray.Status{{Name: "xray"}} }
func (f *fakeCore) Logs(string, int) ([]string, error)    { return []string{"l1"}, nil }
func (f *fakeCore) GenerateConfig(string) ([]byte, error) { return []byte(`{"ok":true}`), nil }

type client struct {
	t   *testing.T
	ts  *httptest.Server
	tok string
}

func (c *client) do(method, path string, body any) (int, map[string]any, http.Header) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.ts.URL+path, r)
	if c.tok != "" {
		req.Header.Set("Authorization", "Bearer "+c.tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		m = map[string]any{"_raw": string(raw)}
	}
	return resp.StatusCode, m, resp.Header
}

func setup(t *testing.T) (*client, *Server) {
	gdb := testutil.DB(t)
	_ = db.SaveSettings(gdb, db.Settings{PanelPort: 9000, PanelPath: "secret", SubPath: "sub", SubUpdateHours: 12, Language: "en", Timezone: "UTC"})
	if _, err := auth.CreateAdmin(gdb, "root", "password123", true); err != nil {
		t.Fatal(err)
	}
	var srv *Server
	srv = New(&Server{
		DB:       gdb,
		Auth:     auth.NewServiceWithSecret(gdb, []byte("k"), time.Hour),
		Users:    &users.Service{DB: gdb, SubURL: func(tok, host string) string { return sub.SubURL(srv.Settings(), tok, host) }},
		Inbounds: &inbounds.Service{DB: gdb, ReservedPorts: func() []int { return []int{9000} }},
		Certs:    &certs.Service{DB: gdb, DataDir: t.TempDir()},
		Sub:      &sub.Service{DB: gdb},
		Core:     &fakeCore{},
	})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return &client{t: t, ts: ts}, srv
}

func TestAPIFlow(t *testing.T) {
	c, _ := setup(t)
	const api = "/secret/api/v1"
	if code, _, _ := c.do("GET", "/", nil); code != 404 {
		t.Fatalf("root should 404, got %d", code)
	}
	if code, _, _ := c.do("GET", api+"/users", nil); code != 401 {
		t.Fatalf("unauth users: %d", code)
	}
	code, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	if code != 200 || m["token"] == nil || m["admin"].(map[string]any)["is_sudo"] != true {
		t.Fatalf("login %d %v", code, m)
	}
	c.tok = m["token"].(string)

	code, m, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "r", "protocol": "vless", "port": 9000})
	if code != 409 || m["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("reserved port accepted: %d %v", code, m)
	}
	code, m, _ = c.do("POST", api+"/inbounds", map[string]any{"remark": "VL", "protocol": "vless", "port": 4443, "security": "reality",
		"flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}})
	if code != 201 {
		t.Fatalf("create inbound %d %v", code, m)
	}
	ibID := m["id"].(float64)
	code, m, _ = c.do("PATCH", api+"/inbounds/1", map[string]any{"remark": "VL2", "reality": map[string]any{"short_ids": []string{"aa"}}})
	if code != 200 || m["remark"] != "VL2" || m["reality"].(map[string]any)["dest"] != "www.example.com:443" {
		t.Fatalf("patch inbound %d %v", code, m)
	}

	code, m, _ = c.do("POST", api+"/users", map[string]any{"username": "alice", "data_limit": 0, "expire_at": nil,
		"reset_strategy": "no_reset", "note": "", "inbound_ids": []float64{ibID}})
	if code != 201 || m["status"] != "active" {
		t.Fatalf("create user %d %v", code, m)
	}
	token := m["sub_token"].(string)
	if !strings.HasSuffix(m["sub_url"].(string), "/sub/"+token) {
		t.Fatalf("sub_url %v", m["sub_url"])
	}
	code, m, _ = c.do("GET", api+"/users?search=ali&status=active", nil)
	if code != 200 || m["total"].(float64) != 1 {
		t.Fatalf("list %d %v", code, m)
	}
	code, m, _ = c.do("GET", api+"/users/1/links", nil)
	links := m["links"].([]any)
	if code != 200 || len(links) != 1 || !strings.HasPrefix(links[0].(map[string]any)["uri"].(string), "vless://") {
		t.Fatalf("links %d %v", code, m)
	}
	code, m, _ = c.do("POST", api+"/users/bulk", map[string]any{"action": "disable", "ids": []int{1}})
	if code != 200 {
		t.Fatalf("bulk %d %v", code, m)
	}
	_, m, _ = c.do("GET", api+"/users/1", nil)
	if m["status"] != "disabled" {
		t.Fatalf("bulk disable failed: %v", m["status"])
	}

	// Public subscription.
	sc := &client{t: t, ts: c.ts}
	code, m, h := sc.do("GET", "/sub/"+token, nil)
	if code != 200 || h.Get("Subscription-Userinfo") == "" || !strings.HasPrefix(h.Get("Profile-Title"), "base64:") {
		t.Fatalf("sub %d %v", code, h)
	}
	raw, _ := base64.StdEncoding.DecodeString(m["_raw"].(string))
	if !strings.HasPrefix(string(raw), "vless://") {
		t.Fatalf("sub body %q", raw)
	}
	if code, _, _ := sc.do("GET", "/sub/nope", nil); code != 404 {
		t.Fatal("unknown token should 404")
	}
	if code, m, _ = sc.do("GET", "/sub/"+token+"/info", nil); code != 200 || m["username"] != "alice" {
		t.Fatalf("info %d %v", code, m)
	}

	// Settings.
	code, m, _ = c.do("PATCH", api+"/settings", map[string]any{"sub_path": "secret"})
	if code != 400 {
		t.Fatalf("sub_path == panel_path accepted: %d", code)
	}
	code, m, _ = c.do("PATCH", api+"/settings", map[string]any{"sub_title": "My VPN", "language": "fa"})
	if code != 200 || m["sub_title"] != "My VPN" || m["panel_path"] != "secret" {
		t.Fatalf("settings %d %v", code, m)
	}

	// Admins & audit.
	code, _, _ = c.do("POST", api+"/admins", map[string]any{"username": "helper", "password": "password123", "is_sudo": false})
	if code != 201 {
		t.Fatalf("create admin %d", code)
	}
	hc := &client{t: t, ts: c.ts}
	_, m, _ = hc.do("POST", api+"/auth/login", map[string]string{"username": "helper", "password": "password123"})
	hc.tok = m["token"].(string)
	if code, _, _ := hc.do("GET", api+"/admins", nil); code != 403 {
		t.Fatalf("non-sudo admins list: %d", code)
	}
	code, m, _ = c.do("GET", api+"/audit", nil)
	if code != 200 || m["total"].(float64) < 5 {
		t.Fatalf("audit %d %v", code, m)
	}
	code, m, _ = c.do("POST", api+"/auth/password", map[string]string{"current_password": "password123", "new_password": "newpassword9"})
	if code != 200 || m["token"] == nil {
		t.Fatalf("password change %d %v", code, m)
	}
	c.tok = m["token"].(string) // tokens issued before the change are revoked
	if code, _, _ := c.do("DELETE", api+"/users/1", nil); code != 200 {
		t.Fatalf("delete user %d", code)
	}
	if code, _, _ := c.do("GET", api+"/nope", nil); code != 404 {
		t.Fatalf("unknown endpoint %d", code)
	}
}

func TestLoginRateLimitHTTP(t *testing.T) {
	c, _ := setup(t)
	for i := 0; i < 5; i++ {
		if code, _, _ := c.do("POST", "/secret/api/v1/auth/login", map[string]string{"username": "root", "password": "x"}); code != 401 {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	code, m, _ := c.do("POST", "/secret/api/v1/auth/login", map[string]string{"username": "root", "password": "password123"})
	if code != 429 || m["error"].(map[string]any)["code"] != "rate_limited" {
		t.Fatalf("expected 429, got %d %v", code, m)
	}
}

func TestUpdateMeUsername(t *testing.T) {
	c, srv := setup(t)
	api := "/secret/api/v1"
	if _, err := auth.CreateAdmin(srv.DB, "other", "password123", false); err != nil {
		t.Fatal(err)
	}
	_, lm, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = lm["token"].(string)
	cases := []struct {
		body map[string]string
		code int
		err  string
	}{
		{map[string]string{"username": "newroot", "current_password": "wrong"}, 400, "invalid_credentials"},
		{map[string]string{"username": "a!", "current_password": "password123"}, 400, "validation_error"},
		{map[string]string{"username": "other", "current_password": "password123"}, 409, "conflict"},
	}
	for _, tc := range cases {
		code, m, _ := c.do("PATCH", api+"/auth/me", tc.body)
		if code != tc.code || m["error"].(map[string]any)["code"] != tc.err {
			t.Fatalf("%v: %d %v", tc.body, code, m)
		}
	}
	code, m, _ := c.do("PATCH", api+"/auth/me", map[string]string{"username": " newroot ", "current_password": "password123"})
	if code != 200 || m["token"] == nil || m["admin"].(map[string]any)["username"] != "newroot" {
		t.Fatalf("rename %d %v", code, m)
	}
	c.tok = m["token"].(string)
	if code, m, _ := c.do("GET", api+"/auth/me", nil); code != 200 || m["username"] != "newroot" {
		t.Fatalf("me after rename %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "newroot", "password": "password123"}); code != 200 {
		t.Fatalf("login with new name %d", code)
	}
	if code, _, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"}); code != 401 {
		t.Fatalf("login with old name %d", code)
	}
	var n int64
	srv.DB.Model(&db.AuditLog{}).Where("action = ? AND target = ?", "auth.username_changed", "root -> newroot").Count(&n)
	if n != 1 {
		t.Fatalf("audit entries: %d", n)
	}
}

func TestTimezoneTehran(t *testing.T) {
	c, _ := setup(t)
	api := "/secret/api/v1"
	_, lm, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = lm["token"].(string)
	st, body, _ := c.do("PATCH", api+"/settings", map[string]any{"timezone": "Asia/Tehran"})
	if st != 200 || body["timezone"] != "Asia/Tehran" {
		t.Fatalf("%d %v", st, body)
	}
	if st, _, _ := c.do("PATCH", api+"/settings", map[string]any{"timezone": "Mars/Base"}); st != 400 {
		t.Fatalf("bad zone accepted: %d", st)
	}
}
