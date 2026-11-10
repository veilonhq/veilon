package inbounds

import (
	"fmt"
	"sort"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Placement: an inbound runs on the main server, on chosen nodes, or (for
// inbounds that were assigned to nodes before placement existed) on both.
//
//   - no nodes: the main server runs it (serve_main is ignored);
//   - nodes and serve_main false: only those nodes run it, the main
//     server's cores do not listen on its port and links point at the
//     nodes only;
//   - nodes and serve_main true: the main server and the nodes run it
//     (what every node assignment meant before; kept for existing data).

// Placements returns the node ids of every inbound that has nodes.
func Placements(gdb *gorm.DB) map[int64][]int64 {
	var rows []db.NodeInbound
	gdb.Order("inbound_id, node_id").Find(&rows)
	out := map[int64][]int64{}
	for _, r := range rows {
		out[r.InboundID] = append(out[r.InboundID], r.NodeID)
	}
	return out
}

// Placed returns the inbounds assigned to at least one node (enabled or
// not). Their links always point at their nodes, never at the main server.
func Placed(gdb *gorm.DB) map[int64]bool {
	var ids []int64
	gdb.Model(&db.NodeInbound{}).Distinct("inbound_id").Pluck("inbound_id", &ids)
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// NodeOnly returns the inbounds the main server does not run (assigned to
// at least one node, with serve_main off).
func NodeOnly(gdb *gorm.DB) map[int64]bool {
	var ids []int64
	gdb.Table("inbounds").Where("serve_main = ? AND id IN (?)", false,
		gdb.Table("node_inbounds").Select("inbound_id")).Pluck("id", &ids)
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// checkNodes validates and de-duplicates v.NodeIDs: every node must exist
// and the protocol must be one a node can run.
func (s *Service) checkNodes(v *View) error {
	if v.NodeIDs == nil {
		v.NodeIDs = []int64{}
	}
	seen := map[int64]bool{}
	clean := []int64{}
	for _, id := range v.NodeIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var n int64
		s.DB.Model(&db.Node{}).Where("id = ?", id).Count(&n)
		if n == 0 {
			return verr("node %d does not exist", id)
		}
		clean = append(clean, id)
	}
	if len(clean) > 0 && !NodeCapable(v.Protocol) {
		return verr("%s inbounds cannot run on a node; nodes serve Xray inbounds only", v.Protocol)
	}
	sort.Slice(clean, func(i, j int) bool { return clean[i] < clean[j] })
	v.NodeIDs = clean
	if len(clean) == 0 {
		v.ServeMain = true
	}
	return nil
}

// setNodes replaces the nodes of an inbound.
func setNodes(tx *gorm.DB, inboundID int64, ids []int64) error {
	if err := tx.Where("inbound_id = ?", inboundID).Delete(&db.NodeInbound{}).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := tx.Create(&db.NodeInbound{NodeID: id, InboundID: inboundID}).Error; err != nil {
			return fmt.Errorf("assign node %d: %w", id, err)
		}
	}
	return nil
}
