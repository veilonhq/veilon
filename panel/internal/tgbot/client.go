// Package tgbot is the panel's built-in Telegram bot: an admin-only remote
// control for the panel (long polling, no webhook or domain needed) plus
// notifications to the admins. Every action goes through the panel's own
// API handlers in process, so validation, permissions and the audit log are
// the same as in the web UI.
package tgbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultAPI is the Telegram Bot API base URL (VEILON_TELEGRAM_API
// overrides it, for tests against a fake server).
const DefaultAPI = "https://api.telegram.org"

// APIBase returns the Bot API base URL in use.
func APIBase() string {
	if v := strings.TrimRight(os.Getenv("VEILON_TELEGRAM_API"), "/"); v != "" {
		return v
	}
	return DefaultAPI
}

// Client talks to the Telegram Bot API. The token is part of every URL, so
// every error it returns has the token removed.
type Client struct {
	Token string
	Base  string
	HTTP  *http.Client
	// Lang is the language of a chat; texts are resolved to it before they
	// are sent (nil: Persian).
	Lang func(chat int64) string
}

// local resolves a two language text for chat.
func (c *Client) local(chat int64, s string) string {
	l := langFa
	if c.Lang != nil {
		l = c.Lang(chat)
	}
	return localize(s, l)
}

func (c *Client) localKB(chat int64, kb Keyboard) Keyboard {
	l := langFa
	if c.Lang != nil {
		l = c.Lang(chat)
	}
	return localizeKB(kb, l)
}

// NewClient builds a client; proxy is "" or an http(s):// or socks5:// URL.
func NewClient(token, base, proxy string) (*Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" {
			return nil, errors.New("invalid proxy URL")
		}
		tr.Proxy = http.ProxyURL(u)
	}
	if base == "" {
		base = APIBase()
	}
	return &Client{Token: token, Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Transport: tr, Timeout: 60 * time.Second}}, nil
}

// APIError is an error answer of the Bot API.
type APIError struct {
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}

// redact removes the token from a message.
func (c *Client) redact(s string) string {
	if c.Token == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.Token, "<token>")
	return strings.ReplaceAll(s, url.PathEscape(c.Token), "<token>")
}

func (c *Client) wrap(err error) error {
	if err == nil {
		return nil
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return err
	}
	return errors.New(c.redact(err.Error()))
}

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) do(ctx context.Context, method string, body io.Reader, ctype string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/bot"+c.Token+"/"+method, body)
	if err != nil {
		return c.wrap(err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return c.wrap(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return c.wrap(err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("telegram: HTTP %d, unreadable answer", resp.StatusCode)
	}
	if !env.OK {
		return &APIError{Code: env.ErrorCode, Description: c.redact(env.Description), RetryAfter: env.Parameters.RetryAfter}
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Call sends a JSON request.
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.do(ctx, method, bytes.NewReader(b), "application/json", out)
}

// Upload sends a multipart request with one file field.
func (c *Client) Upload(ctx context.Context, method string, fields map[string]string, field, filename string, data []byte, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	return c.do(ctx, method, &buf, mw.FormDataContentType(), out)
}

// --- types --------------------------------------------------------------

// User is a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// Chat is a Telegram chat.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Document is a file sent to the bot.
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}

// Message is a Telegram message.
type Message struct {
	MessageID int       `json:"message_id"`
	From      *User     `json:"from"`
	Chat      Chat      `json:"chat"`
	Date      int64     `json:"date"`
	Text      string    `json:"text"`
	Document  *Document `json:"document"`
}

// CallbackQuery is an inline button press.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// Update is one getUpdates item.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// Button is an inline keyboard button.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Keyboard is an inline keyboard.
type Keyboard [][]Button

func markup(kb Keyboard) any {
	if kb == nil {
		kb = Keyboard{}
	}
	return map[string]any{"inline_keyboard": kb}
}

// --- methods ------------------------------------------------------------

// GetMe returns the bot's own account.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	if err := c.Call(ctx, "getMe", map[string]any{}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUpdates long-polls for updates.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var ups []Update
	err := c.Call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": timeout,
		"allowed_updates": []string{"message", "callback_query"}}, &ups)
	return ups, err
}

// Send posts a new HTML message.
func (c *Client) Send(ctx context.Context, chat int64, text string, kb Keyboard) (*Message, error) {
	text, kb = c.local(chat, text), c.localKB(chat, kb)
	p := map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if kb != nil {
		p["reply_markup"] = markup(kb)
	}
	var m Message
	if err := c.Call(ctx, "sendMessage", p, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Edit replaces the text and keyboard of a message.
func (c *Client) Edit(ctx context.Context, chat int64, msgID int, text string, kb Keyboard) error {
	text, kb = c.local(chat, text), c.localKB(chat, kb)
	p := map[string]any{"chat_id": chat, "message_id": msgID, "text": text, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true}, "reply_markup": markup(kb)}
	err := c.Call(ctx, "editMessageText", p, nil)
	var ae *APIError
	if errors.As(err, &ae) && strings.Contains(ae.Description, "message is not modified") {
		return nil
	}
	return err
}

// Answer answers a button press (text shows as a toast, or an alert).
func (c *Client) Answer(ctx context.Context, id, text string, alert bool) error {
	return c.Call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text, "show_alert": alert}, nil)
}

// Delete removes a message (best effort).
func (c *Client) Delete(ctx context.Context, chat int64, msgID int) error {
	return c.Call(ctx, "deleteMessage", map[string]any{"chat_id": chat, "message_id": msgID}, nil)
}

// SendPhoto posts a PNG with an HTML caption.
func (c *Client) SendPhoto(ctx context.Context, chat int64, png []byte, caption string) error {
	caption = c.local(chat, caption)
	return c.Upload(ctx, "sendPhoto", map[string]string{"chat_id": strconv.FormatInt(chat, 10), "caption": caption, "parse_mode": "HTML"},
		"photo", "qr.png", png, nil)
}

// SendDocument posts a file with an HTML caption.
func (c *Client) SendDocument(ctx context.Context, chat int64, name string, data []byte, caption string) error {
	caption = c.local(chat, caption)
	return c.Upload(ctx, "sendDocument", map[string]string{"chat_id": strconv.FormatInt(chat, 10), "caption": caption, "parse_mode": "HTML"},
		"document", name, data, nil)
}

// Download fetches a file sent to the bot (max bytes).
func (c *Client) Download(ctx context.Context, fileID string, max int64) ([]byte, error) {
	var f struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	if err := c.Call(ctx, "getFile", map[string]any{"file_id": fileID}, &f); err != nil {
		return nil, err
	}
	if f.FileSize > max {
		return nil, fmt.Errorf("file is larger than %d MB", max>>20)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/file/bot"+c.Token+"/"+f.FilePath, nil)
	if err != nil {
		return nil, c.wrap(err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, c.wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("telegram file download: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, c.wrap(err)
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("file is larger than %d MB", max>>20)
	}
	return b, nil
}
