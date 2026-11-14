package guard

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Speed limits for AmneziaWG users.
//
// Every AmneziaWG peer has its own tunnel address on its inbound's
// interface (vawg<inbound id>), so its traffic is told apart by that
// address alone, never by the client's public IP: two users on one
// inbound, or behind one NAT, are always separate.
//
//   - Download (server to client) leaves through the AmneziaWG interface
//     with the peer's tunnel address as destination: an HTB tree on that
//     interface (root handle awgTCMajor, its own handle space, so it never
//     meets the Xray tree on the default interface) has one class per
//     limited user, fed by u32 filters on the user's peer addresses.
//   - Upload (client to server) enters through the AmneziaWG interface with
//     the peer's tunnel address as source: one named nft limit per user in
//     table AWGShapeTable (separate from ShapeTable, so the Xray marking is
//     rebuilt without touching it) polices all of the user's peers as one
//     aggregate.
//
// A user without a limit has no class and no rule: full speed, even on the
// same inbound. The plan is re-checked on every enforcement round; an
// interface that is not there yet (the AmneziaWG sidecar starts it) is
// shaped as soon as it appears, and one that was recreated (new ifindex)
// gets its tree again.

// AWGShapeTable is the nftables table that polices AmneziaWG uploads.
const AWGShapeTable = "veilon_awgshape"

// awgTCMajor is the qdisc handle of the AmneziaWG download trees.
const awgTCMajor = "7a"

// AWGEvery is how often a node re-checks its AmneziaWG shaping (the
// main server does it in every Enforce round).
const AWGEvery = 5 * time.Second

// AWGPeer is one AmneziaWG client: the interface of its inbound, its
// tunnel address (IPv4, no prefix) and its user.
type AWGPeer struct {
	Iface  string `json:"if"`
	Addr   string `json:"ip"`
	UserID int64  `json:"u"`
}

var awgIfaceRe = regexp.MustCompile(`^vawg[0-9]{1,12}$`)

// AWGPeerAddr returns the IPv4 tunnel address of an AmneziaWG allowed IP
// ("10.66.66.2/32" -> "10.66.66.2"; "" when it is not one IPv4 address).
func AWGPeerAddr(allowed string) string {
	s := strings.TrimSpace(allowed)
	if p, err := netip.ParsePrefix(s); err == nil {
		if p.Bits() != 32 || !p.Addr().Is4() {
			return ""
		}
		return p.Addr().String()
	}
	if a, err := netip.ParseAddr(s); err == nil && a.Is4() {
		return a.String()
	}
	return ""
}

// awgUser is one limited user's peers on one interface.
type awgUser struct {
	user     int64
	down, up int64
	addrs    []string
}

// awgPlan groups the peers of users with a speed limit per interface
// (sorted, so the scripts are stable). Peers with a bad interface name or
// address, and users without a limit, are left out.
func awgPlan(peers []AWGPeer, limits map[int64]limit) map[string][]awgUser {
	byIf := map[string]map[int64]*awgUser{}
	for _, p := range peers {
		l, ok := limits[p.UserID]
		if !ok || (l.down <= 0 && l.up <= 0) || p.UserID <= 0 || !awgIfaceRe.MatchString(p.Iface) {
			continue
		}
		a := AWGPeerAddr(p.Addr)
		if a == "" {
			continue
		}
		m := byIf[p.Iface]
		if m == nil {
			m = map[int64]*awgUser{}
			byIf[p.Iface] = m
		}
		u := m[p.UserID]
		if u == nil {
			u = &awgUser{user: p.UserID, down: l.down, up: l.up}
			m[p.UserID] = u
		}
		u.addrs = append(u.addrs, a)
	}
	out := make(map[string][]awgUser, len(byIf))
	for ifc, m := range byIf {
		list := make([]awgUser, 0, len(m))
		for _, u := range m {
			sort.Strings(u.addrs)
			list = append(list, *u)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].user < list[j].user })
		out[ifc] = list
	}
	return out
}

// AWGTCScript returns the "tc -batch -" input that builds the download
// tree of one AmneziaWG interface ("" when no user on it has a download
// limit). Run TCRootDeleteScript first.
func AWGTCScript(dev string, users []awgUser) string {
	var b strings.Builder
	n := 0x10
	for _, u := range users {
		if u.down <= 0 || len(u.addrs) == 0 {
			continue
		}
		if b.Len() == 0 {
			fmt.Fprintf(&b, "qdisc add dev %s root handle %s: htb default 1\n", dev, awgTCMajor)
			fmt.Fprintf(&b, "class add dev %s parent %s: classid %s:1 htb rate 10gbit quantum %d\n", dev, awgTCMajor, awgTCMajor, htbQuantum)
			fmt.Fprintf(&b, "qdisc add dev %s parent %s:1 fq_codel\n", dev, awgTCMajor)
		}
		bb := htbBurst(u.down)
		fmt.Fprintf(&b, "class add dev %s parent %s: classid %s:%x htb rate %dkbit ceil %dkbit burst %d cburst %d quantum %d\n",
			dev, awgTCMajor, awgTCMajor, n, u.down, u.down, bb, bb, htbQuantum)
		fmt.Fprintf(&b, "qdisc add dev %s parent %s:%x fq_codel\n", dev, awgTCMajor, n)
		for _, a := range u.addrs {
			fmt.Fprintf(&b, "filter add dev %s parent %s: protocol ip prio 1 u32 match ip dst %s/32 flowid %s:%x\n", dev, awgTCMajor, a, awgTCMajor, n)
		}
		n++
	}
	return b.String()
}

// TCRootDeleteScript removes the root qdisc of dev (run with -force).
func TCRootDeleteScript(dev string) string { return fmt.Sprintf("qdisc del dev %s root\n", dev) }

// AWGShapeScript returns the nft script that (re)creates the AmneziaWG
// upload table for plan ("" when no user has an upload limit): one named
// limit per user, shared by all of the user's peers on every interface.
func AWGShapeScript(plan map[string][]awgUser) string {
	ifs := make([]string, 0, len(plan))
	for ifc := range plan {
		ifs = append(ifs, ifc)
	}
	sort.Strings(ifs)
	rates := map[int64]int64{}
	var rules []string
	for _, ifc := range ifs {
		for _, u := range plan[ifc] {
			if u.up <= 0 {
				continue
			}
			rates[u.user] = u.up
			for _, a := range u.addrs {
				rules = append(rules, fmt.Sprintf("\t\tiifname %q ip saddr %s limit name \"up%d\" drop\n", ifc, a, u.user))
			}
		}
	}
	if len(rules) == 0 {
		return ""
	}
	ids := make([]int64, 0, len(rates))
	for id := range rates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\n", AWGShapeTable, AWGShapeTable)
	fmt.Fprintf(&b, "table inet %s {\n", AWGShapeTable)
	for _, id := range ids {
		fmt.Fprintf(&b, "\tlimit up%d { rate over %d bytes/second burst %d bytes; }\n", id, rates[id]*125, policeBurst(rates[id]))
	}
	b.WriteString("\tchain pre {\n\t\ttype filter hook prerouting priority -150; policy accept;\n")
	for _, r := range rules {
		b.WriteString(r)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// AWGShapeDeleteScript removes the AmneziaWG upload table.
func AWGShapeDeleteScript() string {
	return fmt.Sprintf("add table inet %s\ndelete table inet %s\n", AWGShapeTable, AWGShapeTable)
}

// ifIndex returns the ifindex of dev (0 when it does not exist).
func ifIndex(dev string) int {
	b, err := os.ReadFile("/sys/class/net/" + dev + "/ifindex")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (g *Guard) ifIndex(dev string) int {
	if g.IfIndex != nil {
		return g.IfIndex(dev)
	}
	return ifIndex(dev)
}

// SetAWGPeers replaces the AmneziaWG peers this server runs (from the
// AmneziaWG engine after each plan) and applies the shaping.
func (g *Guard) SetAWGPeers(peers []AWGPeer) {
	g.mu.Lock()
	g.awgPeers = append([]AWGPeer(nil), peers...)
	g.mu.Unlock()
	g.safe("awg shape", g.ApplyAWG)
	g.safe("user firewall", g.ApplyUserFW)
}

// SpeedLimits lists every user with a speed limit (no IPs), for nodes that
// shape AmneziaWG by tunnel address: those limits do not depend on the
// user's live client IPs.
func (g *Guard) SpeedLimits() []Shape {
	g.mu.Lock()
	out := make([]Shape, 0, len(g.limits))
	for id, l := range g.limits {
		if l.down > 0 || l.up > 0 {
			out = append(out, Shape{UserID: id, DownKbps: l.down, UpKbps: l.up})
		}
	}
	g.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// ApplyAWGLimits sets the speed limits received from the master (on a
// node) and applies the AmneziaWG shaping.
func (g *Guard) ApplyAWGLimits(list []Shape) {
	m := make(map[int64]limit, len(list))
	for _, s := range list {
		if s.UserID > 0 && (s.DownKbps > 0 || s.UpKbps > 0) {
			m[s.UserID] = limit{down: s.DownKbps, up: s.UpKbps}
		}
	}
	g.mu.Lock()
	g.awgLimits, g.awgRemote = m, true
	g.mu.Unlock()
	g.ApplyAWG()
}

// RunAWGLoop re-checks the AmneziaWG shaping every AWGEvery until ctx ends
// (a node: the sidecar can recreate its interfaces at any time), then
// removes it.
func (g *Guard) RunAWGLoop(ctx context.Context) {
	t := time.NewTicker(AWGEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			g.safe("awg clear", g.clearAWG)
			g.safe("user firewall clear", g.clearUserFW)
			return
		case <-t.C:
			g.safe("awg shape", g.ApplyAWG)
			g.safe("user firewall", g.ApplyUserFW)
		}
	}
}

// ApplyAWG brings the AmneziaWG shaping in line with the peers and limits.
// Cheap when nothing changed (it reads the interfaces' ifindex only).
func (g *Guard) ApplyAWG() {
	g.mu.Lock()
	lim := g.limits
	if g.awgRemote {
		lim = g.awgLimits
	}
	plan := awgPlan(g.awgPeers, lim)
	prevNft, failed := g.awgNftSig, g.awgFailed
	applied := make(map[string]string, len(g.awgTC))
	for k, v := range g.awgTC {
		applied[k] = v
	}
	g.mu.Unlock()

	nftScript := AWGShapeScript(plan)
	type tcJob struct{ dev, sig, script string }
	var jobs []tcJob
	devs := map[string]bool{}
	for d := range applied {
		devs[d] = true
	}
	for d, users := range plan {
		for _, u := range users {
			if u.down > 0 {
				devs[d] = true
				break
			}
		}
	}
	gone := map[string]bool{}
	for d := range devs {
		script := ""
		if users, ok := plan[d]; ok {
			script = AWGTCScript(d, users)
		}
		idx := g.ifIndex(d)
		if idx == 0 {
			gone[d] = true // not there (yet); nothing to shape or remove
			continue
		}
		sig := ""
		if script != "" {
			sig = strconv.Itoa(idx) + "\n" + script
		}
		if sig == applied[d] {
			continue
		}
		jobs = append(jobs, tcJob{d, sig, script})
	}
	if nftScript == prevNft && len(jobs) == 0 && len(gone) == 0 {
		return
	}
	needTools := (nftScript != "" && nftScript != prevNft) || len(jobs) > 0
	if needTools {
		for _, bin := range []string{"tc", "nft"} {
			if _, err := g.lookPath(bin); err != nil {
				if !failed {
					slog.Warn("amneziawg speed limit: "+bin+" not found; install iproute2 and nftables to enforce speed limits", "err", err)
				}
				g.mu.Lock()
				g.awgFailed = true
				g.mu.Unlock()
				return
			}
		}
	}
	g.nftMu.Lock()
	defer g.nftMu.Unlock()
	ok := true
	newNft := prevNft
	if nftScript != prevNft {
		if nftScript == "" {
			_, _ = g.Run(AWGShapeDeleteScript(), "nft", "-f", "-")
			newNft = ""
		} else if out, err := g.Run(nftScript, "nft", "-f", "-"); err != nil {
			if !failed {
				slog.Warn("amneziawg speed limit: nft failed", "err", err, "out", strings.TrimSpace(out))
			}
			_, _ = g.Run(AWGShapeDeleteScript(), "nft", "-f", "-")
			newNft, ok = "", false
		} else {
			newNft = nftScript
		}
	}
	done := map[string]string{}
	for _, j := range jobs {
		_, _ = g.Run(TCRootDeleteScript(j.dev), "tc", "-force", "-batch", "-")
		if j.script == "" {
			done[j.dev] = ""
			continue
		}
		if out, err := g.Run(j.script, "tc", "-batch", "-"); err != nil {
			if !failed {
				slog.Warn("amneziawg speed limit: tc failed", "dev", j.dev, "err", err, "out", strings.TrimSpace(out))
			}
			_, _ = g.Run(TCRootDeleteScript(j.dev), "tc", "-force", "-batch", "-")
			done[j.dev], ok = "", false
			continue
		}
		done[j.dev] = j.sig
	}
	g.mu.Lock()
	if g.awgTC == nil {
		g.awgTC = map[string]string{}
	}
	for d := range gone {
		delete(g.awgTC, d)
	}
	for d, s := range done {
		if s == "" {
			delete(g.awgTC, d)
		} else {
			g.awgTC[d] = s
		}
	}
	g.awgNftSig = newNft
	g.awgFailed = !ok
	g.mu.Unlock()
	if ok && needTools {
		slog.Info("amneziawg speed limit: shaping updated", "interfaces", len(g.awgTC))
	}
}

// clearAWG removes the AmneziaWG shaping (on shutdown).
func (g *Guard) clearAWG() {
	g.mu.Lock()
	nft := g.awgNftSig != ""
	devs := make([]string, 0, len(g.awgTC))
	for d := range g.awgTC {
		devs = append(devs, d)
	}
	g.awgTC, g.awgNftSig = nil, ""
	g.mu.Unlock()
	if len(devs) == 0 && !nft {
		return
	}
	g.nftMu.Lock()
	defer g.nftMu.Unlock()
	for _, d := range devs {
		_, _ = g.Run(TCRootDeleteScript(d), "tc", "-force", "-batch", "-")
	}
	if nft {
		_, _ = g.Run(AWGShapeDeleteScript(), "nft", "-f", "-")
	}
}
