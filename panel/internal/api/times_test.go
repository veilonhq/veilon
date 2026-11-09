package api

import (
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/logbuf"
)

func TestUTCTimes(t *testing.T) {
	in := `{"a":"2026-11-06T20:14:00+03:30","b":"2026-11-06T16:44:00.594+00:00","c":"2026-11-06T16:44:00Z","d":"note 2026-11-06T20:14:00+03:30 x"}`
	got := string(utcTimes([]byte(in)))
	want := `{"a":"2026-11-06T16:44:00Z","b":"2026-11-06T16:44:00.594Z","c":"2026-11-06T16:44:00Z","d":"note 2026-11-06T20:14:00+03:30 x"}`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestUserTimesAreUTC(t *testing.T) {
	c, _ := setup(t)
	api := "/secret/api/v1"
	_, lm, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = lm["token"].(string)
	code, u, _ := c.do("POST", api+"/users", map[string]any{"username": "tz1", "expire_at": "2026-12-01T00:00:00+03:30"})
	if code != 201 {
		t.Fatalf("%d %v", code, u)
	}
	for _, k := range []string{"expire_at", "created_at", "updated_at"} {
		v, _ := u[k].(string)
		ts, err := time.Parse(time.RFC3339Nano, v)
		if err != nil || !strings.HasSuffix(v, "Z") {
			t.Fatalf("%s = %q is not RFC 3339 UTC", k, v)
		}
		if k == "expire_at" && !ts.Equal(time.Date(2026, 11, 30, 20, 30, 0, 0, time.UTC)) {
			t.Fatalf("expire_at moved: %s", v)
		}
	}
}

func TestCoreLogsEntries(t *testing.T) {
	c, srv := setup(t)
	srv.PanelLogs = logbuf.New("panel", 10)
	srv.PanelLogs.Add("panel started")
	api := "/secret/api/v1"
	_, lm, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = lm["token"].(string)
	for _, src := range []string{"", "panel", "all"} {
		code, m, _ := c.do("GET", api+"/core/logs?lines=5000&core="+src, nil)
		es, _ := m["entries"].([]any)
		if code != 200 || len(es) == 0 || len(m["lines"].([]any)) != len(es) {
			t.Fatalf("%q: %d %v", src, code, m)
		}
		if src == "panel" {
			e := es[0].(map[string]any)
			if e["source"] != "panel" || e["level"] != "info" || !strings.HasSuffix(e["t"].(string), "Z") {
				t.Fatalf("entry %v", e)
			}
		}
	}
}
