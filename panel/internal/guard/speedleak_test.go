package guard

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// shapingSim reads the nft and tc scripts the guard ran and answers how a
// packet of one client connection (client IP and port) would be treated:
// the HTB class of its download and whether its upload is policed.
type shapingSim struct {
	ipClass   map[string]string // u32 "match ip dst" -> classid
	ipPoliced map[string]bool   // u32 "match ip src ... police"
	conn      map[string]string // nft conn map "ip . port" -> mark
	markClass map[string]string // tc fw handle -> classid
	markUp    map[string]bool   // nft "ct mark M limit rate over"
}

var (
	reU32Down = regexp.MustCompile(`u32 match ip6? dst (\S+)/\d+ flowid (\S+)`)
	reU32Up   = regexp.MustCompile(`u32 match ip6? src (\S+)/\d+ action police`)
	reElem    = regexp.MustCompile(`^(add|delete) element inet veilon_shape conn[46] \{ (.*) \}$`)
	reFw      = regexp.MustCompile(`handle (0x[0-9a-f]+) fw flowid (\S+)`)
	reUpRule  = regexp.MustCompile(`ct mark (0x[0-9a-f]+) limit rate over`)
)

func simulate(calls []call) *shapingSim {
	s := &shapingSim{ipClass: map[string]string{}, ipPoliced: map[string]bool{}, conn: map[string]string{},
		markClass: map[string]string{}, markUp: map[string]bool{}}
	for _, c := range calls {
		for _, line := range strings.Split(c.stdin, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(c.cmd, "tc") && strings.Contains(line, "qdisc del dev") && strings.HasSuffix(line, "root"):
				s.ipClass, s.markClass = map[string]string{}, map[string]string{}
			case strings.HasPrefix(c.cmd, "tc") && strings.Contains(line, "qdisc del dev") && strings.HasSuffix(line, "ingress"):
				s.ipPoliced = map[string]bool{}
			case strings.HasPrefix(line, "delete table inet veilon_shape"):
				s.conn, s.markUp = map[string]string{}, map[string]bool{}
			}
			if m := reU32Down.FindStringSubmatch(line); m != nil {
				s.ipClass[m[1]] = m[2]
			}
			if m := reU32Up.FindStringSubmatch(line); m != nil {
				s.ipPoliced[m[1]] = true
			}
			if m := reFw.FindStringSubmatch(line); m != nil {
				s.markClass[m[1]] = m[2]
			}
			if m := reUpRule.FindStringSubmatch(line); m != nil {
				s.markUp[m[1]] = true
			}
			if m := reElem.FindStringSubmatch(line); m != nil {
				for _, e := range strings.Split(m[2], ", ") {
					k, v, _ := strings.Cut(e, " : ")
					k = strings.TrimSpace(k)
					if m[1] == "delete" {
						delete(s.conn, k)
					} else if _, ok := s.conn[k]; !ok {
						s.conn[k] = strings.TrimSpace(v) // "add" never replaces
					}
				}
			}
		}
	}
	return s
}

// down returns the download class of a connection ("" = not shaped).
func (s *shapingSim) down(ip, port string) string {
	if c, ok := s.ipClass[ip]; ok {
		return c
	}
	if m, ok := s.conn[ip+" . "+port]; ok {
		return s.markClass[m]
	}
	return ""
}

// up reports whether a connection's upload is policed.
func (s *shapingSim) up(ip, port string) bool {
	if s.ipPoliced[ip] {
		return true
	}
	m, ok := s.conn[ip+" . "+port]
	return ok && s.markUp[m]
}

func accessLine(ip string, port int, email string) string {
	return "2026/11/12 07:00:00.000000 from " + ip + ":" + strconv.Itoa(port) + " accepted tcp:example.com:443 [inbound-1 >> direct] email: " + email
}

// The bug the user reported: a speed limit on user A also slowed user B,
// who has no limit, on the same inbound. Both used the same phone, so their
// connections came from the same client IP, and shaping was keyed by IP.
func TestSpeedLimitDoesNotLeakToUserOnSameIP(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, nil)
	g.MarkDelay = time.Hour   // flushed by hand below
	g.Limit(1, 0, 1000, 1000) // A: 1 Mbps both ways
	// B (id 2) has no limit.
	g.ObserveLine(accessLine("203.0.113.7", 40001, "1.alice"))
	g.ObserveLine(accessLine("203.0.113.7", 40002, "2.bob"))
	g.Enforce()
	g.ObserveLine(accessLine("203.0.113.7", 40003, "1.alice")) // a later connection of A
	g.ObserveLine(accessLine("203.0.113.7", 40004, "2.bob"))   // and of B
	g.FlushMarks()

	s := simulate(calls)
	for _, p := range []string{"40001", "40003"} {
		if s.down("203.0.113.7", p) == "" {
			t.Errorf("A's connection :%s is not download-limited", p)
		}
		if !s.up("203.0.113.7", p) {
			t.Errorf("A's connection :%s is not upload-limited", p)
		}
	}
	for _, p := range []string{"40002", "40004"} {
		if c := s.down("203.0.113.7", p); c != "" {
			t.Errorf("B (no limit) connection :%s is download-shaped in class %s", p, c)
		}
		if s.up("203.0.113.7", p) {
			t.Errorf("B (no limit) connection :%s is upload-policed", p)
		}
	}
}

// Two limited users behind one IP get their own classes and rates.
func TestSpeedLimitsStayPerUserOnSameIP(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, nil)
	g.MarkDelay = time.Hour
	g.Limit(1, 0, 1000, 0)
	g.Limit(2, 0, 5000, 0)
	g.ObserveLine(accessLine("198.51.100.9", 50001, "1.a"))
	g.ObserveLine(accessLine("198.51.100.9", 50002, "2.b"))
	g.Enforce()
	s := simulate(calls)
	ca, cb := s.down("198.51.100.9", "50001"), s.down("198.51.100.9", "50002")
	if ca == "" || cb == "" || ca == cb {
		t.Fatalf("classes A=%q B=%q, want two different classes", ca, cb)
	}
	var tcBuild string
	for _, c := range calls {
		if c.cmd == "tc -batch -" {
			tcBuild = c.stdin
		}
	}
	for _, want := range []string{"classid " + ca + " htb rate 1000kbit", "classid " + cb + " htb rate 5000kbit"} {
		if !strings.Contains(tcBuild, want) {
			t.Errorf("missing %q in\n%s", want, tcBuild)
		}
	}
}
