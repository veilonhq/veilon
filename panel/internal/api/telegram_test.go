package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

const testBotToken = "123456789:AAH-fake_token_value_for_tests_0123456789"

// fakeTelegram answers getMe and sendMessage like the Bot API.
func fakeTelegram(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	sent := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bot"+testBotToken+"/") {
			_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			return
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		switch method {
		case "getMe":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"Veilon","username":"veilon_test_bot"}}`)
		case "sendMessage":
			if p["chat_id"].(float64) == 999 {
				_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
				return
			}
			mu.Lock()
			sent = append(sent, p["text"].(string))
			mu.Unlock()
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`)
		default:
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, &sent
}

func loginAs(t *testing.T, c *client, user, pass string) {
	t.Helper()
	code, m, _ := c.do("POST", "/secret/api/v1/auth/login", map[string]string{"username": user, "password": pass})
	if code != 200 {
		t.Fatalf("login %s: %d %v", user, code, m)
	}
	c.tok = m["token"].(string)
}

func TestTelegramSettings(t *testing.T) {
	c, srv := setup(t)
	fake, sent := fakeTelegram(t)
	srv.Telegram = &tgbot.Bot{DB: srv.DB, API: srv, Base: fake.URL}
	const api = "/secret/api/v1"
	loginAs(t, c, "root", "password123")

	code, m, _ := c.do("GET", api+"/telegram", nil)
	if code != 200 || m["token_set"] != false || m["enabled"] != false {
		t.Fatalf("get default %d %v", code, m)
	}
	code, m, _ = c.do("PUT", api+"/telegram", map[string]any{"token": "nope", "enabled": true, "admins": []int64{42}})
	if code != 400 {
		t.Fatalf("bad token accepted: %d %v", code, m)
	}
	code, m, _ = c.do("PUT", api+"/telegram", map[string]any{"enabled": true, "admins": []int64{}})
	if code != 400 {
		t.Fatalf("enabled without token accepted: %d %v", code, m)
	}
	code, m, _ = c.do("PUT", api+"/telegram", map[string]any{"token": testBotToken, "enabled": true, "admins": []int64{42, 42, 43}})
	if code != 200 || m["token_set"] != true || m["enabled"] != true || len(m["admins"].([]any)) != 2 {
		t.Fatalf("save %d %v", code, m)
	}
	if hint := m["token_hint"].(string); !strings.HasPrefix(hint, "123456789:") || !strings.HasSuffix(hint, "6789") || strings.Contains(hint, "fake_token") {
		t.Fatalf("hint %q", hint)
	}
	// Saving without a token keeps the stored one.
	code, m, _ = c.do("PUT", api+"/telegram", map[string]any{"proxy": "socks5://127.0.0.1:1080"})
	if code != 200 || m["token_set"] != true || tgbot.LoadConfig(srv.DB).Token != testBotToken {
		t.Fatalf("token lost on save: %d %v", code, m)
	}
	code, m, _ = c.do("PUT", api+"/telegram", map[string]any{"proxy": ""})
	if code != 200 {
		t.Fatalf("clear proxy %d %v", code, m)
	}

	// The full token never comes back from any endpoint or lands in the audit log.
	for _, p := range []string{"/telegram", "/settings", "/audit"} {
		req, _ := http.NewRequest("GET", c.ts.URL+api+p, nil)
		req.Header.Set("Authorization", "Bearer "+c.tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(raw), "fake_token") {
			t.Fatalf("%s leaks the token: %s", p, raw)
		}
	}
	var rows []db.AuditLog
	srv.DB.Where("action = ?", "telegram.update").Find(&rows)
	if len(rows) == 0 {
		t.Fatal("no audit entry")
	}
	for _, r := range rows {
		if strings.Contains(r.Target, "fake_token") {
			t.Fatalf("audit leaks the token: %q", r.Target)
		}
	}

	// Test: stored token, one admin reachable and one not.
	code, m, _ = c.do("POST", api+"/telegram/test", map[string]any{"admins": []int64{42, 999}})
	if code != 200 || m["bot"].(map[string]any)["username"] != "veilon_test_bot" {
		t.Fatalf("test %d %v", code, m)
	}
	res := m["admins"].([]any)
	if res[0].(map[string]any)["ok"] != true || res[1].(map[string]any)["ok"] != false {
		t.Fatalf("test results %v", res)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %d test messages", len(*sent))
	}
	// A wrong token is reported without echoing it.
	wrong := "123456789:BBH-another_wrong_token_value_000000000"
	code, m, _ = c.do("POST", api+"/telegram/test", map[string]any{"token": wrong})
	if code != 502 || strings.Contains(m["error"].(map[string]any)["message"].(string), "another_wrong") {
		t.Fatalf("wrong token %d %v", code, m)
	}

	// Sub-admins cannot see or change the bot.
	if _, err := auth.CreateAdmin(srv.DB, "helper", "password123", false); err != nil {
		t.Fatal(err)
	}
	sc := &client{t: t, ts: c.ts}
	loginAs(t, sc, "helper", "password123")
	for _, mt := range []string{"GET", "PUT"} {
		if code, _, _ := sc.do(mt, api+"/telegram", map[string]any{}); code != 403 {
			t.Fatalf("sub-admin %s /telegram: %d", mt, code)
		}
	}
	if code, _, _ := sc.do("POST", api+"/telegram/test", map[string]any{}); code != 403 {
		t.Fatalf("sub-admin test: %d", code)
	}
}

func TestServerDoInProcess(t *testing.T) {
	_, srv := setup(t)
	var root db.Admin
	srv.DB.Where("username = ?", "root").First(&root)
	actor := root
	actor.Username = "telegram:42"
	st, raw := srv.Do(context.Background(), &actor, "POST", "/users", map[string]any{"username": "bob", "data_limit": 0, "expire_at": nil,
		"reset_strategy": "no_reset", "inbound_ids": []int64{}})
	if st != 201 {
		t.Fatalf("create %d %s", st, raw)
	}
	var e db.AuditLog
	srv.DB.Where("action = ?", "user.create").First(&e)
	if e.AdminName != "telegram:42" || e.AdminID != root.ID || e.IP != "telegram" {
		t.Fatalf("audit %+v", e)
	}
	// The admin row itself is unchanged.
	var again db.Admin
	srv.DB.First(&again, root.ID)
	if again.Username != "root" {
		t.Fatalf("admin renamed to %q", again.Username)
	}
	if st, _ := srv.Do(context.Background(), &actor, "GET", "/users?search=bob", nil); st != 200 {
		t.Fatalf("list %d", st)
	}
	if st, _ := srv.Do(context.Background(), &actor, "PATCH", "/auth/me", map[string]any{"username": "x"}); st != 403 {
		t.Fatalf("auth path in process: %d", st)
	}
	// Without an actor the request is unauthenticated as usual.
	if st, _ := srv.Do(context.Background(), nil, "GET", "/users", nil); st != 401 {
		t.Fatalf("no actor: %d", st)
	}
}
