package tgbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// stubAPI answers /nodes and /system/update with whatever the test sets.
type stubAPI struct {
	mu     sync.Mutex
	status string
	tag    string
}

func (s *stubAPI) Do(_ context.Context, _ *db.Admin, method, path string, _ any) (int, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch path {
	case "/nodes":
		b, _ := json.Marshal([]map[string]any{{"id": 7, "name": "Frankfurt", "enabled": true, "state": map[string]any{"status": s.status, "error": "dial tcp: timeout"}}})
		return 200, b
	case "/system/update":
		var av any
		if s.tag != "" {
			av = map[string]any{"tag": s.tag}
		}
		b, _ := json.Marshal(map[string]any{"current": "v0.3.8", "available": av})
		return 200, b
	}
	return 404, []byte(`{}`)
}

func (s *stubAPI) set(status, tag string) {
	s.mu.Lock()
	s.status, s.tag = status, tag
	s.mu.Unlock()
}

func TestNotifyNodesAndRelease(t *testing.T) {
	gdb := testutil.DB(t)
	if _, err := auth.CreateAdmin(gdb, "root", "password123", true); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		sent = append(sent, p["text"].(string))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer srv.Close()
	api := &stubAPI{status: "online"}
	b := &Bot{DB: gdb, API: api}
	b.init()
	cl, _ := NewClient("1:x", srv.URL, "")
	cfg := Config{Enabled: true, Admins: []int64{42}, Notify: Notify{Nodes: true, Update: true}}
	round := func() []string {
		mu.Lock()
		n := len(sent)
		mu.Unlock()
		b.notifyRound(context.Background(), cl, cfg)
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, sent[n:]...)
	}
	if got := round(); len(got) != 0 {
		t.Fatalf("online node: %v", got)
	}
	api.set("offline", "")
	if got := round(); len(got) != 0 {
		t.Fatalf("one missed check reported: %v", got)
	}
	got := round()
	if len(got) != 1 || !strings.Contains(got[0], "Frankfurt</b> آفلاین شد") {
		t.Fatalf("offline: %v", got)
	}
	if got := round(); len(got) != 0 {
		t.Fatalf("offline twice: %v", got)
	}
	// back online, and a new release in the same round: one message
	api.set("online", "v0.4.0")
	got = round()
	if len(got) != 1 || !strings.Contains(got[0], "دوباره وصل شد") || !strings.Contains(got[0], "v0.4.0") {
		t.Fatalf("back + release: %v", got)
	}
	if got := round(); len(got) != 0 {
		t.Fatalf("repeat: %v", got)
	}
	// down again later: reported again
	api.set("offline", "v0.4.0")
	round()
	if got := round(); len(got) != 1 {
		t.Fatalf("second outage: %v", got)
	}
}
