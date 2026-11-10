package inbounds

import (
	"gorm.io/gorm"
)

// NodeTarget is one enabled node that serves an inbound: clients get an
// extra link to Address for it.
type NodeTarget struct {
	ID      int64
	Name    string
	Address string
}

// NodeTargets returns the enabled nodes of each inbound (by node id order).
func NodeTargets(gdb *gorm.DB, inboundIDs []int64) map[int64][]NodeTarget {
	out := map[int64][]NodeTarget{}
	if len(inboundIDs) == 0 {
		return out
	}
	var rows []struct {
		InboundID int64
		ID        int64
		Name      string
		Address   string
	}
	err := gdb.Table("node_inbounds").
		Select("node_inbounds.inbound_id AS inbound_id, nodes.id AS id, nodes.name AS name, nodes.address AS address").
		Joins("JOIN nodes ON nodes.id = node_inbounds.node_id").
		Where("nodes.enabled = ? AND node_inbounds.inbound_id IN ?", true, inboundIDs).
		Order("node_inbounds.inbound_id, nodes.id").Scan(&rows).Error
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.InboundID] = append(out[r.InboundID], NodeTarget{ID: r.ID, Name: r.Name, Address: r.Address})
	}
	return out
}
