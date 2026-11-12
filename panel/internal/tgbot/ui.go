package tgbot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/users"
)

// chatState is what the bot remembers about one admin's chat.
type chatState struct {
	// menuMsg is the message the menus are drawn in (edited in place).
	menuMsg int
	pending *pending
	// draft holds the values of a multi-step form.
	draft map[string]string
	// userQuery is the users list filter ("" = all) and search text.
	userFilter string
	userSearch string
	// watch changes on every interaction; a running progress watcher
	// stops once it no longer matches the value it started with.
	watch int
	// restore is an uploaded backup waiting for the admin's confirmation.
	restore     []byte
	restoreName string
	// xr is the Xray settings draft (nil: not loaded).
	xr *xrState
}

// pending is a value the bot asked the admin to type (or a file to send).
type pending struct {
	kind string
	args []string
	// back is the callback that cancels the prompt.
	back string
}

// view is one screen: a message text and its buttons.
type view struct {
	text string
	kb   Keyboard
}

// req is one admin interaction (a button press or a message).
type req struct {
	b   *Bot
	ctx context.Context
	// sctx lives as long as the bot session (for progress watchers).
	sctx     context.Context
	cl       *Client
	cfg      Config
	chat     int64
	from     int64
	st       *chatState
	cbID     string
	msgID    int
	answered bool
}

type handler func(r *req, a []string)
type inputHandler func(r *req, a []string, text string, doc *Document)

var (
	routes = map[string]handler{}
	inputs = map[string]inputHandler{}
)

func on(key string, h handler)            { routes[key] = h }
func onInput(kind string, h inputHandler) { inputs[kind] = h }

// cbd builds callback data "key:arg:arg" (Telegram allows 64 bytes).
func cbd(key string, args ...any) string {
	parts := []string{key}
	for _, a := range args {
		parts = append(parts, fmt.Sprint(a))
	}
	s := strings.Join(parts, ":")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func btn(text, key string, args ...any) Button { return Button{Text: text, Data: cbd(key, args...)} }

func (r *req) route(data string) {
	key, rest, _ := strings.Cut(data, ":")
	var args []string
	if rest != "" {
		args = strings.Split(rest, ":")
	}
	h := routes[key]
	if h == nil {
		r.toast("این دکمه دیگر کار نمی کند")
		home(r, nil)
		return
	}
	// A button press cancels a value prompt unless it belongs to it.
	if r.st.pending != nil && key != "noop" {
		r.st.pending = nil
	}
	if key != "noop" {
		r.st.watch++
	}
	h(r, args)
}

func (r *req) onMessage(m *Message) {
	r.st.watch++
	text := strings.TrimSpace(m.Text)
	if text == "/start" || text == "/menu" || strings.HasPrefix(text, "/start ") {
		r.st.pending = nil
		r.msgID = 0
		home(r, nil)
		return
	}
	p := r.st.pending
	if p == nil {
		// Typed text without a question: show the menu again.
		r.msgID = 0
		home(r, nil)
		return
	}
	h := inputs[p.kind]
	if h == nil {
		r.st.pending = nil
		return
	}
	// Keep the chat clean: the typed value is removed once read.
	_ = r.cl.Delete(r.ctx, r.chat, m.MessageID)
	r.msgID = r.st.menuMsg
	r.st.pending = nil
	h(r, p.args, text, m.Document)
}

// show draws a screen in place (or as a new message when there is none).
func (r *req) show(v view) {
	if r.msgID != 0 {
		err := r.cl.Edit(r.ctx, r.chat, r.msgID, v.text, v.kb)
		if err == nil {
			return
		}
		var ae *APIError
		if !errors.As(err, &ae) {
			return
		}
	}
	m, err := r.cl.Send(r.ctx, r.chat, v.text, v.kb)
	if err == nil {
		r.msgID = m.MessageID
		r.st.menuMsg = m.MessageID
	}
}

// fresh draws a screen as a new message at the bottom of the chat (after a
// photo or file was sent, so the menu stays in view).
func (r *req) fresh(v view) {
	r.msgID = 0
	r.show(v)
}

func (r *req) toast(text string) {
	if r.cbID == "" || r.answered {
		return
	}
	r.answered = true
	_ = r.cl.Answer(r.ctx, r.cbID, text, false)
}

func (r *req) alert(text string) {
	if r.cbID == "" || r.answered {
		return
	}
	r.answered = true
	_ = r.cl.Answer(r.ctx, r.cbID, text, true)
}

// ask shows a prompt and waits for the admin to type a value.
func (r *req) ask(kind string, args []string, title, prompt, back string, extra ...[]Button) {
	r.st.pending = &pending{kind: kind, args: args, back: back}
	kb := Keyboard{}
	kb = append(kb, extra...)
	kb = append(kb, []Button{btn("✖️ انصراف", back)})
	r.show(view{text: header("✍️", title) + prompt, kb: kb})
}

// progress shows a short "working" screen before a slow action.
func (r *req) progress(text string) {
	r.toast("⏳")
	r.show(view{text: "⏳ " + text + "\nلطفا چند لحظه صبر کنید...", kb: Keyboard{}})
}

// watch redraws the current screen every b.WatchEvery until draw reports
// done, the admin presses anything else, or limit passes. The first draw
// happens right away (in the caller).
func (r *req) watch(limit time.Duration, draw func(w *req) (view, bool)) {
	v, done := draw(r)
	r.show(v)
	if done {
		return
	}
	token := r.st.watch
	msg := r.msgID
	b, sctx := r.b, r.sctx
	if sctx == nil {
		return
	}
	go func() {
		deadline := time.Now().Add(limit)
		for time.Now().Before(deadline) {
			if !sleepCtx(sctx, b.watchEvery()) {
				return
			}
			l := b.chatLock(r.chat)
			l.Lock()
			if r.st.watch != token || r.st.menuMsg != msg {
				l.Unlock()
				return
			}
			ctx, cancel := context.WithTimeout(sctx, 30*time.Second)
			w := &req{b: b, ctx: ctx, sctx: sctx, cl: r.cl, cfg: r.cfg, chat: r.chat, from: r.from, st: r.st, msgID: msg}
			v, done := draw(w)
			w.show(v)
			cancel()
			l.Unlock()
			if done {
				return
			}
		}
	}()
}

func (r *req) call(method, path string, body, out any) error {
	return r.b.call(r.ctx, r.from, method, path, body, out)
}

// fail shows an API error under a screen with a way back.
func (r *req) fail(err error, back string) {
	r.show(view{text: header("⚠️", "انجام نشد") + esc(errText(err)), kb: Keyboard{{btn("🔙 بازگشت", back), btn("🏠 خانه", "home")}}})
}

func errText(err error) string {
	var ae *apiErr
	if errors.As(err, &ae) {
		switch ae.Status {
		case 403:
			return "دسترسی کافی نیست: " + ae.Message
		case 404:
			return "پیدا نشد (شاید حذف شده است)."
		}
		return ae.Message
	}
	return err.Error()
}

// --- formatting ----------------------------------------------------------

const line = "┈┈┈┈┈┈┈┈┈┈┈┈┈┈"

func header(icon, title string) string {
	return icon + " <b>" + esc(title) + "</b>\n" + line + "\n"
}

func esc(s string) string { return html.EscapeString(s) }

func code(s string) string { return "<code>" + esc(s) + "</code>" }

// fa writes the digits of s in Persian.
func fa(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '۰' + (r - '0')
		}
		return r
	}, s)
}

func faInt[T ~int | ~int64](n T) string { return fa(strconv.FormatInt(int64(n), 10)) }

// size formats bytes ("۱.۵ GB").
func size(b int64) string {
	if b < 0 {
		b = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(b)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	s := strconv.FormatFloat(f, 'f', 1, 64)
	if i == 0 || f >= 100 {
		s = strconv.FormatFloat(math.Round(f), 'f', 0, 64)
	}
	s = strings.TrimSuffix(s, ".0")
	return fa(s) + " " + units[i]
}

func gb(n float64) int64 { return int64(n * 1024 * 1024 * 1024) }

// parseNum reads a number typed with Persian or Latin digits.
func parseNum(s string) (float64, bool) {
	s = strings.TrimSpace(toLatinDigits(s))
	s = strings.ReplaceAll(s, "٫", ".")
	s = strings.ReplaceAll(s, "/", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func parseInt(s string) (int64, bool) {
	f, ok := parseNum(s)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

func (b *Bot) loc() *time.Location {
	return users.LoadLocation(db.LoadSettings(b.DB).Timezone)
}

// jdate formats a time as a Persian (solar hijri) date and clock in the
// panel's time zone.
func jdate(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	y, m, d := toJalali(t.Year(), int(t.Month()), t.Day())
	return fa(fmt.Sprintf("%04d/%02d/%02d %02d:%02d", y, m, d, t.Hour(), t.Minute()))
}

// toJalali converts a Gregorian date to the solar hijri calendar.
func toJalali(gy, gm, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm <= 2 {
		gy2 = gy - 1
	}
	days := 355666 + (365 * gy) + ((gy2 + 3) / 4) - ((gy2 + 99) / 100) + ((gy2 + 399) / 400) + gd + gdm[gm-1]
	jy := -1595 + (33 * (days / 12053))
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	var jm, jd int
	if days < 186 {
		jm = 1 + days/31
		jd = 1 + days%31
	} else {
		jm = 7 + (days-186)/30
		jd = 1 + (days-186)%30
	}
	return jy, jm, jd
}

// left describes the time until t ("۳ روز", "۵ ساعت").
func left(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d >= 48*time.Hour:
		return faInt(int64(d/(24*time.Hour))) + " روز"
	case d >= time.Hour:
		return faInt(int64(d/time.Hour)) + " ساعت"
	default:
		return faInt(int64(d/time.Minute)) + " دقیقه"
	}
}

func onOff(v bool) string {
	if v {
		return "✅"
	}
	return "▫️"
}

// pager adds previous / next buttons.
func pager(key string, page, pages int, args ...any) []Button {
	if pages <= 1 {
		return nil
	}
	var row []Button
	if page > 1 {
		row = append(row, btn("◀️", key, append([]any{page - 1}, args...)...))
	}
	row = append(row, btn(fa(fmt.Sprintf("%d / %d", page, pages)), "noop"))
	if page < pages {
		row = append(row, btn("▶️", key, append([]any{page + 1}, args...)...))
	}
	return row
}

func nav(back string) []Button {
	if back == "" || back == "home" {
		return []Button{btn("🏠 خانه", "home")}
	}
	return []Button{btn("🔙 بازگشت", back), btn("🏠 خانه", "home")}
}

func argInt(a []string, i int) int64 {
	if i >= len(a) {
		return 0
	}
	n, _ := strconv.ParseInt(a[i], 10, 64)
	return n
}

func argStr(a []string, i int) string {
	if i >= len(a) {
		return ""
	}
	return a[i]
}

func chunk(bs []Button, n int) Keyboard {
	var kb Keyboard
	for i := 0; i < len(bs); i += n {
		j := i + n
		if j > len(bs) {
			j = len(bs)
		}
		kb = append(kb, bs[i:j])
	}
	return kb
}

func cut(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

func init() {
	on("noop", func(r *req, a []string) { r.toast("") })
	on("home", home)
}

// home is the main menu.
func home(r *req, _ []string) {
	r.st.pending = nil
	r.st.draft = map[string]string{}
	var st struct {
		UsersTotal  int64   `json:"users_total"`
		UsersOnline int64   `json:"users_online"`
		CPU         float64 `json:"cpu_percent"`
	}
	text := header("🛡", "Veilon | مدیریت پنل")
	if err := r.call("GET", "/system/stats", nil, &st); err == nil {
		text += fmt.Sprintf("👥 کاربران: <b>%s</b>   🟢 آنلاین: <b>%s</b>\n🧠 پردازنده: <b>%s٪</b>\n",
			faInt(st.UsersTotal), faInt(st.UsersOnline), fa(strconv.FormatFloat(st.CPU, 'f', 0, 64)))
	}
	text += "\nیک بخش را انتخاب کنید:"
	r.show(view{text: text, kb: Keyboard{
		{btn("👥 کاربران", "ul", 1), btn("📥 ورودی ها", "il")},
		{btn("🌐 نود ها", "nl"), btn("📊 وضعیت سرور", "sys")},
		{btn("⬆️ بروزرسانی", "upd"), btn("💾 پشتیبان", "bk")},
		{btn("🧭 تنظیمات Xray", "xr"), btn("⚙️ تنظیمات پنل", "ps")},
		{btn("👤 ادمین ها", "al"), btn("🗂 گزارش فعالیت", "au", 1)},
	}})
}
