package inbounds

import (
	"fmt"
	"sort"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Ports are unique per server, not across the whole panel. The servers of
// an inbound are the main server when it runs there (no nodes, or
// serve_main) plus each of its nodes. Two inbounds conflict only when they
// share a port and at least one server; so 443 on the main server and 443
// on a node-only inbound live side by side, while two inbounds on 443 that
// both run on the main server (or on the same node) do not.

// servers is the set of servers an inbound runs on.
type servers struct {
	main  bool
	nodes map[int64]bool
}

func serversOf(serveMain bool, nodeIDs []int64) servers {
	s := servers{main: serveMain || len(nodeIDs) == 0, nodes: map[int64]bool{}}
	for _, id := range nodeIDs {
		s.nodes[id] = true
	}
	return s
}

// minus returns the servers of s that are not in o.
func (s servers) minus(o servers) servers {
	out := servers{main: s.main && !o.main, nodes: map[int64]bool{}}
	for id := range s.nodes {
		if !o.nodes[id] {
			out.nodes[id] = true
		}
	}
	return out
}

func (s servers) empty() bool { return !s.main && len(s.nodes) == 0 }

// shared returns the first server s and o have in common: the main server
// first, then the lowest node id. ok is false when they share none.
func (s servers) shared(o servers) (main bool, node int64, ok bool) {
	if s.main && o.main {
		return true, 0, true
	}
	ids := make([]int64, 0, len(s.nodes))
	for id := range s.nodes {
		if o.nodes[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return false, 0, false
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return false, ids[0], true
}

// nodeIDs returns the nodes of s in order.
func (s servers) nodeIDs() []int64 {
	ids := make([]int64, 0, len(s.nodes))
	for id := range s.nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// portConflict builds the conflict error for port on the main server (node
// == 0) or on the node with that id. other is the remark of the inbound
// that holds the port ("" for a reserved port).
func (s *Service) portConflict(port int, main bool, node int64, other string) *ConflictError {
	ce := &ConflictError{Field: "port", Other: other, Port: port}
	if main {
		ce.Server = "main"
		ce.Msg = fmt.Sprintf("Port %d is already used on the main server", port)
		return ce
	}
	var n db.Node
	s.DB.Select("id", "name").First(&n, node)
	name := n.Name
	if name == "" {
		name = fmt.Sprintf("#%d", node)
	}
	ce.Server, ce.NodeID, ce.NodeName = "node", node, name
	ce.Msg = fmt.Sprintf("Port %d is already used on node %s", port, name)
	return ce
}

// checkPort rejects port when one of the servers in want already uses it:
// a reserved port of the main server (panel, Xray / sing-box API), a
// node's agent port, or another inbound (not TUN, which has no port) that
// runs on the same server.
func (s *Service) checkPort(id int64, port int, want servers) error {
	if want.empty() {
		return nil
	}
	if want.main && s.ReservedPorts != nil {
		for _, p := range s.ReservedPorts() {
			if p == port {
				return s.portConflict(port, true, 0, "")
			}
		}
	}
	if ids := want.nodeIDs(); len(ids) > 0 {
		var ns []db.Node
		s.DB.Select("id", "name", "api_port").Where("id IN ?", ids).Order("id").Find(&ns)
		for _, n := range ns {
			if n.APIPort == port {
				return s.portConflict(port, false, n.ID, "")
			}
		}
	}
	var rows []db.Inbound
	if err := s.DB.Select("id", "remark", "port", "protocol", "serve_main").
		Where("id <> ? AND port = ? AND protocol <> ?", id, port, TUN).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	placed := Placements(s.DB)
	for _, o := range rows {
		if main, node, ok := want.shared(serversOf(o.ServeMain, placed[o.ID])); ok {
			return s.portConflict(port, main, node, o.Remark)
		}
	}
	return nil
}

// CheckMoveToMain is asked before inbounds that ran only on nodes go back
// to the main server because they lose their last node (the node is
// deleted, or drops them). It rejects the move when an inbound's port is
// taken on the main server: by a reserved port, by an inbound that already
// runs there, or by another inbound moving with it.
func (s *Service) CheckMoveToMain(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	moving := map[int64]bool{}
	for _, id := range ids {
		moving[id] = true
	}
	var ibs []db.Inbound
	if err := s.DB.Select("id", "remark", "port", "protocol").Where("id IN ? AND protocol <> ?", ids, TUN).Order("id").Find(&ibs).Error; err != nil {
		return err
	}
	if len(ibs) == 0 {
		return nil
	}
	var reserved []int
	if s.ReservedPorts != nil {
		reserved = s.ReservedPorts()
	}
	var all []db.Inbound
	if err := s.DB.Select("id", "remark", "port", "protocol", "serve_main").Where("protocol <> ?", TUN).Order("id").Find(&all).Error; err != nil {
		return err
	}
	placed := Placements(s.DB)
	moveErr := func(ib db.Inbound, other string) error {
		ce := &ConflictError{Field: "port", Other: other, Port: ib.Port, Server: "main", MovingID: ib.ID, Moving: ib.Remark}
		if other == "" {
			ce.Msg = fmt.Sprintf("Inbound %q would move back to the main server, but port %d is reserved there; change its port or run it on another node first", ib.Remark, ib.Port)
		} else {
			ce.Msg = fmt.Sprintf("Inbound %q would move back to the main server, but port %d is already used there by inbound %q; change its port or run it on another node first", ib.Remark, ib.Port, other)
		}
		return ce
	}
	for _, ib := range ibs {
		for _, p := range reserved {
			if p == ib.Port {
				return moveErr(ib, "")
			}
		}
		for _, o := range all {
			if o.ID == ib.ID || o.Port != ib.Port {
				continue
			}
			if moving[o.ID] || serversOf(o.ServeMain, placed[o.ID]).main {
				return moveErr(ib, o.Remark)
			}
		}
	}
	return nil
}
