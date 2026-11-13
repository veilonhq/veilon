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
	"github.com/veilonhq/veilon/panel/internal/sub"
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

// placementKeys applies the placement rule to a decoded create/update body:
// choosing nodes ("node_ids") without saying "serve_main" means the nodes
// only, so the main server stops serving the inbound.
func placementKeys(body []byte, v *inbounds.View) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(body, &keys) != nil {
		return
	}
	_, hasNodes := keys["node_ids"]
	_, hasMain := keys["serve_main"]
	if hasNodes && !hasMain {
		v.ServeMain = len(v.NodeIDs) == 0
	}
}

func (s *Server) createInbound(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	v := inbounds.Defaults()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "cannot read body")
		return
	}
	if err := json.Unmarshal(body, &v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	placementKeys(body, &v)
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
	// "advanced" is replaced whole (header maps would otherwise merge).
	var keys map[string]json.RawMessage
	if json.Unmarshal(body, &keys) == nil {
		if _, ok := keys["advanced"]; ok {
			cur.Advanced = nil
		}
	}
	if err := json.Unmarshal(body, cur); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	cur.ID = id
	placementKeys(body, cur)
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

// vlessEncKeys makes a VLESS Encryption key pair ({"auth": "x25519" or
// "mlkem768"}, default x25519) with the installed Xray-core when it can.
func (s *Server) vlessEncKeys(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Auth string `json:"auth"`
	}
	if b, _ := io.ReadAll(io.LimitReader(r.Body, 4096)); len(b) > 0 {
		if err := json.Unmarshal(b, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
			return
		}
	}
	if req.Auth == "" {
		req.Auth = inbounds.EncAuthX25519
	}
	k, err := inbounds.GenerateVLESSEncKeys(inbounds.XrayBinary, req.Auth)
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

// mldsa65Keys makes a Reality ML-DSA-65 pair (server seed, client verify).
func (s *Server) mldsa65Keys(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	k, err := inbounds.GenerateMLDSA65(inbounds.XrayBinary)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, k)
}

// echKeys makes TLS ECH server keys and the client config list
// ({"server_name": public name}, default cloudflare-ech.com).
func (s *Server) echKeys(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		ServerName string `json:"server_name"`
	}
	if b, _ := io.ReadAll(io.LimitReader(r.Body, 4096)); len(b) > 0 {
		if err := json.Unmarshal(b, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
			return
		}
	}
	k, err := inbounds.GenerateECH(inbounds.XrayBinary, req.ServerName)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, k)
}

// reverseBridge returns the Xray config a reverse-proxy bridge runs
// (?node=<id> picks the node's address; default the first address).
func (s *Server) reverseBridge(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ib, err := s.Inbounds.Get(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if !ib.ReverseOn() {
		writeErr(w, http.StatusConflict, "no_reverse", "the inbound has no reverse proxy")
		return
	}
	ex := sub.Extras{Pins: sub.CertPinsFull(s.DB), Nodes: inbounds.NodeTargets(s.DB, []int64{id}),
		NodeOnly: inbounds.NodeOnly(s.DB), Placed: inbounds.Placed(s.DB)}
	ps, err := sub.ReverseBridgeTargets(ib, db.LoadSettings(s.DB), r.Host, ex)
	if err != nil {
		writeErr(w, http.StatusConflict, "no_reverse", err.Error())
		return
	}
	p := &ps[0]
	if nid, _ := strconv.ParseInt(r.URL.Query().Get("node"), 10, 64); nid > 0 {
		found := false
		for _, n := range ex.Nodes[id] {
			if n.ID != nid {
				continue
			}
			for i := range ps {
				if ps[i].Server == n.Address {
					p, found = &ps[i], true
					break
				}
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, "not_found", "the inbound does not run on that node")
			return
		}
	}
	b, err := sub.ReverseBridgeConfig(p)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="veilon-bridge-%d.json"`, id))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
