package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

// The bot token never leaves the server in full: answers carry only
// token_set and a hint (bot id and last four characters), and neither the
// log nor the audit log ever sees it.

func (s *Server) telegramView() map[string]any {
	c := tgbot.LoadConfig(s.DB)
	v := map[string]any{
		"enabled":    c.Enabled,
		"admins":     c.Admins,
		"proxy":      c.Proxy,
		"notify":     c.Notify,
		"token_set":  c.Token != "",
		"token_hint": tgbot.TokenHint(c.Token),
		"max_admins": tgbot.MaxAdmins,
	}
	if s.Telegram != nil {
		v["status"] = s.Telegram.Status()
	} else {
		v["status"] = tgbot.Status{}
	}
	return v
}

func (s *Server) getTelegram(w http.ResponseWriter, _ *http.Request, _ *db.Admin) {
	writeJSON(w, http.StatusOK, s.telegramView())
}

type telegramReq struct {
	Enabled *bool         `json:"enabled"`
	Admins  *[]int64      `json:"admins"`
	Proxy   *string       `json:"proxy"`
	Notify  *tgbot.Notify `json:"notify"`
	// Token: omitted or "" keeps the stored token; ClearToken removes it.
	Token      string `json:"token"`
	ClearToken bool   `json:"clear_token"`
}

func (s *Server) putTelegram(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req telegramReq
	if !decode(w, r, &req) {
		return
	}
	c := tgbot.LoadConfig(s.DB)
	oldTok := c.Token
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	if req.Admins != nil {
		c.Admins = *req.Admins
	}
	if req.Proxy != nil {
		c.Proxy = *req.Proxy
	}
	if req.Notify != nil {
		c.Notify = *req.Notify
	}
	if req.ClearToken {
		c.Token = ""
	}
	if t := strings.TrimSpace(req.Token); t != "" {
		c.Token = t
	}
	if err := c.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if err := tgbot.SaveConfig(s.DB, c); err != nil {
		writeServiceErr(w, err)
		return
	}
	detail := "enabled=" + strconv.FormatBool(c.Enabled) + " admins=" + strconv.Itoa(len(c.Admins))
	if c.Token != oldTok {
		if c.Token == "" {
			detail += " token=removed"
		} else {
			detail += " token=changed"
		}
	}
	audit.Log(s.DB, a, "telegram.update", detail, s.clientIP(r))
	if s.Telegram != nil {
		s.Telegram.Reload()
	}
	writeJSON(w, http.StatusOK, s.telegramView())
}

type telegramTestReq struct {
	Token  string   `json:"token"`
	Admins *[]int64 `json:"admins"`
	Proxy  *string  `json:"proxy"`
}

// testTelegram checks a token (the one given, else the stored one): it
// identifies the bot and sends a test message to each admin id.
func (s *Server) testTelegram(w http.ResponseWriter, r *http.Request, _ *db.Admin) {
	var req telegramTestReq
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	c := tgbot.LoadConfig(s.DB)
	if t := strings.TrimSpace(req.Token); t != "" {
		c.Token = t
	}
	if req.Admins != nil {
		c.Admins = *req.Admins
	}
	if req.Proxy != nil {
		c.Proxy = *req.Proxy
	}
	c.Enabled = false
	if c.Token == "" {
		writeErr(w, http.StatusBadRequest, "validation_error", "a bot token is required")
		return
	}
	if err := c.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	base := ""
	if s.Telegram != nil {
		base = s.Telegram.Base
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	me, res, err := tgbot.Test(ctx, c, base)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "telegram_error", redactToken(err.Error(), c.Token))
		return
	}
	out := []map[string]any{}
	for _, id := range c.Admins {
		e := res[id]
		row := map[string]any{"id": id, "ok": e == nil}
		if e != nil {
			row["error"] = redactToken(e.Error(), c.Token)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": map[string]any{"id": me.ID, "username": me.Username, "name": me.FirstName}, "admins": out})
}

func redactToken(s, tok string) string {
	if tok == "" {
		return s
	}
	return strings.ReplaceAll(s, tok, "<token>")
}
