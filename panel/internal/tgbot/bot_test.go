package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/nodes"
	"github.com/veilonhq/veilon/panel/internal/sub"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

const (
	token      = "123456789:AAH-fake_token_value_for_tests_0123456789"
	adminID    = int64(42)
	strangerID = int64(77)
)

type fakeCore struct{}

func (fakeCore) Status() xray.Status                   { return xray.Status{Name: "xray"} }
func (fakeCore) Restart()                              {}
func (fakeCore) Statuses() []xray.Status               { return []xray.Status{{Name: "xray"}} }
func (fakeCore) Logs(string, int) ([]string, error)    { return nil, nil }
func (fakeCore) GenerateConfig(string) ([]byte, error) { return []byte(`{}`), nil }

// call is one request the bot made to the fake Bot API.
type call struct {
	method string
	p      map[string]any
}

// fakeTG is a minimal Bot API: getUpdates hands out queued updates, every
// other method is recorded and answered like Telegram does.
type fakeTG struct {
	t       *testing.T
	srv     *httptest.Server
	updates chan map[string]any
	mu      sync.Mutex
	calls   []call
	nextMsg int
	nextUp  int64
	// files are the files users "sent" to the bot (file_id -> content).
	files map[string][]byte
}

func newFakeTG(t *testing.T) *fakeTG {
	f := &fakeTG{t: t, updates: make(chan map[string]any, 16), nextMsg: 100, files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTG) reply(w http.ResponseWriter, result any) {
	b, _ := json.Marshal(map[string]any{"ok": true, "result": result})
	_, _ = w.Write(b)
}

func (f *fakeTG) serve(w http.ResponseWriter, r *http.Request) {
	if fp, ok := strings.CutPrefix(r.URL.Path, "/file/bot"+token+"/"); ok {
		f.mu.Lock()
		b, found := f.files[fp]
		f.mu.Unlock()
		if !found {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/bot"+token+"/") {
		_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
		return
	}
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	p := map[string]any{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&p)
	} else if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(64 << 20); err == nil {
			for k, v := range r.MultipartForm.Value {
				p[k] = v[0]
			}
			for k, fhs := range r.MultipartForm.File {
				fh := fhs[0]
				fr, _ := fh.Open()
				b, _ := io.ReadAll(fr)
				fr.Close()
				p["_field"], p["_name"], p["_data"] = k, fh.Filename, string(b)
			}
		}
	}
	switch method {
	case "getMe":
		f.reply(w, map[string]any{"id": 123456789, "is_bot": true, "first_name": "Veilon", "username": "veilon_test_bot"})
		return
	case "getFile":
		id := fmt.Sprint(p["file_id"])
		f.mu.Lock()
		b := f.files[id]
		f.mu.Unlock()
		f.reply(w, map[string]any{"file_id": id, "file_size": len(b), "file_path": id})
		return
	case "getUpdates":
		select {
		case u := <-f.updates:
			f.reply(w, []any{u})
		case <-time.After(300 * time.Millisecond):
			f.reply(w, []any{})
		case <-r.Context().Done():
		}
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{method, p})
	f.nextMsg++
	id := f.nextMsg
	f.mu.Unlock()
	switch method {
	case "sendMessage":
		f.reply(w, map[string]any{"message_id": id, "chat": map[string]any{"id": p["chat_id"], "type": "private"}, "text": p["text"]})
	default:
		f.reply(w, true)
	}
}

func (f *fakeTG) mark() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeTG) since(n int) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call{}, f.calls[n:]...)
}

func (f *fakeTG) push(u map[string]any) {
	f.mu.Lock()
	f.nextUp++
	u["update_id"] = f.nextUp
	f.mu.Unlock()
	f.updates <- u
}

func (f *fakeTG) text(from int64, msgID int, text string) {
	f.push(map[string]any{"message": map[string]any{"message_id": msgID, "date": time.Now().Unix(), "text": text,
		"from": map[string]any{"id": from, "is_bot": false, "first_name": "A"}, "chat": map[string]any{"id": from, "type": "private"}}})
}

func (f *fakeTG) press(from int64, menuMsg int, data string) {
	f.push(map[string]any{"callback_query": map[string]any{"id": fmt.Sprint("cb", time.Now().UnixNano()), "data": data,
		"from":    map[string]any{"id": from, "is_bot": false, "first_name": "A"},
		"message": map[string]any{"message_id": menuMsg, "chat": map[string]any{"id": from, "type": "private"}}}})
}

// screen waits for the next drawn screen (a new or edited message) after
// mark n whose text contains want.
func (f *fakeTG) screen(n int, want string) call {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range f.since(n) {
			if (c.method == "sendMessage" || c.method == "editMessageText") && strings.Contains(fmt.Sprint(c.p["text"]), want) {
				return c
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("no screen with %q; calls: %s", want, f.dump(n))
	return call{}
}

func (f *fakeTG) dump(n int) string {
	var b strings.Builder
	for _, c := range f.since(n) {
		fmt.Fprintf(&b, "\n  %s %v", c.method, c.p["text"])
	}
	return b.String()
}

// button returns the callback data of the button whose label contains label.
func button(t *testing.T, c call, label string) string {
	t.Helper()
	m, _ := c.p["reply_markup"].(map[string]any)
	rows, _ := m["inline_keyboard"].([]any)
	for _, row := range rows {
		for _, b := range row.([]any) {
			bm := b.(map[string]any)
			if strings.Contains(fmt.Sprint(bm["text"]), label) {
				return fmt.Sprint(bm["callback_data"])
			}
		}
	}
	t.Fatalf("no button %q in %v", label, c.p["reply_markup"])
	return ""
}

type rig struct {
	t   *testing.T
	gdb *gorm.DB
	tg  *fakeTG
	srv *api.Server
	ib  int64
	// menu is the message the bot draws its menus in.
	menu int
}

func newRig(t *testing.T) *rig { return newRigWith(t, nil) }

// newRigWith builds the rig; setup can add services to the API server and
// the bot before the bot starts.
func newRigWith(t *testing.T, setup func(srv *api.Server, b *tgbot.Bot)) *rig {
	t.Helper()
	gdb := testutil.DB(t)
	_ = db.SaveSettings(gdb, db.Settings{PanelPort: 9000, PanelPath: "secret", SubPath: "sub", SubUpdateHours: 12, Language: "fa", Timezone: "UTC", ServerAddress: "1.2.3.4"})
	if _, err := auth.CreateAdmin(gdb, "root", "password123", true); err != nil {
		t.Fatal(err)
	}
	var srv *api.Server
	srv = api.New(&api.Server{
		DB:       gdb,
		Auth:     auth.NewServiceWithSecret(gdb, []byte("k"), time.Hour),
		Users:    &users.Service{DB: gdb, SubURL: func(tok, host string) string { return sub.SubURL(srv.Settings(), tok, host) }},
		Inbounds: &inbounds.Service{DB: gdb, ReservedPorts: func() []int { return []int{9000} }},
		Certs:    &certs.Service{DB: gdb, DataDir: t.TempDir()},
		Sub:      &sub.Service{DB: gdb},
		Core:     fakeCore{},
	})
	nm := &nodes.Manager{DB: gdb, DataDir: t.TempDir(), PKI: &nodes.PKI{Dir: t.TempDir()}, Version: "v0.3.8", RequestTimeout: time.Second}
	srv.Nodes = &nodes.Service{DB: gdb, PKI: nm.PKI, Manager: nm, CheckMoveToMain: srv.Inbounds.CheckMoveToMain}
	var root db.Admin
	gdb.Where("username = ?", "root").First(&root)
	st, raw := srv.Do(context.Background(), &root, "POST", "/inbounds", map[string]any{"remark": "VL", "protocol": "vless", "port": 4443,
		"security": "reality", "flow": "xtls-rprx-vision", "reality": map[string]any{"dest": "www.example.com"}})
	if st != 201 {
		t.Fatalf("inbound %d %s", st, raw)
	}
	var ib struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(raw, &ib)

	tg := newFakeTG(t)
	t.Setenv("VEILON_TELEGRAM_API", tg.srv.URL)
	if err := tgbot.SaveConfig(gdb, tgbot.Config{Enabled: true, Admins: []int64{adminID}, Token: token, Notify: tgbot.Notify{}}); err != nil {
		t.Fatal(err)
	}
	b := &tgbot.Bot{DB: gdb, API: srv, PollTimeout: 1, WatchEvery: 100 * time.Millisecond}
	if setup != nil {
		setup(srv, b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for !b.Status().Running {
		if time.Now().After(deadline) {
			t.Fatalf("bot not running: %+v", b.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return &rig{t: t, gdb: gdb, tg: tg, srv: srv, ib: ib.ID}
}

// start opens the main menu and returns it.
func (r *rig) start() call {
	r.t.Helper()
	n := r.tg.mark()
	r.tg.text(adminID, 1, "/start")
	c := r.tg.screen(n, "مدیریت پنل")
	if c.method != "sendMessage" || c.p["chat_id"].(float64) != float64(adminID) {
		r.t.Fatalf("menu %s to %v", c.method, c.p["chat_id"])
	}
	r.menu = r.tg.nextMsgOf(n)
	return c
}

// nextMsgOf returns the message id the first sendMessage after n got.
func (f *fakeTG) nextMsgOf(n int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := 100
	for i := 0; i < len(f.calls); i++ {
		id++
		if i >= n && f.calls[i].method == "sendMessage" {
			return id
		}
	}
	return 0
}

func (r *rig) press(data, want string) call {
	r.t.Helper()
	n := r.tg.mark()
	r.tg.press(adminID, r.menu, data)
	return r.tg.screen(n, want)
}

func (r *rig) typeText(text, want string) call {
	r.t.Helper()
	n := r.tg.mark()
	r.tg.text(adminID, 500+n, text)
	return r.tg.screen(n, want)
}

func (r *rig) user(name string) db.User {
	var u db.User
	r.gdb.Where("username = ?", name).Limit(1).Find(&u)
	return u
}

func TestNonAdminIgnored(t *testing.T) {
	r := newRig(t)
	n := r.tg.mark()
	r.tg.text(strangerID, 1, "/start")
	r.tg.press(strangerID, 1, "ul:1")
	time.Sleep(time.Second)
	if calls := r.tg.since(n); len(calls) != 0 {
		t.Fatalf("bot answered a stranger:%s", r.tg.dump(n))
	}
	// A group chat with an admin in it is ignored too.
	r.tg.push(map[string]any{"message": map[string]any{"message_id": 2, "text": "/start",
		"from": map[string]any{"id": adminID}, "chat": map[string]any{"id": -100, "type": "group"}}})
	time.Sleep(500 * time.Millisecond)
	if calls := r.tg.since(n); len(calls) != 0 {
		t.Fatalf("bot answered in a group:%s", r.tg.dump(n))
	}
	// The admin is still served afterwards.
	r.start()
}

func TestAdminMainMenu(t *testing.T) {
	r := newRig(t)
	c := r.start()
	for _, label := range []string{"کاربران", "ورودی ها", "👤 ادمین ها"} {
		button(t, c, label)
	}
	if strings.ContainsAny(fmt.Sprint(c.p), "\u200c\u200d") {
		t.Fatal("menu contains a zero-width joiner")
	}
	// The users button opens the users list in place.
	c = r.press(button(t, c, "کاربران"), "<b>کاربران</b>")
	if c.method != "editMessageText" {
		t.Fatalf("list drawn with %s", c.method)
	}
	button(t, c, "کاربر جدید")
}

func TestCreateUserFlow(t *testing.T) {
	r := newRig(t)
	r.start()
	list := r.press("ul:1", "کاربر")
	r.press(button(t, list, "کاربر جدید"), "نام کاربری")
	r.typeText("alice", "حجم به گیگابایت")
	c := r.typeText("۱۰", "چند روز")
	c = r.press(button(t, c, "۳۰ روز"), "ورودی های کاربر")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "alice") {
		t.Fatalf("summary %v", c.p["text"])
	}
	button(t, c, "VL")
	before := time.Now().UTC()
	r.press(button(t, c, "ساختن کاربر"), "alice")
	u := r.user("alice")
	if u.ID == 0 {
		t.Fatal("user not created")
	}
	if u.DataLimit != 10<<30 {
		t.Fatalf("data limit %d", u.DataLimit)
	}
	if u.ExpireAt == nil || u.ExpireAt.Sub(before) < 29*24*time.Hour || u.ExpireAt.Sub(before) > 31*24*time.Hour {
		t.Fatalf("expire %v", u.ExpireAt)
	}
	var links int64
	r.gdb.Table("user_inbounds").Where("user_id = ? AND inbound_id = ?", u.ID, r.ib).Count(&links)
	if links != 1 {
		t.Fatal("inbound not assigned")
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "user.create").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
	// The typed values were removed from the chat.
	var deleted int
	for _, c := range r.tg.since(0) {
		if c.method == "deleteMessage" {
			deleted++
		}
	}
	if deleted < 2 {
		t.Fatalf("%d typed messages removed", deleted)
	}
}

func (r *rig) createUser(name string, gb int64, days int) db.User {
	r.t.Helper()
	var root db.Admin
	r.gdb.Where("username = ?", "root").First(&root)
	exp := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	st, raw := r.srv.Do(context.Background(), &root, "POST", "/users", map[string]any{"username": name, "data_limit": gb << 30,
		"expire_at": exp, "inbound_ids": []int64{r.ib}})
	if st != 201 {
		r.t.Fatalf("create %d %s", st, raw)
	}
	return r.user(name)
}

func TestRenewUser(t *testing.T) {
	r := newRig(t)
	u := r.createUser("bob", 10, 5)
	r.start()
	v := r.press(fmt.Sprintf("uv:%d", u.ID), "bob")
	c := r.press(button(t, v, "تمدید"), "تمدید bob")
	r.press(button(t, c, "۳۰ روز"), "bob")
	after := r.user("bob")
	if d := after.ExpireAt.Sub(*u.ExpireAt); d < 30*24*time.Hour-time.Minute || d > 30*24*time.Hour+time.Minute {
		t.Fatalf("extended by %v", d)
	}
	c = r.press(fmt.Sprintf("ur:%d", u.ID), "تمدید bob")
	r.press(button(t, c, "۵۰ GB"), "bob")
	if got := r.user("bob").DataLimit; got != 60<<30 {
		t.Fatalf("data limit %d", got)
	}
	// Custom number of days, typed in Persian digits.
	c = r.press(fmt.Sprintf("ur:%d", u.ID), "تمدید bob")
	r.press(button(t, c, "روز دلخواه"), "چند روز")
	r.typeText("۷", "bob")
	if d := r.user("bob").ExpireAt.Sub(*after.ExpireAt); d < 7*24*time.Hour-time.Minute || d > 7*24*time.Hour+time.Minute {
		t.Fatalf("custom extend %v", d)
	}
}

func TestDeleteUserNeedsConfirm(t *testing.T) {
	r := newRig(t)
	u := r.createUser("carol", 1, 3)
	r.start()
	v := r.press(fmt.Sprintf("uv:%d", u.ID), "carol")
	c := r.press(button(t, v, "حذف"), "حذف شود")
	if r.user("carol").ID == 0 {
		t.Fatal("deleted before confirm")
	}
	// "No" goes back to the user and keeps it.
	r.press(button(t, c, "نه"), "carol")
	if r.user("carol").ID == 0 {
		t.Fatal("deleted on no")
	}
	c = r.press(fmt.Sprintf("ud:%d", u.ID), "حذف شود")
	r.press(button(t, c, "بله"), "کاربر")
	if r.user("carol").ID != 0 {
		t.Fatal("user still exists")
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "user.delete").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
}
