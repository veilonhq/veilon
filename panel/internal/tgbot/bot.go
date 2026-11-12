package tgbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// Caller runs one request through the panel's own API handlers in process,
// acting as actor. path is relative to /api/v1 ("/users?page=1").
type Caller interface {
	Do(ctx context.Context, actor *db.Admin, method, path string, body any) (int, []byte)
}

// Backups creates and restores database backups.
type Backups interface {
	// Create returns a consistent copy of the database.
	Create(ctx context.Context) (name string, data []byte, err error)
	// Check validates an uploaded backup and describes it.
	Check(name string, data []byte) (string, error)
	// Restore stages the backup and restarts the panel, which puts it in
	// place before it opens the database (a safety copy of the current
	// database is kept).
	Restore(name string, data []byte) error
}

// Status is the bot's live state for the settings page.
type Status struct {
	Running  bool       `json:"running"`
	Username string     `json:"username"`
	Error    string     `json:"error"`
	Since    *time.Time `json:"since"`
}

// Bot is the admin bot. Run it once; Reload after the settings change.
type Bot struct {
	DB      *gorm.DB
	API     Caller
	Backups Backups
	Version string
	// Base overrides the Bot API URL (tests); "" = APIBase().
	Base string
	// PollTimeout is getUpdates' long-poll wait in seconds (default 25).
	PollTimeout int
	// NotifyEvery is the notification check interval (default 1 minute).
	NotifyEvery time.Duration
	// Now is the clock (tests).
	Now func() time.Time

	once   sync.Once
	reload chan struct{}
	mu     sync.Mutex
	status Status
	chats  map[int64]*chatState
	locks  map[int64]*sync.Mutex
	// nodeOff counts the checks a node was seen offline in a row.
	nodeOff map[int64]int
}

func (b *Bot) init() {
	b.once.Do(func() {
		b.reload = make(chan struct{}, 1)
		b.chats = map[int64]*chatState{}
		b.locks = map[int64]*sync.Mutex{}
		b.nodeOff = map[int64]int{}
		if b.PollTimeout == 0 {
			b.PollTimeout = 25
		}
		if b.NotifyEvery == 0 {
			b.NotifyEvery = time.Minute
		}
	})
}

func (b *Bot) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// Reload restarts the bot with the stored settings.
func (b *Bot) Reload() {
	b.init()
	select {
	case b.reload <- struct{}{}:
	default:
	}
}

// Status returns the live state.
func (b *Bot) Status() Status {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *Bot) setStatus(f func(s *Status)) {
	b.mu.Lock()
	f(&b.status)
	b.mu.Unlock()
}

// Run keeps the bot running with the stored settings until ctx ends.
func (b *Bot) Run(ctx context.Context) {
	b.init()
	for {
		cfg := LoadConfig(b.DB)
		var done chan struct{}
		cancel := func() {}
		if cfg.Enabled && cfg.Token != "" && len(cfg.Admins) > 0 {
			var sctx context.Context
			sctx, cancel = context.WithCancel(ctx)
			done = make(chan struct{})
			go func() {
				defer close(done)
				util.Supervise(sctx, "telegram bot", func(c context.Context) { b.session(c, cfg) })
			}()
		} else {
			b.setStatus(func(s *Status) { *s = Status{} })
		}
		select {
		case <-ctx.Done():
			cancel()
			if done != nil {
				<-done
			}
			return
		case <-b.reload:
			cancel()
			if done != nil {
				<-done
			}
			b.mu.Lock()
			b.chats = map[int64]*chatState{}
			b.mu.Unlock()
		}
	}
}

func (b *Bot) client(cfg Config) (*Client, error) {
	return NewClient(cfg.Token, b.Base, cfg.Proxy)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// session runs one bot session: it identifies the bot, then long-polls.
func (b *Bot) session(ctx context.Context, cfg Config) {
	cl, err := b.client(cfg)
	if err != nil {
		b.setStatus(func(s *Status) { *s = Status{Error: err.Error()} })
		<-ctx.Done()
		return
	}
	pause := 3 * time.Second
	var me *User
	for {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		me, err = cl.GetMe(cctx)
		cancel()
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("telegram bot: cannot reach Telegram", "err", err)
		b.setStatus(func(s *Status) { *s = Status{Error: err.Error()} })
		if !sleepCtx(ctx, pause) {
			return
		}
		if pause < time.Minute {
			pause *= 2
		}
	}
	now := b.now()
	b.setStatus(func(s *Status) { *s = Status{Running: true, Username: me.Username, Since: &now} })
	slog.Info("telegram bot started", "bot", me.Username, "admins", len(cfg.Admins))
	defer slog.Info("telegram bot stopped")

	var offset int64
	pause = 2 * time.Second
	for ctx.Err() == nil {
		pctx, cancel := context.WithTimeout(ctx, time.Duration(b.PollTimeout+15)*time.Second)
		ups, err := cl.GetUpdates(pctx, offset, b.PollTimeout)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ae *APIError
			if errors.As(err, &ae) && ae.Code == http.StatusConflict {
				// A webhook is set on this bot: polling needs it removed.
				_ = cl.Call(ctx, "deleteWebhook", map[string]any{}, nil)
			}
			if errors.As(err, &ae) && ae.RetryAfter > 0 {
				pause = time.Duration(ae.RetryAfter) * time.Second
			}
			b.setStatus(func(s *Status) { s.Error = err.Error() })
			slog.Warn("telegram bot: getUpdates failed", "err", err)
			if !sleepCtx(ctx, pause) {
				return
			}
			if pause < 30*time.Second {
				pause *= 2
			}
			continue
		}
		pause = 2 * time.Second
		b.setStatus(func(s *Status) { s.Error = "" })
		for _, up := range ups {
			if up.UpdateID >= offset {
				offset = up.UpdateID + 1
			}
			up := up
			go util.Safe("telegram update", func() { b.handle(ctx, cl, cfg, &up) })
		}
	}
}

func (b *Bot) chatLock(id int64) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		b.locks[id] = l
	}
	return l
}

func (b *Bot) state(chat int64) *chatState {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.chats[chat]
	if st == nil {
		st = &chatState{draft: map[string]string{}}
		b.chats[chat] = st
	}
	return st
}

// handle processes one update from an admin; anyone else is ignored.
func (b *Bot) handle(ctx context.Context, cl *Client, cfg Config, up *Update) {
	var from *User
	var chat Chat
	switch {
	case up.CallbackQuery != nil:
		from = &up.CallbackQuery.From
		if up.CallbackQuery.Message == nil {
			return
		}
		chat = up.CallbackQuery.Message.Chat
	case up.Message != nil:
		from, chat = up.Message.From, up.Message.Chat
	default:
		return
	}
	if from == nil || from.IsBot || chat.Type != "private" || chat.ID != from.ID || !cfg.IsAdmin(from.ID) {
		return
	}
	l := b.chatLock(chat.ID)
	l.Lock()
	defer l.Unlock()
	hctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	r := &req{b: b, ctx: hctx, cl: cl, cfg: cfg, chat: chat.ID, from: from.ID, st: b.state(chat.ID)}
	if q := up.CallbackQuery; q != nil {
		r.cbID = q.ID
		r.msgID = q.Message.MessageID
		r.st.menuMsg = q.Message.MessageID
		r.route(q.Data)
		if !r.answered {
			_ = cl.Answer(hctx, q.ID, "", false)
		}
		return
	}
	r.onMessage(up.Message)
}

// actor is the panel admin the bot acts as: the first full (sudo) admin,
// named "telegram:<id>" in the audit log.
func (b *Bot) actor(tgID int64) (*db.Admin, error) {
	var a db.Admin
	if err := b.DB.Where("is_sudo = ?", true).Order("id").Limit(1).Find(&a).Error; err != nil {
		return nil, err
	}
	if a.ID == 0 {
		return nil, errors.New("no full admin exists in the panel")
	}
	a.Username = fmt.Sprintf("telegram:%d", tgID)
	return &a, nil
}

// apiErr is an error answer of the panel API.
type apiErr struct {
	Status  int
	Code    string
	Message string
	Raw     []byte
}

func (e *apiErr) Error() string { return e.Message }

// call runs an API request and decodes a 2xx answer into out.
func (b *Bot) call(ctx context.Context, tgID int64, method, path string, body, out any) error {
	a, err := b.actor(tgID)
	if err != nil {
		return err
	}
	st, raw := b.API.Do(ctx, a, method, path, body)
	if st < 200 || st > 299 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if msg == "" {
			msg = http.StatusText(st)
		}
		return &apiErr{Status: st, Code: e.Error.Code, Message: msg, Raw: raw}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Test checks a token: it identifies the bot and sends a test message to
// every admin id. It returns the bot account and one result per admin.
func Test(ctx context.Context, cfg Config, base string) (*User, map[int64]error, error) {
	cl, err := NewClient(cfg.Token, base, cfg.Proxy)
	if err != nil {
		return nil, nil, err
	}
	me, err := cl.GetMe(ctx)
	if err != nil {
		return nil, nil, err
	}
	res := map[int64]error{}
	for _, id := range cfg.Admins {
		_, err := cl.Send(ctx, id, "✅ <b>Veilon</b>\nاتصال ربات به پنل برقرار است. برای باز کردن منو /start را بفرستید.", nil)
		res[id] = err
	}
	return me, res, nil
}
