package guard

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Per-user IP limits across nodes.
//
// A node reads its own Xray access log (IPCollector) and sends the client
// IPs it saw with every stats batch. The master feeds them into the same
// tracker as its own access log (ObserveRemote), so the IP limit counts the
// union of a user's IPs on the main server and every node, and the existing
// rule applies unchanged: the newest IPs over the limit are dropped for
// BanTTL. The master enforces the drop on its own firewall and sends the
// list of dropped IPs (Bans) to every node, which drops them with the same
// nftables rules (NodeFirewall). A node that is down stops reporting; its
// IPs age out of the tracker after LiveWindow, and it never blocks the
// master or the other nodes.

// SeenIP is a client IP a node saw for a user, in the node's clock.
type SeenIP struct {
	UserID int64     `json:"u"`
	IP     string    `json:"ip"`
	First  time.Time `json:"f"`
	Last   time.Time `json:"l"`
	Hits   int64     `json:"n"`
}

// Ban is an IP dropped for the IP limit, with the time it has left.
type Ban struct {
	IP  string `json:"ip"`
	TTL int64  `json:"ttl"` // seconds
}

// IPCollector gathers client IPs from a node's Xray access log until the
// next batch takes them.
type IPCollector struct {
	mu sync.Mutex
	m  map[string]*SeenIP
	// Now is the clock (tests).
	Now func() time.Time
}

func (c *IPCollector) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// ObserveLine parses one Xray output line.
func (c *IPCollector) ObserveLine(line string) {
	email, ip, ok := ParseAccessLine(line)
	if !ok {
		return
	}
	idStr, _, _ := strings.Cut(email, ".")
	var id int64
	for _, ch := range idStr {
		if ch < '0' || ch > '9' {
			return
		}
		id = id*10 + int64(ch-'0')
	}
	if id <= 0 {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*SeenIP{}
	}
	k := idStr + " " + ip
	s := c.m[k]
	if s == nil {
		if len(c.m) >= 50000 {
			return // a batch is overdue; never grow without bound
		}
		s = &SeenIP{UserID: id, IP: ip, First: now}
		c.m[k] = s
	}
	s.Last = now
	s.Hits++
}

// Drain returns and forgets the IPs seen since the last drain.
func (c *IPCollector) Drain() []SeenIP {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]SeenIP, 0, len(c.m))
	for _, s := range c.m {
		out = append(out, *s)
	}
	c.m = nil
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		return out[i].IP < out[j].IP
	})
	return out
}

// ObserveRemote records IPs a node reported. skew is master time minus node
// time (from the batch's own timestamp), so a node with a wrong clock still
// counts correctly. Times in the future are clamped to now.
func (g *Guard) ObserveRemote(ips []SeenIP, skew time.Duration) {
	now := g.now()
	for _, s := range ips {
		if s.UserID <= 0 || s.IP == "" {
			continue
		}
		first, last := s.First.Add(skew), s.Last.Add(skew)
		if last.After(now) {
			last = now
		}
		if first.After(last) || first.IsZero() {
			first = last
		}
		g.Tracker.ObserveSpan(s.UserID, s.IP, first, last, max(1, s.Hits))
	}
}

// Bans returns the IPs currently dropped for the IP limit and their
// remaining time, for the nodes.
func (g *Guard) Bans() []Ban {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Ban, 0, len(g.banned))
	for ip, until := range g.banned {
		if left := until.Sub(now); left > 0 {
			out = append(out, Ban{IP: ip, TTL: int64(left.Seconds()) + 1})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}

// NodeFirewall drops banned IPs on a node's inbound ports (same nftables
// table and rules as the main server).
type NodeFirewall struct {
	Run Runner
	// LookPath finds nft (tests replace it).
	LookPath func(string) (string, error)

	mu     sync.Mutex
	ports  []int
	ready  bool
	warned bool
	banned map[string]time.Time
	now    func() time.Time
}

func (f *NodeFirewall) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// SetPorts sets the inbound ports to guard (the table is rebuilt, with the
// active bans, on the next Apply).
func (f *NodeFirewall) SetPorts(ports []int) {
	sort.Ints(ports)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !sameInts(ports, f.ports) {
		f.ports = ports
		f.ready = false
	}
}

// Apply bans the given IPs (new ones, or ones whose time was extended).
func (f *NodeFirewall) Apply(bans []Ban) error {
	now := f.clock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.banned == nil {
		f.banned = map[string]time.Time{}
	}
	for ip, until := range f.banned {
		if now.After(until) {
			delete(f.banned, ip)
		}
	}
	type item struct {
		ip  string
		ttl time.Duration
	}
	var fresh []item
	for _, b := range bans {
		if b.TTL <= 0 || b.TTL > int64(BanTTL.Seconds())+60 {
			continue
		}
		ttl := time.Duration(b.TTL) * time.Second
		until := now.Add(ttl)
		if old, ok := f.banned[b.IP]; ok && until.Sub(old) < 30*time.Second {
			continue
		}
		f.banned[b.IP] = until
		fresh = append(fresh, item{b.IP, ttl})
	}
	run := f.Run
	if run == nil {
		run = ExecRunner
	}
	if !f.ready {
		if len(f.banned) == 0 {
			return nil // build lazily, on the first ban
		}
		lp := f.LookPath
		if lp == nil {
			lp = lookPath
		}
		if _, err := lp("nft"); err != nil {
			if !f.warned {
				slog.Warn("ip limit: nft not found on this node; install nftables to enforce IP limits")
				f.warned = true
			}
			return err
		}
		if out, err := run(FirewallScript(f.ports), "nft", "-f", "-"); err != nil {
			slog.Warn("ip limit: nft setup failed", "err", err, "out", strings.TrimSpace(out))
			return err
		}
		f.ready = true
		// the table was (re)built: every active ban goes in again
		fresh = fresh[:0]
		for ip, until := range f.banned {
			fresh = append(fresh, item{ip, until.Sub(now)})
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	// one script per distinct ttl (BanScript takes one ttl)
	byTTL := map[int64][]string{}
	for _, it := range fresh {
		s := int64(it.ttl.Seconds())
		byTTL[s] = append(byTTL[s], it.ip)
	}
	for s, ips := range byTTL {
		if out, err := run(BanScript(ips, time.Duration(s)*time.Second), "nft", "-f", "-"); err != nil {
			slog.Warn("ip limit: ban failed", "err", err, "out", strings.TrimSpace(out))
			for _, ip := range ips {
				delete(f.banned, ip)
			}
			f.ready = false
			return err
		}
	}
	slog.Info("ip limit: dropped client IPs (sent by the panel)", "n", len(fresh))
	return nil
}
