package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	ip := s.clientIP(r)
	a, tok, exp, err := s.Auth.Login(ip, strings.TrimSpace(req.Username), req.Password)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	case errors.Is(err, auth.ErrAdminDisabled):
		audit.Log(s.DB, nil, "auth.login_failed", req.Username, ip)
		writeErr(w, http.StatusForbidden, "admin_disabled", "this admin account is disabled")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		audit.Log(s.DB, nil, "auth.login_failed", req.Username, ip)
		writeErr(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	case err != nil:
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "auth.login", a.Username, ip)
	writeJSON(w, 200, map[string]any{"token": tok, "expires_at": rfc(exp), "admin": adminView(a)})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	writeJSON(w, 200, adminView(a))
}

// updateMe lets the signed-in admin change their own username. The current
// password is required. The response carries a fresh token (its claims hold
// the username) and the updated admin.
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Username string `json:"username"`
		Current  string `json:"current_password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !auth.CheckPassword(a.PasswordHash, req.Current) {
		writeErr(w, http.StatusBadRequest, "invalid_credentials", "current password is incorrect")
		return
	}
	old := a.Username
	switch err := auth.RenameAdmin(s.DB, a, strings.TrimSpace(req.Username)); {
	case errors.Is(err, auth.ErrBadUsername):
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	case errors.Is(err, auth.ErrUsernameTaken):
		writeErr(w, http.StatusConflict, "conflict", err.Error())
		return
	case err != nil:
		writeServiceErr(w, err)
		return
	}
	if old != a.Username {
		audit.Log(s.DB, a, "auth.username_changed", old+" -> "+a.Username, s.clientIP(r))
	}
	tok, exp, err := s.Auth.Issue(a)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok, "expires_at": rfc(exp), "admin": adminView(a)})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !auth.CheckPassword(a.PasswordHash, req.Current) {
		writeErr(w, http.StatusBadRequest, "invalid_credentials", "current password is incorrect")
		return
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if err := auth.SetPassword(s.DB, a, req.New); err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "auth.password_changed", a.Username, s.clientIP(r))
	tok, exp, err := s.Auth.Issue(a)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "token": tok, "expires_at": rfc(exp)})
}

func (s *Server) listAdmins(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var rows []db.Admin
	if err := s.DB.Order("id").Find(&rows).Error; err != nil {
		writeServiceErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, s.adminFull(&rows[i]))
	}
	writeJSON(w, 200, out)
}

func (s *Server) createAdmin(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsSudo   bool   `json:"is_sudo"`
		// Sub-admins only (defaults: manage own users, no quotas).
		Permissions  *[]string `json:"permissions"`
		UserQuota    int       `json:"user_quota"`
		TrafficQuota int64     `json:"traffic_quota"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.UserQuota < 0 || req.TrafficQuota < 0 {
		writeErr(w, http.StatusBadRequest, "validation_error", "quotas must be >= 0")
		return
	}
	na, err := auth.CreateAdmin(s.DB, strings.TrimSpace(req.Username), req.Password, req.IsSudo)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if !na.IsSudo {
		perms := auth.DefaultSubPerms
		if req.Permissions != nil {
			perms = *req.Permissions
		}
		na.Permissions, na.UserQuota, na.TrafficQuota = auth.EncodePerms(perms), req.UserQuota, req.TrafficQuota
		s.DB.Model(&db.Admin{}).Where("id = ?", na.ID).Updates(map[string]any{
			"permissions": na.Permissions, "user_quota": na.UserQuota, "traffic_quota": na.TrafficQuota})
	}
	audit.Log(s.DB, a, "admin.create", na.Username, s.clientIP(r))
	writeJSON(w, 201, s.adminFull(na))
}

func (s *Server) deleteAdmin(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if id == a.ID {
		writeErr(w, http.StatusBadRequest, "validation_error", "you cannot delete yourself")
		return
	}
	var t db.Admin
	if err := s.DB.First(&t, id).Error; err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "admin not found")
		return
	}
	// Same rules as `veilon admin delete`: never the last full (sudo) admin,
	// never an admin who still owns users. Checks and delete share one
	// transaction.
	if _, err := auth.DeleteAdmin(s.DB, t.Username); err != nil {
		var owns *auth.AdminOwnsUsersError
		switch {
		case errors.Is(err, auth.ErrAdminNotFound):
			writeErr(w, http.StatusNotFound, "not_found", "admin not found")
		case errors.Is(err, auth.ErrLastSudo):
			writeErr(w, http.StatusBadRequest, "validation_error", "cannot delete the last sudo admin")
		case errors.As(err, &owns):
			writeErr(w, http.StatusConflict, "conflict", err.Error())
		default:
			writeServiceErr(w, err)
		}
		return
	}
	audit.Log(s.DB, a, "admin.delete", t.Username, s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	q := r.URL.Query()
	items, total, err := audit.ListBy(s.DB, strings.TrimSpace(q.Get("admin")), page, per)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	out := map[string]any{"items": items, "total": total}
	if q.Get("actors") == "1" {
		out["actors"] = audit.Actors(s.DB, 30)
	}
	writeJSON(w, 200, out)
}
