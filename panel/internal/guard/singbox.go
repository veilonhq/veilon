package guard

import (
	"net"
	"strings"
	"sync"
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

// maxPendingSources bounds the connection ids waiting for their user line.
// The two lines are written back to back, so a handful is normal; the map
// is reset when it grows past this (a lost pairing only skips one hit).
const maxPendingSources = 1024

// SingboxParser pairs sing-box "inbound connection from" lines with the
// user line of the same connection. It is safe for concurrent use.
type SingboxParser struct {
	mu      sync.Mutex
	pending map[string]string // connection id -> client IP
}

// NewSingboxParser creates a parser.
func NewSingboxParser() *SingboxParser {
	return &SingboxParser{pending: map[string]string{}}
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
	line = StripANSI(line)
	id, msg, found := splitSingboxLine(line)
	if !found {
		return "", "", false
	}
	for _, pre := range []string{"inbound connection from ", "inbound packet connection from "} {
		if rest, ok := strings.CutPrefix(msg, pre); ok {
			src := clientIP(strings.TrimSpace(rest))
			if src == "" {
				return "", "", false
			}
			p.mu.Lock()
			if len(p.pending) >= maxPendingSources {
				p.pending = map[string]string{}
			}
			p.pending[id] = src
			p.mu.Unlock()
			return "", "", false
		}
	}
	// "[user] inbound connection to dest" / "[user] inbound packet connection to dest"
	if !strings.HasPrefix(msg, "[") {
		return "", "", false
	}
	end := strings.Index(msg, "] ")
	if end < 0 {
		return "", "", false
	}
	user, rest := msg[1:end], msg[end+2:]
	if !strings.HasPrefix(rest, "inbound connection to ") && !strings.HasPrefix(rest, "inbound packet connection to ") {
		return "", "", false
	}
	p.mu.Lock()
	src, have := p.pending[id]
	delete(p.pending, id)
	p.mu.Unlock()
	if !have || user == "" || strings.ContainsAny(user, " \t") {
		return "", "", false
	}
	return user, src, true
}

// splitSingboxLine finds "[<id> <duration>] inbound/<type>[<tag>]: <msg>"
// and returns the connection id and the message.
func splitSingboxLine(line string) (id, msg string, ok bool) {
	k := strings.Index(line, "] inbound/")
	if k < 0 {
		return "", "", false
	}
	open := strings.LastIndexByte(line[:k], '[')
	if open < 0 {
		return "", "", false
	}
	f := strings.Fields(line[open+1 : k])
	if len(f) == 0 {
		return "", "", false
	}
	id = f[0]
	for _, c := range id {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	rest := line[k+len("] inbound/"):]
	c := strings.Index(rest, "]: ")
	if c < 0 {
		return "", "", false
	}
	return id, rest[c+3:], true
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
	if user, ip, ok := p.Line(line); ok {
		g.ObserveEmail(user, ip)
	}
}
