package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func (s *Server) listInbounds(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	list, err := s.Inbounds.List()
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) getInbound(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := s.Inbounds.Get(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) createInbound(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	v := inbounds.Defaults()
	if !decode(w, r, &v) {
		return
	}
	out, err := s.Inbounds.Create(&v)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "inbound.create", fmt.Sprintf("inbound:%d %s", out.ID, out.Remark), s.clientIP(r))
	writeJSON(w, 201, out)
}

// updateInbound merges the JSON patch onto the existing inbound (nested
// tls/reality/transport objects merge field by field; null clears them).
func (s *Server) updateInbound(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cur, err := s.Inbounds.Get(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "cannot read body")
		return
	}
	if err := json.Unmarshal(body, cur); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	cur.ID = id
	out, err := s.Inbounds.Update(cur)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "inbound.update", fmt.Sprintf("inbound:%d %s", out.ID, out.Remark), s.clientIP(r))
	writeJSON(w, 200, out)
}

func (s *Server) deleteInbound(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cur, err := s.Inbounds.Get(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := s.Inbounds.Delete(id); err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "inbound.delete", fmt.Sprintf("inbound:%d %s", id, cur.Remark), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) realityKeys(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	k, err := inbounds.GenerateRealityKeys()
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, k)
}

func (s *Server) wireguardKeys(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	priv, pub, err := inbounds.GenerateWGKeys()
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"private_key": priv, "public_key": pub, "awg": inbounds.DefaultAWG()})
}

// inboundHosts lists an inbound's hosts (link overrides) in order.
func (s *Server) inboundHosts(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	hs, err := s.Inbounds.Hosts(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, hs)
}

// setInboundHosts replaces an inbound's hosts ({"hosts": [...]}, in order).
func (s *Server) setInboundHosts(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Hosts []inbounds.HostInput `json:"hosts"`
	}
	if !decode(w, r, &body) {
		return
	}
	hs, err := s.Inbounds.SetHosts(id, body.Hosts)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "inbound.hosts", strconv.FormatInt(id, 10), s.clientIP(r))
	writeJSON(w, 200, hs)
}
