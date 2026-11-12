// Package guard enforces the per-user connection limits that the proxy
// cores cannot enforce themselves:
//
//   - Client IPs are learned from the Xray access log ("from IP:port
//     accepted ... email: ID.name"). An IP counts as live while it opened a
//     connection within LiveWindow. Every seen IP is stored in user_ips (the
//     connection log) with first/last seen, hits and its country (geoip.dat).
//     TUIC users (sing-box) reach Xray over a loopback bridge, so their
//     client IPs come from sing-box's own connection log instead
//     (ObserveSingboxLine); both feed the same tracker. IPs an admin blocked
//     for a user are refused by the cores and never count as live.
//   - Concurrent IP limit: when a user has more live IPs than allowed, the
//     newest IPs are dropped at the firewall (nftables set with a timeout,
//     matching only the inbound ports) for BanTTL; they are re-banned while
//     the user stays over the limit. The oldest live IPs keep working.
//   - Speed limits: each connection of a limited user (client IP and port
//     from the log) gets the user's conntrack mark (nftables, connshape.go).
//     Download (server -> client) is shaped on the default-route interface
//     with one HTB class per user fed by that mark (aggregate per user);
//     upload (client -> server) is policed by one nftables rule per user on
//     the same mark. Other users behind the same IP are not affected.
//
// The IP limit applies per IP address, so clients behind one NAT address
// share it.
package guard

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// LiveWindow is how recently an IP must have opened a connection to count
// as live for the IP limit and speed shaping.
const LiveWindow = 2 * time.Minute

type seen struct {
	first, last time.Time // first = start of the current live session
	hits        int64     // connections since the last flush
	flushed     bool
}

// Tracker keeps recently seen client IPs per user in memory.
type Tracker struct {
	mu   sync.Mutex
	live map[int64]map[string]*seen
}

// NewTracker creates an empty tracker.
func NewTracker() *Tracker { return &Tracker{live: map[int64]map[string]*seen{}} }

// Observe records a connection from ip for user id at t.
func (t *Tracker) Observe(id int64, ip string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.live[id]
	if m == nil {
		m = map[string]*seen{}
		t.live[id] = m
	}
	s := m[ip]
	if s == nil {
		s = &seen{first: at}
		m[ip] = s
	} else if at.Sub(s.last) > LiveWindow {
		s.first = at // a new live session
	}
	s.last = at
	s.hits++
}

// ObserveSpan records hits connections from ip for user id between first
// and last (a node's report). Unlike Observe it never moves an IP's last
// sighting backwards, so reports that arrive late or twice are harmless.
func (t *Tracker) ObserveSpan(id int64, ip string, first, last time.Time, hits int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.live[id]
	if m == nil {
		m = map[string]*seen{}
		t.live[id] = m
	}
	s := m[ip]
	switch {
	case s == nil:
		s = &seen{first: first, last: last}
		m[ip] = s
	case first.Sub(s.last) > LiveWindow:
		s.first, s.last = first, last // a new live session
	case last.After(s.last):
		s.last = last
	}
	s.hits += hits
}

// Live returns the user's live IPs at now, oldest session first.
func (t *Tracker) Live(id int64, now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	type kv struct {
		ip    string
		first time.Time
	}
	var out []kv
	for ip, s := range t.live[id] {
		if now.Sub(s.last) <= LiveWindow {
			out = append(out, kv{ip, s.first})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].first.Equal(out[j].first) {
			return out[i].ip < out[j].ip
		}
		return out[i].first.Before(out[j].first)
	})
	ips := make([]string, len(out))
	for i := range out {
		ips[i] = out[i].ip
	}
	return ips
}

// Users returns the ids of users with at least one live IP.
func (t *Tracker) Users(now time.Time) []int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ids []int64
	for id, m := range t.live {
		for _, s := range m {
			if now.Sub(s.last) <= LiveWindow {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Pending is one IP to write to the connection log.
type Pending struct {
	UserID      int64
	IP          string
	First, Last time.Time
	Hits        int64
	New         bool // not written before by this process
}

// Drain returns IPs with new connections since the last drain and forgets
// IPs idle for longer than keep.
func (t *Tracker) Drain(now time.Time, keep time.Duration) []Pending {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Pending
	for id, m := range t.live {
		for ip, s := range m {
			if s.hits > 0 {
				out = append(out, Pending{UserID: id, IP: ip, First: s.first, Last: s.last, Hits: s.hits, New: !s.flushed})
				s.hits, s.flushed = 0, true
			} else if now.Sub(s.last) > keep {
				delete(m, ip)
			}
		}
		if len(m) == 0 {
			delete(t.live, id)
		}
	}
	return out
}

// DrainNew returns the IPs never written to the connection log by this
// process (a new user and IP pair), so they appear in the log within
// seconds; IPs already written wait for the next Drain.
func (t *Tracker) DrainNew() []Pending {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Pending
	for id, m := range t.live {
		for ip, s := range m {
			if !s.flushed && s.hits > 0 {
				out = append(out, Pending{UserID: id, IP: ip, First: s.first, Last: s.last, Hits: s.hits, New: true})
				s.hits, s.flushed = 0, true
			}
		}
	}
	return out
}

// ParseAccessLine extracts the client IP and the user email from an Xray
// access log line such as
//
//	2026/11/07 10:00:00.000000 from 1.2.3.4:50000 accepted tcp:example.com:443 [in-1 >> direct] email: 7.alice
//	2026/11/07 10:00:00 from tcp:[2001:db8::1]:50000 accepted udp:1.1.1.1:53 [in-1 -> direct] email: 7.alice
//
// Loopback and unparsable sources are rejected.
func ParseAccessLine(line string) (email, ip string, ok bool) {
	i := strings.Index(line, " from ")
	if i < 0 || !strings.Contains(line, " accepted ") {
		return "", "", false
	}
	j := strings.LastIndex(line, "email: ")
	if j < 0 {
		return "", "", false
	}
	email = strings.TrimSpace(line[j+len("email: "):])
	if f := strings.Fields(email); len(f) > 0 {
		email = f[0]
	}
	src := line[i+len(" from "):]
	if k := strings.IndexByte(src, ' '); k >= 0 {
		src = src[:k]
	}
	src = strings.TrimPrefix(strings.TrimPrefix(src, "tcp:"), "udp:")
	host, _, err := net.SplitHostPort(src)
	if err != nil {
		host = strings.Trim(src, "[]")
	}
	// An IPv6 link-local source carries its zone ("fe80::1%eth0"); the IP
	// itself is what counts.
	if k := strings.IndexByte(host, '%'); k >= 0 {
		host = host[:k]
	}
	p := net.ParseIP(host)
	if p == nil || p.IsLoopback() || p.IsUnspecified() || email == "" {
		return "", "", false
	}
	if v4 := p.To4(); v4 != nil {
		p = v4
	}
	return email, p.String(), true
}
