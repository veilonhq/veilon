package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/selfupdate"
)

func releaseFeed(t *testing.T, mode *atomic.Value, hits *atomic.Int32) string {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch mode.Load().(string) {
		case "ratelimit":
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "rate limit", http.StatusForbidden)
		case "down":
			http.Error(w, "down", http.StatusServiceUnavailable)
		case "newer":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v9.0.0", "name": "Veilon v9.0.0", "body": "## English\n- new", "html_url": "https://example.invalid/v9.0.0"}})
		default:
			_, _ = w.Write([]byte("[]"))
		}
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestUpdateCheckEndpoint(t *testing.T) {
	c, srv := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)
	if code, _, _ := c.do("POST", api+"/system/update/check", nil); code != 501 {
		t.Fatalf("without updates want 501, got %d", code)
	}

	var mode atomic.Value
	mode.Store("newer")
	var hits atomic.Int32
	url := releaseFeed(t, &mode, &hits)
	srv.Update = &selfupdate.Service{Checker: &selfupdate.Checker{Current: "v0.3.2", URL: url}, DataDir: t.TempDir(), Current: "v0.3.2"}

	code, m, _ := c.do("POST", api+"/system/update/check", nil)
	if code != 200 || m["cached"] != false || m["checked_at"] == nil || m["current"] == nil {
		t.Fatalf("check %d %v", code, m)
	}
	if av, _ := m["available"].(map[string]any); av == nil || av["tag"] != "v9.0.0" || av["notes"] == "" {
		t.Fatalf("available %v", m["available"])
	}
	for _, k := range []string{"can_update", "status", "next_check_at"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %s in %v", k, m)
		}
	}
	// Second call inside the gap: cached, no new request.
	code, m, _ = c.do("POST", api+"/system/update/check", nil)
	if code != 200 || m["cached"] != true || hits.Load() != 1 {
		t.Fatalf("cached check %d %v hits=%d", code, m, hits.Load())
	}
	// GET sees the same answer.
	if code, m, _ = c.do("GET", api+"/system/update", nil); code != 200 || m["available"].(map[string]any)["tag"] != "v9.0.0" {
		t.Fatalf("get %d %v", code, m)
	}
	var n int64
	srv.DB.Model(&db.AuditLog{}).Where("action = ?", "panel.update_check").Count(&n)
	if n != 1 {
		t.Fatalf("want 1 audit entry, got %d", n)
	}

	// GitHub rate limit and an unreachable source give clear errors.
	srv.Update.Checker = &selfupdate.Checker{Current: "v0.3.2", URL: url}
	mode.Store("ratelimit")
	code, m, _ = c.do("POST", api+"/system/update/check", nil)
	if e, _ := m["error"].(map[string]any); code != 503 || e["code"] != "rate_limited" {
		t.Fatalf("rate limited %d %v", code, m)
	}
	if code, m, _ = c.do("POST", api+"/system/update/check", nil); code != 503 || m["retry_after"].(float64) <= 0 {
		t.Fatalf("cached failure %d %v", code, m)
	}
	srv.Update.Checker = &selfupdate.Checker{Current: "v0.3.2", URL: "http://127.0.0.1:1/releases"}
	code, m, _ = c.do("POST", api+"/system/update/check", nil)
	if e, _ := m["error"].(map[string]any); code != 502 || e["code"] != "update_check_failed" {
		t.Fatalf("unreachable %d %v", code, m)
	}

	// Up to date.
	srv.Update.Checker = &selfupdate.Checker{Current: "v0.3.2", URL: url}
	mode.Store("latest")
	if code, m, _ = c.do("POST", api+"/system/update/check", nil); code != 200 || m["available"] != nil {
		t.Fatalf("latest %d %v", code, m)
	}

	// Non-sudo admins are refused.
	if _, err := auth.CreateAdmin(srv.DB, "helper", "password123", false); err != nil {
		t.Fatal(err)
	}
	_, m, _ = c.do("POST", api+"/auth/login", map[string]string{"username": "helper", "password": "password123"})
	c.tok = m["token"].(string)
	if code, _, _ := c.do("POST", api+"/system/update/check", nil); code != 403 {
		t.Fatalf("non-sudo want 403, got %d", code)
	}
}
