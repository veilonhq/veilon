package guard

import (
	"net"
	"strings"
	"sync"
	"time"
)

// sing-box (TUIC) serves its users through Xray's loopback bridge, so Xray
// only ever sees 127.0.0.1 for them. The real client address comes from
// sing-box's own log at level "info", which writes two lines per accepted
// connection (stream), tied together by the connection id in brackets:
//
//	+0000 2026-11-08 18:17:04 INFO [3172046751 0ms] inbound/tuic[inbound-3]: inbound connection from 10.77.0.2:54053
//	+0000 2026-11-08 18:17:04 INFO [3172046751 0ms] inbound/tuic[inbound-3]: [7.alice] inbound connection to 127.0.0.1:8080
//
// UDP relays say "inbound packet connection from/to"; IPv6 sources are
// bracketed ("from [fd77::2]:41666"). sing-box colors its output with ANSI
// escapes even when it is not writing to a terminal; they are stripped.
// Connections that fail authentication never get a user line.
//
// NaiveProxy writes the user on the first line already:
//
//	INFO [3445973830 0ms] inbound/naive[inbound-7]: [7.alice] inbound connection from 10.77.0.2:39998
//
// ShadowTLS hands the stream to its inner Shadowsocks inbound under a new
// connection id, so its "from" line is never followed by a user line with
// the same id:
//
//	INFO [657807017 0ms] inbound/shadowtls[inbound-8]: inbound connection from 10.77.0.2:55150
//	INFO [3403315555 51ms] inbound/shadowtls[inbound-8]: [7.alice] inbound connection to :0
//
// Its sources wait in a queue per inbound instead, and a user line of that
// inbound takes the oldest one (younger than shadowTLSPairAge). A source
// whose id shows up again (an error, a fallback to the handshake server)
// leaves the queue. Handshakes that overlap from different client IPs can
// be paired the wrong way round; this only affects the IP log and the IP
// limit of ShadowTLS users.

// maxPendingSources bounds the connection ids waiting for their user line.
// The two lines are written back to back, so a handful is normal; the map
// is reset when it grows past this (a lost pairing only skips one hit).
const maxPendingSources = 1024

// SingboxParser pairs sing-box "inbound connection from" lines with the
// user line of the same connection. It is safe for concurrent use.
type SingboxParser struct {
	mu      sync.Mutex
	pending map[string]sbSource // connection id -> client IP and port
	// queue: ShadowTLS sources by inbound tag, oldest first.
	queue map[string][]sbQueued
	now   func() time.Time
}

type sbSource struct {
	ip   string
	port int
}

type sbQueued struct {
	id  string
	src sbSource
	at  time.Time
}

// shadowTLSPairAge is how long a ShadowTLS source waits for a user line
// (the handshake with the borrowed server takes a round trip or two).
const shadowTLSPairAge = 15 * time.Second

// maxQueuedSources bounds each ShadowTLS queue.
const maxQueuedSources = 256

// NewSingboxParser creates a parser.
func NewSingboxParser() *SingboxParser {
	return &SingboxParser{pending: map[string]sbSource{}, queue: map[string][]sbQueued{}, now: time.Now}
}

// StripANSI removes ANSI escape sequences (colors) from s.
func StripANSI(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != 0x1b {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			// CSI: ESC [ parameters... final byte in 0x40-0x7e.
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j // skip the final byte too (loop adds 1)
			continue
		}
		i++ // a two-byte escape
	}
	return b.String()
}

// Line parses one sing-box log line. It returns the user name and client
// IP when the line completes a connection whose source was seen before.
func (p *SingboxParser) Line(line string) (user, ip string, ok bool) {
	user, ip, _, ok = p.LineConn(line)
	return user, ip, ok
}

// LineConn is Line plus the client port (0 when the log had none).
func (p *SingboxParser) LineConn(line string) (user, ip string, port int, ok bool) {
	line = StripANSI(line)
	id, kind, tag, msg, found := splitSingboxLine(line)
	if !found {
		return "", "", 0, false
	}
	for _, pre := range []string{"inbound connection from ", "inbound packet connection from "} {
		if rest, ok := strings.CutPrefix(msg, pre); ok {
			rest = strings.TrimSpace(rest)
			src := clientIP(rest)
			if src == "" {
				return "", "", 0, false
			}
			if f := strings.Fields(rest); len(f) > 0 {
				rest = f[0]
			}
			p.mu.Lock()
			if kind == "shadowtls" {
				p.enqueue(tag, id, sbSource{src, srcPort(rest)})
				p.mu.Unlock()
				return "", "", 0, false
			}
			if len(p.pending) >= maxPendingSources {
				p.pending = map[string]sbSource{}
			}
			p.pending[id] = sbSource{src, srcPort(rest)}
			p.mu.Unlock()
			return "", "", 0, false
		}
	}
	// "[user] inbound connection to dest" / "[user] inbound packet connection to dest"
	// (NaiveProxy: "[user] inbound connection from src").
	if !strings.HasPrefix(msg, "[") {
		if kind == "shadowtls" {
			p.mu.Lock()
			p.dropQueued(tag, id)
			p.mu.Unlock()
		}
		return "", "", 0, false
	}
	end := strings.Index(msg, "] ")
	if end < 0 {
		return "", "", 0, false
	}
	user, rest := msg[1:end], msg[end+2:]
	if user == "" || strings.ContainsAny(user, " \t") {
		return "", "", 0, false
	}
	for _, pre := range []string{"inbound connection from ", "inbound packet connection from "} {
		if r, ok := strings.CutPrefix(rest, pre); ok {
			r = strings.TrimSpace(r)
			src := clientIP(r)
			if src == "" {
				return "", "", 0, false
			}
			if f := strings.Fields(r); len(f) > 0 {
				r = f[0]
			}
			return user, src, srcPort(r), true
		}
	}
	if !strings.HasPrefix(rest, "inbound connection to ") && !strings.HasPrefix(rest, "inbound packet connection to ") {
		return "", "", 0, false
	}
	p.mu.Lock()
	src, have := p.pending[id]
	delete(p.pending, id)
	if !have && kind == "shadowtls" {
		src, have = p.dequeue(tag)
	}
	p.mu.Unlock()
	if !have {
		return "", "", 0, false
	}
	return user, src.ip, src.port, true
}

// enqueue adds a ShadowTLS source (p.mu held).
func (p *SingboxParser) enqueue(tag, id string, src sbSource) {
	q := p.prune(tag)
	if len(q) >= maxQueuedSources {
		q = q[1:]
	}
	p.queue[tag] = append(q, sbQueued{id: id, src: src, at: p.now()})
}

// dequeue takes the oldest live ShadowTLS source of tag (p.mu held).
func (p *SingboxParser) dequeue(tag string) (sbSource, bool) {
	q := p.prune(tag)
	if len(q) == 0 {
		return sbSource{}, false
	}
	p.queue[tag] = q[1:]
	if len(q) == 1 {
		delete(p.queue, tag)
	}
	return q[0].src, true
}

// dropQueued removes the source with connection id from tag's queue: that
// connection logged something else (an error, a fallback), so it is not
// the one a later user line belongs to (p.mu held).
func (p *SingboxParser) dropQueued(tag, id string) {
	q := p.queue[tag]
	for i := range q {
		if q[i].id == id {
			p.queue[tag] = append(q[:i:i], q[i+1:]...)
			if len(p.queue[tag]) == 0 {
				delete(p.queue, tag)
			}
			return
		}
	}
}

// prune drops sources older than shadowTLSPairAge (p.mu held).
func (p *SingboxParser) prune(tag string) []sbQueued {
	q := p.queue[tag]
	cut := p.now().Add(-shadowTLSPairAge)
	i := 0
	for i < len(q) && q[i].at.Before(cut) {
		i++
	}
	q = q[i:]
	if len(q) == 0 {
		delete(p.queue, tag)
	} else {
		p.queue[tag] = q
	}
	return q
}

// splitSingboxLine finds "[<id> <duration>] inbound/<type>[<tag>]: <msg>"
// and returns the connection id, the inbound type and tag, and the message.
func splitSingboxLine(line string) (id, kind, tag, msg string, ok bool) {
	k := strings.Index(line, "] inbound/")
	if k < 0 {
		return "", "", "", "", false
	}
	open := strings.LastIndexByte(line[:k], '[')
	if open < 0 {
		return "", "", "", "", false
	}
	f := strings.Fields(line[open+1 : k])
	if len(f) == 0 {
		return "", "", "", "", false
	}
	id = f[0]
	for _, c := range id {
		if c < '0' || c > '9' {
			return "", "", "", "", false
		}
	}
	rest := line[k+len("] inbound/"):]
	c := strings.Index(rest, "]: ")
	if c < 0 {
		return "", "", "", "", false
	}
	head := rest[:c] // "<type>[<tag>"
	if b := strings.IndexByte(head, '['); b >= 0 {
		kind, tag = head[:b], head[b+1:]
	}
	return id, kind, tag, rest[c+3:], true
}

// clientIP turns "1.2.3.4:5678" or "[2001:db8::1]:5678" into a canonical
// IP, rejecting loopback and unspecified addresses.
func clientIP(src string) string {
	if f := strings.Fields(src); len(f) > 0 {
		src = f[0]
	}
	host, _, err := net.SplitHostPort(src)
	if err != nil {
		host = strings.Trim(src, "[]")
	}
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i] // zone
	}
	p := net.ParseIP(host)
	if p == nil || p.IsLoopback() || p.IsUnspecified() {
		return ""
	}
	if v4 := p.To4(); v4 != nil {
		p = v4
	}
	return p.String()
}

// ObserveSingboxLine feeds a sing-box log line to the guard: a completed
// connection is recorded for the user (sing-box user names are the Xray
// emails, "ID.name").
func (g *Guard) ObserveSingboxLine(line string) {
	g.mu.Lock()
	if g.sbParser == nil {
		g.sbParser = NewSingboxParser()
	}
	p := g.sbParser
	g.mu.Unlock()
	if user, ip, port, ok := p.LineConn(line); ok {
		g.ObserveEmail(user, ip)
		if id := userID(user); id > 0 {
			g.observeConn(id, ip, port)
		}
	}
}
