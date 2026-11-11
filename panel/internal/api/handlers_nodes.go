package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/nodes"
)

func (s *Server) nodesOK(w http.ResponseWriter) bool {
	if s.Nodes == nil {
		writeErr(w, http.StatusNotImplemented, "unsupported", "nodes are not available")
		return false
	}
	return true
}

func writeNodeErr(w http.ResponseWriter, err error) {
	var ve *nodes.ValidationError
	var ce *nodes.ConflictError
	var ue *nodes.UpdateError
	switch {
	case errors.As(err, &ue):
		writeErr(w, ue.Status, ue.Code, ue.Msg)
	case errors.Is(err, nodes.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", err.Error())
	case errors.As(err, &ve):
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
	case errors.As(err, &ce):
		writeErr(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeServiceErr(w, err)
	}
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	vs, err := s.Nodes.List()
	if err != nil {
		writeNodeErr(w, err)
		return
	}
	writeJSON(w, 200, vs)
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	var in nodes.Input
	if !decode(w, r, &in) {
		return
	}
	v, inst, err := s.Nodes.Create(in)
	if err != nil {
		writeNodeErr(w, err)
		return
	}
	audit.Log(s.DB, a, "node.create", v.Name, s.clientIP(r))
	writeJSON(w, 201, map[string]any{"node": v, "install": inst})
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in nodes.Input
	if !decode(w, r, &in) {
		return
	}
	v, err := s.Nodes.Update(id, in)
	if err != nil {
		writeNodeErr(w, err)
		return
	}
	audit.Log(s.DB, a, "node.update", v.Name, s.clientIP(r))
	writeJSON(w, 200, v)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Nodes.Delete(id); err != nil {
		writeNodeErr(w, err)
		return
	}
	audit.Log(s.DB, a, "node.delete", strconv.FormatInt(id, 10), s.clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// nodeInstall issues a new certificate and install command for a node.
func (s *Server) nodeInstall(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	inst, err := s.Nodes.Enroll(id)
	if err != nil {
		writeNodeErr(w, err)
		return
	}
	audit.Log(s.DB, a, "node.install", strconv.FormatInt(id, 10), s.clientIP(r))
	writeJSON(w, 200, inst)
}

// syncNodes asks for an immediate round (returns at once).
func (s *Server) syncNodes(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	s.Nodes.Manager.Kick()
	w.WriteHeader(http.StatusAccepted)
}

// nodeUpdate asks a node to update its agent to the main server's version
// (202: started; the node's state.update follows it).
func (s *Server) nodeUpdate(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := s.Nodes.StartUpdate(r.Context(), id, a, s.clientIP(r))
	if err != nil {
		writeNodeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, v)
}

// nodesUpdateOutdated starts an update on every node whose agent is older
// than the main server and reports each node's answer.
func (s *Server) nodesUpdateOutdated(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	if !s.nodesOK(w) {
		return
	}
	res, err := s.Nodes.Manager.UpdateOutdated(r.Context(), a, s.clientIP(r))
	if err != nil {
		writeNodeErr(w, err)
		return
	}
	started := 0
	for _, x := range res {
		if x.OK {
			started++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"started": started, "results": res})
}
