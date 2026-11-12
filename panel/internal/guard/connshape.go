package guard

import (
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// Speed limits are classified per connection, never per client IP: users
// behind one address (two profiles on one phone, a shared NAT, a carrier
// grade NAT) each keep their own limit, and a user with no limit is never
// slowed by one who has a limit.
//
// The access log names the user and the client IP and port of every
// connection. The guard remembers recent connections (connTable) and puts
// those of users with a speed limit into the nft maps of ShapeScript, which
// tag the connection's conntrack entry with the user's mark; the tc tree
// and the upload policers act on that mark only.

// DefaultMarkDelay batches access log connections before one nft run.
const DefaultMarkDelay = 50 * time.Millisecond

// maxConns bounds the remembered connections.
const maxConns = 200000

type connKey struct {
	ip   string
	port int
}

type connSeen struct {
	user int64
	last time.Time
}

// ParseAccessConn is ParseAccessLine plus the client port (0 when the
// line has none).
func ParseAccessConn(line string) (email, ip string, port int, ok bool) {
	email, ip, ok = ParseAccessLine(line)
	if !ok {
		return "", "", 0, false
	}
	return email, ip, accessPort(line), true
}

// accessPort returns the client port of an access log line ("from
// [tcp:]host:port ...").
func accessPort(line string) int {
	i := strings.Index(line, " from ")
	if i < 0 {
		return 0
	}
	src := line[i+len(" from "):]
	if k := strings.IndexByte(src, ' '); k >= 0 {
		src = src[:k]
	}
	return srcPort(strings.TrimPrefix(strings.TrimPrefix(src, "tcp:"), "udp:"))
}

// srcPort returns the port of "host:port" / "[v6]:port" (0 if none).
func srcPort(src string) int {
	_, p, err := net.SplitHostPort(src)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return n
}

func userID(email string) int64 {
	idStr, _, _ := strings.Cut(email, ".")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// ObserveConnLine feeds an Xray access log line to the speed limits only
// (a node: its IP log goes to the master through the IPCollector).
func (g *Guard) ObserveConnLine(line string) {
	if email, ip, port, ok := ParseAccessConn(line); ok {
		if id := userID(email); id > 0 {
			g.observeConn(id, ip, port)
		}
	}
}

// observeConn remembers a connection and, when its user is shaped right
// now, queues it for the nft maps.
func (g *Guard) observeConn(id int64, ip string, port int) {
	if port == 0 {
		return
	}
	now := g.now()
	k := connKey{ip, port}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.conns == nil {
		g.conns = map[connKey]connSeen{}
	}
	if len(g.conns) >= maxConns {
		g.pruneConnsLocked(now)
		if len(g.conns) >= maxConns {
			g.conns = map[connKey]connSeen{}
		}
	}
	prev, had := g.conns[k]
	g.conns[k] = connSeen{user: id, last: now}
	if !g.marked[id] || !g.shapeReady {
		return
	}
	if had && prev.user == id && now.Sub(prev.last) < connElemTTL/2 {
		return // queued moments ago and still in the map
	}
	g.pendingConns = append(g.pendingConns, Conn{UserID: id, IP: ip, Port: port})
	if !g.markTimer {
		g.markTimer = true
		d := g.MarkDelay
		if d <= 0 {
			d = DefaultMarkDelay
		}
		time.AfterFunc(d, func() { g.safe("mark", g.FlushMarks) })
	}
}

// pruneConnsLocked forgets connections not seen for LiveWindow.
func (g *Guard) pruneConnsLocked(now time.Time) {
	for k, c := range g.conns {
		if now.Sub(c.last) > LiveWindow {
			delete(g.conns, k)
		}
	}
}

// recentConnsLocked returns the remembered connections of the users in
// marked.
func (g *Guard) recentConnsLocked(marked map[int64]bool, now time.Time) []Conn {
	var out []Conn
	for k, c := range g.conns {
		if marked[c.user] && now.Sub(c.last) <= LiveWindow {
			out = append(out, Conn{UserID: c.user, IP: k.ip, Port: k.port})
		}
	}
	return out
}

// FlushMarks puts the queued connections into the nft maps.
func (g *Guard) FlushMarks() {
	g.mu.Lock()
	pend := g.pendingConns
	g.pendingConns = nil
	g.markTimer = false
	ready := g.shapeReady
	g.mu.Unlock()
	if len(pend) == 0 || !ready {
		return
	}
	g.nftMu.Lock()
	defer g.nftMu.Unlock()
	if out, err := g.Run(ConnElemScript(pend), "nft", "-f", "-"); err != nil {
		slog.Warn("speed limit: marking connections failed", "err", err, "out", strings.TrimSpace(out))
		// The table may be gone (a firewall reload); rebuild next round.
		g.mu.Lock()
		g.shapeReady, g.tcSig = false, ""
		g.mu.Unlock()
	}
}
