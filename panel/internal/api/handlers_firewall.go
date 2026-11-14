package api

import (
	"fmt"
	"net/http"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/userfw"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// Per-user firewall: the admin's own list of domains and IP addresses /
// ranges blocked for one user. The same list is the "firewall" field of
// the user (GET/PATCH /users/{id}); these endpoints add, remove or replace
// entries without sending the whole user.

// fwEntry is one entry with what it is and where it applies.
type fwEntry struct {
	Value string `json:"value"`
	Kind  string `json:"kind"`
	// AmneziaWG is true when the entry applies on AmneziaWG inbounds too
	// (addresses and ranges; names are matched by Xray and sing-box only).
	AmneziaWG bool `json:"amneziawg"`
}

type fwReply struct {
	UserID  int64     `json:"user_id"`
	Entries []fwEntry `json:"entries"`
	Max     int       `json:"max"`
}

func fwView(id int64, list []string) fwReply {
	out := fwReply{UserID: id, Entries: make([]fwEntry, 0, len(list)), Max: userfw.MaxEntries}
	for _, v := range list {
		k := userfw.Kind(v)
		out.Entries = append(out.Entries, fwEntry{Value: v, Kind: k, AmneziaWG: k == userfw.KindIP})
	}
	return out
}

type fwBody struct {
	Entries []string `json:"entries"`
}

func (s *Server) getUserFirewall(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.Users.GetModel(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, fwView(id, userfw.Split(u.Firewall)))
}

// setFirewall stores list (already merged by the caller) and audits the
// change.
func (s *Server) setFirewall(w http.ResponseWriter, r *http.Request, a *db.Admin, id int64, before, list []string) {
	v, err := s.Users.Update(id, &users.Input{Firewall: util.Opt[[]string]{Set: true, Val: list}}, r.Host)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	s.auditFirewall(r, a, v, before)
	writeJSON(w, 200, fwView(id, v.Firewall))
}

func (s *Server) loadFirewall(w http.ResponseWriter, r *http.Request) (int64, []string, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return 0, nil, false
	}
	u, err := s.Users.GetModel(id)
	if err != nil {
		writeServiceErr(w, err)
		return 0, nil, false
	}
	return id, userfw.Split(u.Firewall), true
}

// putUserFirewall replaces the whole list ([] clears it).
func (s *Server) putUserFirewall(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, before, ok := s.loadFirewall(w, r)
	if !ok {
		return
	}
	var b fwBody
	if !decode(w, r, &b) {
		return
	}
	if b.Entries == nil {
		b.Entries = []string{}
	}
	s.setFirewall(w, r, a, id, before, b.Entries)
}

// addUserFirewall adds entries (ones already on the list are ignored).
func (s *Server) addUserFirewall(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, before, ok := s.loadFirewall(w, r)
	if !ok {
		return
	}
	var b fwBody
	if !decode(w, r, &b) {
		return
	}
	if len(b.Entries) == 0 {
		writeErr(w, http.StatusBadRequest, "validation_error", "entries is required")
		return
	}
	s.setFirewall(w, r, a, id, before, append(append([]string{}, before...), b.Entries...))
}

// deleteUserFirewall removes one entry (?entry=, written in any accepted
// form) or, without it, the whole list.
func (s *Server) deleteUserFirewall(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, before, ok := s.loadFirewall(w, r)
	if !ok {
		return
	}
	list := []string{}
	if e := r.URL.Query().Get("entry"); e != "" {
		v, _, err := userfw.Normalize(e)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		found := false
		for _, x := range before {
			if x == v {
				found = true
				continue
			}
			list = append(list, x)
		}
		if !found {
			writeErr(w, http.StatusNotFound, "not_found", "entry not on the user's firewall list")
			return
		}
	}
	s.setFirewall(w, r, a, id, before, list)
}

// auditFirewall records what changed in a user's firewall list (counts and
// the entries themselves: they are the admin's own input, not secrets).
func (s *Server) auditFirewall(r *http.Request, a *db.Admin, v *users.View, before []string) {
	had := map[string]bool{}
	for _, x := range before {
		had[x] = true
	}
	now := map[string]bool{}
	var added, removed []string
	for _, x := range v.Firewall {
		now[x] = true
		if !had[x] {
			added = append(added, x)
		}
	}
	for _, x := range before {
		if !now[x] {
			removed = append(removed, x)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return
	}
	t := fmt.Sprintf("user:%d %s entries=%d", v.ID, v.Username, len(v.Firewall))
	if len(added) > 0 {
		t += " +" + short(added)
	}
	if len(removed) > 0 {
		t += " -" + short(removed)
	}
	audit.Log(s.DB, a, "user.firewall", t, s.clientIP(r))
}

// short lists up to five entries and how many more there are.
func short(l []string) string {
	const max = 5
	out := ""
	for i, x := range l {
		if i == max {
			return out + fmt.Sprintf(" (+%d more)", len(l)-max)
		}
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}
