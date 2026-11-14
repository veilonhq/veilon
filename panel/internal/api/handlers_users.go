package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/sub"
	"github.com/veilonhq/veilon/panel/internal/userfw"
	"github.com/veilonhq/veilon/panel/internal/users"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	per, _ := strconv.Atoi(q.Get("per_page"))
	gid, _ := strconv.ParseInt(q.Get("group_id"), 10, 64)
	items, total, err := s.Users.List(users.ListParams{
		Search: q.Get("search"), Status: q.Get("status"), Page: page, PerPage: per, Sort: q.Get("sort"),
		GroupID: gid, AdminID: auth.OwnerScope(a),
	}, r.Host)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var in users.Input
	if !decode(w, r, &in) {
		return
	}
	var owner *int64
	if !a.IsSudo {
		id := a.ID
		owner = &id
		if err := s.checkQuota(&db.Admin{ID: a.ID, UserQuota: a.UserQuota}, 0, 0, true); err != nil {
			writeQuotaOr(w, err)
			return
		}
	}
	v, err := s.Users.CreateOwned(&in, r.Host, owner)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	// The traffic quota is checked against the final data limit (group
	// defaults included); a user that does not fit is removed again.
	if !a.IsSudo && a.TrafficQuota > 0 {
		if err := s.checkQuota(&db.Admin{ID: a.ID, TrafficQuota: a.TrafficQuota}, v.ID, v.DataLimit, false); err != nil {
			_, _ = s.Users.Delete(v.ID)
			writeQuotaOr(w, err)
			return
		}
	}
	audit.Log(s.DB, a, "user.create", fmt.Sprintf("user:%d %s", v.ID, v.Username), s.clientIP(r))
	if len(v.Firewall) > 0 {
		s.auditFirewall(r, a, v, nil)
	}
	writeJSON(w, 201, v)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := s.Users.Get(id, r.Host)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in users.Input
	if !decode(w, r, &in) {
		return
	}
	if !a.IsSudo && a.TrafficQuota > 0 && in.DataLimit.Set {
		if err := s.checkQuota(a, id, in.DataLimit.Val, false); err != nil {
			writeQuotaOr(w, err)
			return
		}
	}
	var fwBefore []string
	if in.Firewall.Set {
		if u, err := s.Users.GetModel(id); err == nil {
			fwBefore = userfw.Split(u.Firewall)
		}
	}
	v, err := s.Users.Update(id, &in, r.Host)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "user.update", fmt.Sprintf("user:%d %s", v.ID, v.Username), s.clientIP(r))
	if in.Firewall.Set {
		s.auditFirewall(r, a, v, fwBefore)
	}
	writeJSON(w, 200, v)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.Users.GetModel(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if _, err := s.Users.Delete(id); err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "user.delete", fmt.Sprintf("user:%d %s", id, u.Username), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) resetUserTraffic(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.Users.GetModel(id); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := s.Users.ResetTraffic(id); err != nil {
		writeServiceErr(w, err)
		return
	}
	v, err := s.Users.Get(id, r.Host)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "user.reset_traffic", fmt.Sprintf("user:%d %s", id, v.Username), s.clientIP(r))
	writeJSON(w, 200, v)
}

func (s *Server) revokeUserSub(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := s.Users.RevokeSub(id, r.Host)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "user.revoke_sub", fmt.Sprintf("user:%d %s", id, v.Username), s.clientIP(r))
	writeJSON(w, 200, v)
}

func (s *Server) userLinks(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.Users.GetModel(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	_, ibs, err := s.Sub.Resolve(u.SubToken)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	st := s.Settings()
	ps := sub.BuildProxiesX(u, ibs, st, r.Host, s.Sub.Extras(u, ibs))
	links := make([]map[string]any, 0, len(ps))
	for i := range ps {
		l := map[string]any{"inbound_id": ps[i].InboundID, "remark": ps[i].Name, "protocol": ps[i].Protocol, "uri": sub.URI(&ps[i])}
		if name, body, ok := sub.ConfFile(&ps[i]); ok {
			l["conf_name"], l["conf"] = name, body
			l["conf_url"] = fmt.Sprintf("%s/conf/%d", sub.SubURL(st, u.SubToken, r.Host), ps[i].InboundID)
		}
		links = append(links, l)
	}
	writeJSON(w, 200, map[string]any{"links": links, "sub_url": sub.SubURL(st, u.SubToken, r.Host)})
}

func (s *Server) bulkUsers(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Action  string  `json:"action"`
		IDs     []int64 `json:"ids"`
		Days    int     `json:"days"`
		GroupID int64   `json:"group_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	need := auth.PermUsersEdit
	if req.Action == "delete" {
		need = auth.PermUsersDelete
	}
	if !auth.Has(a, need) {
		writeErr(w, http.StatusForbidden, "forbidden", "permission required: "+need)
		return
	}
	if len(req.IDs) > 0 {
		req.IDs = s.ownedIDs(a, req.IDs)
		if len(req.IDs) == 0 {
			writeErr(w, http.StatusNotFound, "not_found", users.ErrNotFound.Error())
			return
		}
	}
	if len(req.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "validation_error", "ids is required")
		return
	}
	if len(req.IDs) > 10000 {
		writeErr(w, http.StatusBadRequest, "validation_error", "too many ids (max 10000)")
		return
	}
	var err error
	affected := int64(len(req.IDs))
	switch req.Action {
	case "delete":
		affected, err = s.Users.Delete(req.IDs...)
	case "disable":
		err = s.Users.SetDisabled(true, req.IDs...)
	case "enable":
		err = s.Users.SetDisabled(false, req.IDs...)
	case "reset_traffic":
		err = s.Users.ResetTraffic(req.IDs...)
	case "extend":
		err = s.Users.Extend(req.Days, req.IDs...)
	case "set_group":
		err = s.Users.SetGroup(req.GroupID, req.IDs...)
	default:
		writeErr(w, http.StatusBadRequest, "validation_error", "action must be delete, disable, enable, reset_traffic, extend or set_group")
		return
	}
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "user.bulk_"+req.Action, fmt.Sprintf("%d users %v", len(req.IDs), req.IDs), s.clientIP(r))
	writeJSON(w, 200, map[string]any{"ok": true, "affected": affected})
}
