package inbounds

import (
	"fmt"
	"net/netip"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// EnsurePeers returns the WireGuard/AmneziaWG peer of every given user on a
// WG-based inbound, generating keys and allocating tunnel addresses from the
// inbound's subnet for users that have none yet (or whose address left the
// subnet after it was changed).
func EnsurePeers(gdb *gorm.DB, ib *View, userIDs []int64) (map[int64]db.WGPeer, error) {
	out := map[int64]db.WGPeer{}
	if !IsWG(ib.Protocol) || len(userIDs) == 0 {
		return out, nil
	}
	srv, err := netip.ParsePrefix(ib.Settings.Address)
	if err != nil {
		return nil, fmt.Errorf("inbound %d: bad address %q", ib.ID, ib.Settings.Address)
	}
	subnet := srv.Masked()
	err = gdb.Transaction(func(tx *gorm.DB) error {
		var rows []db.WGPeer
		if err := tx.Where("inbound_id = ?", ib.ID).Find(&rows).Error; err != nil {
			return err
		}
		used := map[netip.Addr]bool{srv.Addr(): true}
		have := map[int64]db.WGPeer{}
		for _, r := range rows {
			p, err := netip.ParsePrefix(r.Address)
			if err != nil || !subnet.Contains(p.Addr()) || used[p.Addr()] {
				continue // re-allocate below
			}
			used[p.Addr()] = true
			have[r.UserID] = r
		}
		next := subnet.Addr().Next()
		alloc := func() (netip.Addr, error) {
			for a := next; subnet.Contains(a); a = a.Next() {
				if used[a] || isBroadcast(a, subnet) {
					continue
				}
				used[a] = true
				next = a.Next()
				return a, nil
			}
			return netip.Addr{}, fmt.Errorf("subnet %s of inbound %q is full", subnet, ib.Remark)
		}
		for _, uid := range userIDs {
			if p, ok := have[uid]; ok {
				out[uid] = p
				continue
			}
			a, err := alloc()
			if err != nil {
				return err
			}
			p := db.WGPeer{InboundID: ib.ID, UserID: uid, Address: netip.PrefixFrom(a, 32).String(), CreatedAt: time.Now().UTC()}
			// Keep existing keys when only the address moves.
			var old db.WGPeer
			if tx.Where("inbound_id = ? AND user_id = ?", ib.ID, uid).Limit(1).Find(&old); old.PublicKey != "" {
				p.PrivateKey, p.PublicKey, p.PresharedKey, p.CreatedAt = old.PrivateKey, old.PublicKey, old.PresharedKey, old.CreatedAt
				if err := tx.Model(&db.WGPeer{}).Where("inbound_id = ? AND user_id = ?", ib.ID, uid).Update("address", p.Address).Error; err != nil {
					return err
				}
			} else {
				priv, pub, err := GenerateWGKeys()
				if err != nil {
					return err
				}
				p.PrivateKey, p.PublicKey, p.PresharedKey = priv, pub, GeneratePSK()
				if err := tx.Create(&p).Error; err != nil {
					return err
				}
			}
			out[uid] = p
		}
		return nil
	})
	return out, err
}

func isBroadcast(a netip.Addr, p netip.Prefix) bool {
	if !a.Is4() {
		return false
	}
	b := a.As4()
	m := p.Masked().Addr().As4()
	bits := p.Bits()
	for i := 0; i < 4; i++ {
		for j := 0; j < 8; j++ {
			pos := i*8 + j
			if pos < bits {
				continue
			}
			if b[i]&(1<<(7-j)) == 0 {
				return false
			}
		}
	}
	_ = m
	return true
}
