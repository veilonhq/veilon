package guard

import (
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/userfw"
)

// Per-user firewall on AmneziaWG.
//
// AmneziaWG runs as a kernel-style tunnel (interface vawg<inbound id>), so
// its traffic never passes Xray's routing. A user's own block list is
// applied there with nftables instead: every peer of a user with a list
// gets one rule in the forward hook that rejects packets from the peer's
// tunnel address to the listed addresses and ranges. Names (domains) cannot
// be matched on raw packets and are left out; the peers' tunnel addresses
// are IPv4, so only IPv4 entries apply. Xray and sing-box apply the whole
// list themselves (package xray, cores).

// UserFWTable is the nftables table of the per-user firewall on AmneziaWG.
const UserFWTable = "veilon_userfw"

// UserFW is one user's address and range entries (prefixes), as the
// master sends them to its nodes.
type UserFW struct {
	UserID int64    `json:"u"`
	CIDRs  []string `json:"cidrs"`
}

// UserFWScript returns the nft script that (re)creates the table for the
// given peers and lists ("" when no peer of a listed user is there): one
// interval set of IPv4 ranges per user, one reject rule per peer.
func UserFWScript(peers []AWGPeer, lists map[int64][]string) string {
	sets := map[int64][]string{}
	for id, l := range lists {
		var v4 []string
		for _, c := range l {
			p, err := netip.ParsePrefix(c)
			if err != nil || !p.Addr().Is4() {
				continue
			}
			v4 = append(v4, p.Masked().String())
		}
		if len(v4) > 0 {
			sort.Strings(v4)
			sets[id] = v4
		}
	}
	type rule struct {
		ifc, addr string
		user      int64
	}
	var rules []rule
	used := map[int64]bool{}
	seen := map[string]bool{}
	for _, p := range peers {
		if _, ok := sets[p.UserID]; !ok || p.UserID <= 0 || !awgIfaceRe.MatchString(p.Iface) {
			continue
		}
		a := AWGPeerAddr(p.Addr)
		if a == "" {
			continue
		}
		k := fmt.Sprintf("%s %s %d", p.Iface, a, p.UserID)
		if seen[k] {
			continue
		}
		seen[k] = true
		used[p.UserID] = true
		rules = append(rules, rule{p.Iface, a, p.UserID})
	}
	if len(rules) == 0 {
		return ""
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].user != rules[j].user {
			return rules[i].user < rules[j].user
		}
		if rules[i].ifc != rules[j].ifc {
			return rules[i].ifc < rules[j].ifc
		}
		return rules[i].addr < rules[j].addr
	})
	ids := make([]int64, 0, len(used))
	for id := range used {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\n", UserFWTable, UserFWTable)
	fmt.Fprintf(&b, "table inet %s {\n", UserFWTable)
	for _, id := range ids {
		fmt.Fprintf(&b, "\tset u%d {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n\t\telements = { %s }\n\t}\n", id, strings.Join(sets[id], ", "))
	}
	b.WriteString("\tchain fwd {\n\t\ttype filter hook forward priority -5; policy accept;\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\tiifname %q ip saddr %s ip daddr @u%d reject\n", r.ifc, r.addr, r.user)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// UserFWDeleteScript removes the table.
func UserFWDeleteScript() string {
	return fmt.Sprintf("add table inet %s\ndelete table inet %s\n", UserFWTable, UserFWTable)
}

// refreshUserFW reloads the lists from the database (main server).
func (g *Guard) refreshUserFW() {
	if g.DB == nil {
		return
	}
	var us []db.User
	g.DB.Select("id", "firewall").Where("firewall <> ?", "").Find(&us)
	lists := make(map[int64][]string, len(us))
	for _, u := range us {
		if c := userfw.CIDRs(u.Firewall); len(c) > 0 {
			lists[u.ID] = c
		}
	}
	g.mu.Lock()
	if !g.fwRemote {
		g.fwLists = lists
	}
	g.mu.Unlock()
}

// UserFirewalls lists the address entries of every user with a list (for
// the nodes; names are matched by the nodes' Xray from its config).
func (g *Guard) UserFirewalls() []UserFW {
	g.mu.Lock()
	out := make([]UserFW, 0, len(g.fwLists))
	for id, l := range g.fwLists {
		out = append(out, UserFW{UserID: id, CIDRs: append([]string(nil), l...)})
	}
	g.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// ApplyUserFirewall sets the lists received from the master (on a node)
// and applies them to this node's AmneziaWG peers.
func (g *Guard) ApplyUserFirewall(list []UserFW) {
	m := make(map[int64][]string, len(list))
	for _, u := range list {
		if u.UserID <= 0 {
			continue
		}
		var ok []string
		for _, c := range u.CIDRs {
			if p := userfw.Prefix(strings.TrimSpace(c)); p != "" {
				ok = append(ok, p)
			}
		}
		if len(ok) > 0 {
			m[u.UserID] = ok
		}
	}
	g.mu.Lock()
	g.fwLists, g.fwRemote = m, true
	g.mu.Unlock()
	g.ApplyUserFW()
}

// ApplyUserFW brings the nftables table in line with the AmneziaWG peers
// and the lists. Cheap when nothing changed.
func (g *Guard) ApplyUserFW() {
	g.mu.Lock()
	script := UserFWScript(g.awgPeers, g.fwLists)
	prev, failed := g.fwSig, g.fwNftFailed
	g.mu.Unlock()
	if script == prev {
		return
	}
	if script != "" {
		if _, err := g.lookPath("nft"); err != nil {
			if !failed {
				slog.Warn("user firewall: nft not found; install nftables to apply firewall lists on AmneziaWG", "err", err)
			}
			g.mu.Lock()
			g.fwNftFailed = true
			g.mu.Unlock()
			return
		}
	}
	g.nftMu.Lock()
	defer g.nftMu.Unlock()
	newSig, ok := script, true
	if script == "" {
		_, _ = g.Run(UserFWDeleteScript(), "nft", "-f", "-")
	} else if out, err := g.Run(script, "nft", "-f", "-"); err != nil {
		if !failed {
			slog.Warn("user firewall: nft failed", "err", err, "out", strings.TrimSpace(out))
		}
		_, _ = g.Run(UserFWDeleteScript(), "nft", "-f", "-")
		newSig, ok = "", false
	}
	g.mu.Lock()
	g.fwSig, g.fwNftFailed = newSig, !ok
	g.mu.Unlock()
	if ok && script != "" {
		slog.Info("user firewall: amneziawg rules updated")
	}
}

// clearUserFW removes the table (on shutdown).
func (g *Guard) clearUserFW() {
	g.mu.Lock()
	had := g.fwSig != ""
	g.fwSig = ""
	g.mu.Unlock()
	if !had {
		return
	}
	g.nftMu.Lock()
	defer g.nftMu.Unlock()
	_, _ = g.Run(UserFWDeleteScript(), "nft", "-f", "-")
}
