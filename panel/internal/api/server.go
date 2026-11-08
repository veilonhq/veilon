// Package api implements the HTTP API (v1), routing for the panel UI and
// public subscription endpoints.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/stats"
	"github.com/veilonhq/veilon/panel/internal/sub"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/webui"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Core is the subset of the Xray manager the API needs.
type Core interface {
	// Status is the primary engine (Xray-core).
	Status() xray.Status
	// Statuses lists every engine (xray, sing-box, mtg, amneziawg).
	Statuses() []xray.Status
	Restart()
	Logs(core string, n int) ([]string, error)
	GenerateConfig(core string) ([]byte, error)
}

// Server wires services into HTTP handlers.
type Server struct {
	DB         *gorm.DB
	Auth       *auth.Service
	Users      *users.Service
	Inbounds   *inbounds.Service
	Certs      *certs.Service
	Sub        *sub.Service
	Core       Core
	Sampler    *stats.Sampler
	TrustProxy bool
	// OnListenerChange is called (after the response is written) when the
	// panel port or certificate changed and the listener must restart.
	OnListenerChange func()
	// Live returns a user's live client IPs (connection guard; may be nil).
	Live func(userID int64) []string

	settings atomic.Pointer[db.Settings]
	api      http.Handler
}

// New builds a server and its routes.
func New(s *Server) *Server {
	s.ReloadSettings()
	s.api = s.routes()
	return s
}

// ReloadSettings refreshes the cached settings.
func (s *Server) ReloadSettings() {
	st := db.LoadSettings(s.DB)
	s.settings.Store(&st)
}

// Settings returns cached settings.
func (s *Server) Settings() db.Settings { return *s.settings.Load() }

// ServeHTTP dispatches between API, UI and subscription paths.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	p := r.URL.Path
	panel := "/" + st.PanelPath
	apiPrefix := panel + "/api/v1"
	switch {
	case st.PanelPath != "" && p == panel:
		http.Redirect(w, r, panel+"/", http.StatusMovedPermanently)
	case st.PanelPath != "" && (p == apiPrefix || strings.HasPrefix(p, apiPrefix+"/")):
		securityHeaders(w)
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		http.StripPrefix(apiPrefix, s.api).ServeHTTP(w, r)
	case st.PanelPath != "" && strings.HasPrefix(p, panel+"/"):
		securityHeaders(w)
		webui.Handler(panel+"/", st.Language).ServeHTTP(w, r)
	case strings.HasPrefix(p, "/"+st.SubPath+"/"):
		rest := strings.Trim(strings.TrimPrefix(p, "/"+st.SubPath+"/"), "/")
		token, tail, _ := strings.Cut(rest, "/")
		if token == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if idStr, ok := strings.CutPrefix(tail, "conf/"); ok {
			id, err := strconv.ParseInt(strings.TrimSuffix(idStr, ".conf"), 10, 64)
			if err != nil || id <= 0 {
				http.NotFound(w, r)
				return
			}
			s.Sub.ServeConf(w, r, token, id)
			return
		}
		if tail != "" && tail != "info" {
			http.NotFound(w, r)
			return
		}
		s.Sub.Serve(w, r, token, tail == "info")
	default:
		// ACME http-01 when the panel itself listens on the challenge port.
		if s.Certs != nil && s.Certs.ServeHTTP01(w, r) {
			return
		}
		http.NotFound(w, r)
	}
}

func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
}

// --- helpers -------------------------------------------------------------

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

// writeServiceErr maps service errors to HTTP responses.
func writeServiceErr(w http.ResponseWriter, err error) {
	var (
		uv *users.ValidationError
		iv *inbounds.ValidationError
		cv *certs.ValidationError
		ic *inbounds.ConflictError
	)
	switch {
	case errors.As(err, &ic):
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
			"code": "conflict", "message": ic.Msg, "field": ic.Field, "inbound": ic.Other}})
	case errors.As(err, &uv), errors.As(err, &iv), errors.As(err, &cv):
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, users.ErrNotFound), errors.Is(err, inbounds.ErrNotFound), errors.Is(err, certs.ErrNotFound), db.IsNotFound(err):
		writeErr(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, certs.ErrInUse):
		writeErr(w, http.StatusConflict, "conflict", err.Error())
	default:
		slog.Error("api error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip.String()
			}
		}
		if xr := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); xr != nil {
			return xr.String()
		}
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

type authed func(w http.ResponseWriter, r *http.Request, a *db.Admin)

func (s *Server) authn(h authed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		tok, ok := strings.CutPrefix(hdr, "Bearer ")
		if !ok || tok == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		a, err := s.Auth.Verify(strings.TrimSpace(tok))
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", err.Error())
			return
		}
		h(w, r, a)
	}
}

func (s *Server) sudo(h authed) http.HandlerFunc {
	return s.authn(func(w http.ResponseWriter, r *http.Request, a *db.Admin) {
		if !a.IsSudo {
			writeErr(w, http.StatusForbidden, "forbidden", "sudo admin required")
			return
		}
		h(w, r, a)
	})
}

func adminView(a *db.Admin) map[string]any {
	return map[string]any{"id": a.ID, "username": a.Username, "is_sudo": a.IsSudo, "created_at": a.CreatedAt,
		"permissions": auth.PermList(a), "user_quota": a.UserQuota, "traffic_quota": a.TrafficQuota}
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }
