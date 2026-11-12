package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/users"
)

// --- permission middleware -------------------------------------------------

func (s *Server) perm(p string, h authed) http.HandlerFunc {
	return s.anyPerm([]string{p}, h)
}

func (s *Server) anyPerm(ps []string, h authed) http.HandlerFunc {
	return s.authn(func(w http.ResponseWriter, r *http.Request, a *db.Admin) {
		for _, p := range ps {
			if auth.Has(a, p) {
				h(w, r, a)
				return
			}
		}
		writeErr(w, http.StatusForbidden, "forbidden", "permission required: "+strings.Join(ps, " or "))
	})
}

// userPerm checks a permission and that the admin may act on the user in
// the {id} path segment (its own user unless it may see all users).
func (s *Server) userPerm(p string, h authed) http.HandlerFunc {
	return s.perm(p, func(w http.ResponseWriter, r *http.Request, a *db.Admin) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if !s.ownsUser(a, id) {
			writeErr(w, http.StatusNotFound, "not_found", users.ErrNotFound.Error())
			return
		}
		h(w, r, a)
	})
}

func (s *Server) ownsUser(a *db.Admin, id int64) bool {
	scope := auth.OwnerScope(a)
	if scope == nil {
		return true
	}
	var n int64
	s.DB.Model(&db.User{}).Where("id = ? AND admin_id = ?", id, *scope).Count(&n)
	return n > 0
}

// ownedIDs filters ids to the users the admin may act on.
func (s *Server) ownedIDs(a *db.Admin, ids []int64) []int64 {
	scope := auth.OwnerScope(a)
	if scope == nil {
		return ids
	}
	var out []int64
	s.DB.Model(&db.User{}).Where("id IN ? AND admin_id = ?", ids, *scope).Pluck("id", &out)
	return out
}

// errQuota is returned when a sub-admin's quota would be exceeded.
type quotaError struct{ msg string }

func (e *quotaError) Error() string { return e.msg }

// checkQuota enforces a sub-admin's user and traffic quotas for creating a
// user (exclude = 0) or changing one user's data limit to newLimit.
func (s *Server) checkQuota(a *db.Admin, exclude int64, newLimit int64, creating bool) error {
	if a.IsSudo {
		return nil
	}
	if creating && a.UserQuota > 0 {
		var n int64
		s.DB.Model(&db.User{}).Where("admin_id = ?", a.ID).Count(&n)
		if n >= int64(a.UserQuota) {
			return &quotaError{fmt.Sprintf("user quota reached (%d users)", a.UserQuota)}
		}
	}
	if a.TrafficQuota > 0 {
		if newLimit <= 0 {
			return &quotaError{"an unlimited data limit is not allowed with a traffic quota"}
		}
		var sum int64
		s.DB.Model(&db.User{}).Where("admin_id = ? AND id <> ?", a.ID, exclude).Select("COALESCE(SUM(data_limit),0)").Scan(&sum)
		if sum+newLimit > a.TrafficQuota {
			return &quotaError{fmt.Sprintf("traffic quota exceeded: %d of %d bytes already allocated", sum, a.TrafficQuota)}
		}
	}
	return nil
}

func writeQuotaOr(w http.ResponseWriter, err error) {
	var q *quotaError
	if errors.As(err, &q) {
		writeErr(w, http.StatusForbidden, "quota_exceeded", q.msg)
		return
	}
	writeServiceErr(w, err)
}

func subID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("sub"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

// --- connection log ----------------------------------------------------------

func (s *Server) userConnections(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, _ := pathID(w, r)
	cl, err := s.Users.Connections(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	live := map[string]bool{}
	if s.Live != nil {
		for _, ip := range s.Live(id) {
			live[ip] = true
		}
	}
	type ipOut struct {
		users.IPView
		Live bool `json:"live"`
	}
	ips := make([]ipOut, 0, len(cl.IPs))
	for _, v := range cl.IPs {
		ips = append(ips, ipOut{v, live[v.IP]})
	}
	writeJSON(w, 200, map[string]any{"ips": ips, "devices": cl.Devices})
}

func (s *Server) userBlockIP(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, _ := pathID(w, r)
	var req struct {
		IP      string `json:"ip"`
		Blocked bool   `json:"blocked"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Users.BlockIP(id, req.IP, req.Blocked); err != nil {
		writeServiceErr(w, err)
		return
	}
	act := "user.ip_unblock"
	if req.Blocked {
		act = "user.ip_block"
	}
	audit.Log(s.DB, a, act, fmt.Sprintf("user:%d %s", id, req.IP), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) userDeleteIP(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, _ := pathID(w, r)
	sid, ok := subID(w, r)
	if !ok {
		return
	}
	if err := s.Users.DeleteIP(id, sid); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) userBlockDevice(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, _ := pathID(w, r)
	sid, ok := subID(w, r)
	if !ok {
		return
	}
	var req struct {
		Blocked bool `json:"blocked"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Users.BlockDevice(id, sid, req.Blocked); err != nil {
		writeServiceErr(w, err)
		return
	}
	act := "user.device_unblock"
	if req.Blocked {
		act = "user.device_block"
	}
	audit.Log(s.DB, a, act, fmt.Sprintf("user:%d device:%d", id, sid), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) userDeleteDevice(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, _ := pathID(w, r)
	sid, ok := subID(w, r)
	if !ok {
		return
	}
	if err := s.Users.DeleteDevice(id, sid); err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "user.device_delete", fmt.Sprintf("user:%d device:%d", id, sid), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// --- groups ------------------------------------------------------------------

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	gs, err := s.Users.Groups(auth.OwnerScope(a))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, gs)
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var in users.GroupInput
	if !decode(w, r, &in) {
		return
	}
	g, err := s.Users.SaveGroup(0, &in)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "group.create", fmt.Sprintf("group:%d %s", g.ID, g.Name), s.clientIP(r))
	writeJSON(w, 201, g)
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in users.GroupInput
	if !decode(w, r, &in) {
		return
	}
	g, err := s.Users.SaveGroup(id, &in)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "group.update", fmt.Sprintf("group:%d %s", g.ID, g.Name), s.clientIP(r))
	writeJSON(w, 200, g)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Users.DeleteGroup(id); err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "group.delete", fmt.Sprintf("group:%d", id), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// --- admins ------------------------------------------------------------------

func (s *Server) updateAdmin(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Permissions  *[]string `json:"permissions"`
		UserQuota    *int      `json:"user_quota"`
		TrafficQuota *int64    `json:"traffic_quota"`
		IsSudo       *bool     `json:"is_sudo"`
		Password     *string   `json:"password"`
		Disabled     *bool     `json:"disabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	var t db.Admin
	if err := s.DB.First(&t, id).Error; err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "admin not found")
		return
	}
	if req.IsSudo != nil && !*req.IsSudo && t.IsSudo {
		if t.ID == a.ID {
			writeErr(w, http.StatusBadRequest, "validation_error", "you cannot remove your own sudo access")
			return
		}
		var n int64
		s.DB.Model(&db.Admin{}).Where("is_sudo = ? AND disabled = ? AND id <> ?", true, false, t.ID).Count(&n)
		if n == 0 {
			writeErr(w, http.StatusBadRequest, "validation_error", "cannot demote the last sudo admin")
			return
		}
	}
	if req.Disabled != nil && *req.Disabled && !t.Disabled {
		if t.ID == a.ID {
			writeErr(w, http.StatusBadRequest, "validation_error", "you cannot disable yourself")
			return
		}
		if t.IsSudo {
			var n int64
			s.DB.Model(&db.Admin{}).Where("is_sudo = ? AND disabled = ? AND id <> ?", true, false, t.ID).Count(&n)
			if n == 0 {
				writeErr(w, http.StatusBadRequest, "validation_error", "cannot disable the last sudo admin")
				return
			}
		}
	}
	upd := map[string]any{}
	if req.Permissions != nil {
		upd["permissions"] = auth.EncodePerms(*req.Permissions)
	}
	if req.UserQuota != nil {
		if *req.UserQuota < 0 {
			writeErr(w, http.StatusBadRequest, "validation_error", "user_quota must be >= 0")
			return
		}
		upd["user_quota"] = *req.UserQuota
	}
	if req.TrafficQuota != nil {
		if *req.TrafficQuota < 0 {
			writeErr(w, http.StatusBadRequest, "validation_error", "traffic_quota must be >= 0")
			return
		}
		upd["traffic_quota"] = *req.TrafficQuota
	}
	if req.IsSudo != nil {
		upd["is_sudo"] = *req.IsSudo
	}
	if req.Disabled != nil {
		upd["disabled"] = *req.Disabled
	}
	if req.Password != nil && *req.Password != "" {
		if err := auth.SetPassword(s.DB, &t, *req.Password); err != nil {
			writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}
	if len(upd) > 0 {
		if err := s.DB.Model(&db.Admin{}).Where("id = ?", id).Updates(upd).Error; err != nil {
			writeServiceErr(w, err)
			return
		}
	}
	s.DB.First(&t, id)
	audit.Log(s.DB, a, "admin.update", t.Username, s.clientIP(r))
	writeJSON(w, 200, s.adminFull(&t))
}

// adminFull is the admin view with permissions and quota usage.
func (s *Server) adminFull(a *db.Admin) map[string]any {
	v := adminView(a)
	var n, sum int64
	s.DB.Model(&db.User{}).Where("admin_id = ?", a.ID).Count(&n)
	s.DB.Model(&db.User{}).Where("admin_id = ?", a.ID).Select("COALESCE(SUM(data_limit),0)").Scan(&sum)
	v["users"] = n
	v["traffic_allocated"] = sum
	return v
}
