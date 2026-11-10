package api

import (
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
	out := map[string]any{"current": version.Version, "available": nil, "can_update": false, "checked_at": nil, "status": nil}
	if s.Update != nil {
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
