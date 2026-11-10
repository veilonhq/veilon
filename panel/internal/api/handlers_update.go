package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/selfupdate"
	"github.com/veilonhq/veilon/panel/internal/version"
)

// updateInfo tells sudo admins whether a newer stable release exists, and
// reports the current or last update (it survives the restart).
func (s *Server) updateInfo(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	writeJSON(w, 200, s.updatePayload())
}

func (s *Server) updatePayload() map[string]any {
	out := map[string]any{"current": version.Version, "available": nil, "can_update": false, "checked_at": nil, "next_check_at": nil, "status": nil}
	if s.Update != nil {
		if n := s.Update.Checker.NextCheck(); !n.IsZero() {
			out["next_check_at"] = n.UTC()
		}
		rel, at := s.Update.Checker.Latest()
		if rel != nil {
			out["available"] = map[string]any{"tag": rel.Tag, "name": rel.Name, "notes": rel.Notes, "url": rel.URL, "published_at": rel.PublishedAt}
		}
		if !at.IsZero() {
			out["checked_at"] = at
		}
		out["can_update"] = s.Update.CanUpdate()
		if st := s.Update.Status(); st != nil {
			if st.Running && !selfupdate.Active(st, time.Now()) {
				st.Running, st.Phase, st.Error = false, selfupdate.PhaseFailed, "the updater stopped without finishing"
			}
			out["status"] = st
		}
	}
	return out
}

// checkTimeout bounds a manual check.
const checkTimeout = 20 * time.Second

// checkUpdate asks the release source right away (at most one real request
// per selfupdate.ManualGap; inside it the last outcome is returned with
// cached:true). The answer has the shape of GET /system/update plus cached.
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if s.Update == nil {
		writeErr(w, http.StatusNotImplemented, "unsupported", "updates are not available")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()
	cached, retry, err := s.Update.Checker.CheckNow(ctx)
	if !cached {
		res := "ok"
		if err != nil {
			res = "failed"
		}
		audit.Log(s.DB, a, "panel.update_check", res, s.clientIP(r))
	}
	if err != nil {
		status, code, msg := http.StatusBadGateway, "update_check_failed", "could not reach the release server; check the server's internet connection and try again"
		switch {
		case errors.Is(err, selfupdate.ErrRateLimited):
			status, code, msg = http.StatusServiceUnavailable, "rate_limited", "GitHub's rate limit was reached; try again later"
		case errors.Is(err, context.DeadlineExceeded):
			status, code, msg = http.StatusGatewayTimeout, "timeout", "the release server did not answer in time; try again"
		}
		writeJSON(w, status, map[string]any{
			"error":       map[string]any{"code": code, "message": msg, "detail": err.Error()},
			"retry_after": int((retry + time.Second - 1) / time.Second),
		})
		return
	}
	out := s.updatePayload()
	out["cached"] = cached
	writeJSON(w, 200, out)
}

func (s *Server) startUpdate(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if s.Update == nil {
		writeErr(w, http.StatusNotImplemented, "unsupported", "updates are not available")
		return
	}
	var body struct {
		Tag string `json:"tag"`
	}
	if !decode(w, r, &body) {
		return
	}
	st, err := s.Update.Start(body.Tag, a.Username)
	switch {
	case errors.Is(err, selfupdate.ErrBusy):
		writeErr(w, http.StatusConflict, "busy", err.Error())
		return
	case err != nil && st == nil:
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "update_error", err.Error())
		return
	}
	audit.Log(s.DB, a, "panel.update", version.Version+" -> "+body.Tag, s.clientIP(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "status": st})
}
