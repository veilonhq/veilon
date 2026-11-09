package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// xrayProvider is implemented by a Core that exposes Xray-core maintenance.
type xrayProvider interface {
	XrayController() xray.Controller
}

func (s *Server) xrayCtl(w http.ResponseWriter) xray.Controller {
	if p, ok := s.Core.(xrayProvider); ok {
		if c := p.XrayController(); c != nil {
			return c
		}
	}
	writeErr(w, http.StatusNotImplemented, "unsupported", "xray core controls are not available")
	return nil
}

// coreXray reports the core state, the current maintenance job and geo files.
func (s *Server) coreXray(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	writeJSON(w, 200, map[string]any{"status": c.Status(), "job": c.Job(), "geo": c.GeoStatus()})
}

func (s *Server) corePower(on bool) func(http.ResponseWriter, *http.Request, *db.Admin) {
	return func(w http.ResponseWriter, r *http.Request, a *db.Admin) {
		c := s.xrayCtl(w)
		if c == nil {
			return
		}
		if err := c.SetPower(r.Context(), on); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "core_error", err.Error())
			return
		}
		act := "core.stop"
		if on {
			act = "core.start"
		}
		audit.Log(s.DB, a, act, "xray", s.clientIP(r))
		writeJSON(w, 200, map[string]any{"ok": true, "status": c.Status()})
	}
}

func (s *Server) coreReleases(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	list, err := c.Releases(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		code, status := "upstream_error", http.StatusBadGateway
		if errors.Is(err, xray.ErrRateLimited) {
			code, status = "rate_limited", http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": err.Error()}, "rate_limited_until": list.RateLimitedUntil})
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) coreInstall(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	v := strings.TrimSpace(body.Version)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if err := c.StartInstall(v); err != nil {
		s.coreJobErr(w, err)
		return
	}
	audit.Log(s.DB, a, "core.install", "xray "+v, s.clientIP(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job": c.Job()})
}

func (s *Server) coreGeoUpdate(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	c := s.xrayCtl(w)
	if c == nil {
		return
	}
	if err := c.StartGeoUpdate(); err != nil {
		s.coreJobErr(w, err)
		return
	}
	audit.Log(s.DB, a, "core.geo_update", "xray", s.clientIP(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job": c.Job()})
}

func (s *Server) coreJobErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, xray.ErrBusy):
		writeErr(w, http.StatusConflict, "busy", err.Error())
	case errors.Is(err, xray.ErrBadVersion):
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "core_error", err.Error())
	}
}
