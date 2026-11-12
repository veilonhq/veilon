package guard

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Runner runs a command with optional stdin and returns its combined output.
type Runner func(stdin string, name string, args ...string) (string, error)

// ExecRunner runs real commands. The service has CAP_NET_ADMIN as an
// ambient capability, which nft and tc inherit.
func ExecRunner(stdin string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// --- firewall (concurrent IP limit) ------------------------------------------

// NFTable is the nftables table owned by the guard.
const NFTable = "veilon_guard"

// FirewallScript returns the nft script that (re)creates the guard table:
// timed ban sets for IPv4/IPv6 that drop traffic to the inbound ports only
// (the panel and SSH stay reachable from a banned address).
func FirewallScript(ports []int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\n", NFTable, NFTable)
	fmt.Fprintf(&b, "table inet %s {\n", NFTable)
	b.WriteString("\tset ports { type inet_service; flags interval; }\n")
	b.WriteString("\tset ban4 { type ipv4_addr; flags timeout; }\n")
	b.WriteString("\tset ban6 { type ipv6_addr; flags timeout; }\n")
	b.WriteString("\tchain input {\n\t\ttype filter hook input priority -5; policy accept;\n")
	b.WriteString("\t\tip saddr @ban4 meta l4proto { tcp, udp } th dport @ports drop\n")
	b.WriteString("\t\tip6 saddr @ban6 meta l4proto { tcp, udp } th dport @ports drop\n")
	b.WriteString("\t}\n}\n")
	if len(ports) > 0 {
		fmt.Fprintf(&b, "add element inet %s ports { %s }\n", NFTable, joinInts(ports))
	}
	return b.String()
}

// BanScript returns the nft commands that ban ips for ttl.
//
// Re-adding an element that is already in a timeout set does not reset its
// timer, so each batch adds (a no-op if present), deletes and adds again:
// one atomic transaction that works whether or not the IP is already banned
// and restarts the timeout on every nftables version.
func BanScript(ips []string, ttl time.Duration) string {
	var v4, v6 []string
	for _, s := range ips {
		p := net.ParseIP(s)
		if p == nil {
			continue
		}
		if p.To4() != nil {
			v4 = append(v4, p.To4().String())
		} else {
			v6 = append(v6, p.String())
		}
	}
	secs := int(ttl.Seconds())
	if secs < 1 {
		secs = 1
	}
	var b strings.Builder
	for _, set := range []struct {
		name string
		ips  []string
	}{{"ban4", v4}, {"ban6", v6}} {
		if len(set.ips) == 0 {
			continue
		}
		timed := make([]string, len(set.ips))
		for i, ip := range set.ips {
			timed[i] = fmt.Sprintf("%s timeout %ds", ip, secs)
		}
		fmt.Fprintf(&b, "add element inet %s %s { %s }\n", NFTable, set.name, strings.Join(timed, ", "))
		fmt.Fprintf(&b, "delete element inet %s %s { %s }\n", NFTable, set.name, strings.Join(set.ips, ", "))
		fmt.Fprintf(&b, "add element inet %s %s { %s }\n", NFTable, set.name, strings.Join(timed, ", "))
	}
	return b.String()
}

func joinInts(v []int) string {
	s := append([]int(nil), v...)
	sort.Ints(s)
	out := make([]string, 0, len(s))
	last := -1
	for _, p := range s {
		if p == last || p < 1 || p > 65535 {
			continue
		}
		last = p
		out = append(out, fmt.Sprint(p))
	}
	return strings.Join(out, ", ")
}

// --- traffic control (speed limits) ----------------------------------------------

// Shape is the speed limit of one user. IPs are the user's live client IPs;
// current shaping classifies per connection and ignores them, they are
// still sent so nodes on older agents keep their per-IP shaping.
type Shape struct {
	UserID   int64    `json:"u"`
	DownKbps int64    `json:"down"`
	UpKbps   int64    `json:"up"`
	IPs      []string `json:"ips"`
}

// htbBurst is the HTB burst for a download class: 50 ms at the rate.
func htbBurst(kbps int64) int64 {
	b := kbps * 1000 / 8 / 20
	if b < 16000 {
		b = 16000
	}
	return b
}

// policeBurst is the token bucket of an upload policer: 200 ms at the rate,
// at least 64 KiB. Measured with iperf3: a 50 ms bucket lets TCP reach only
// ~25% of the configured rate (the policer drops whole windows and TCP backs
// off), 200 ms reaches 93-97%.
func policeBurst(kbps int64) int64 {
	b := kbps * 1000 / 8 / 5
	if b < 65536 {
		b = 65536
	}
	return b
}

// htbQuantum is set explicitly so the kernel does not warn that the quantum
// derived from the rate (rate/r2q) is too big; classes never borrow from
// each other (rate == ceil, no parent class), so it only has to be >= MTU.
const htbQuantum = 60000

// TCDeleteScript removes the guard's qdiscs from dev. Run on its own with
// -force: deleting a qdisc that does not exist is an error.
func TCDeleteScript(dev string) string {
	return fmt.Sprintf("qdisc del dev %s root\nqdisc del dev %s ingress\n", dev, dev)
}

// TCScript returns the "tc -batch -" input that builds the download
// shaping tree on dev for plan, after TCDeleteScript removed the old one.
// It returns "" when no user in plan has a download limit.
//
// Root HTB qdisc; unmatched traffic goes to class 1:1 (no limit). Each user
// with a download limit gets class 1:<n> at its rate, fed by one fw filter
// on the user's packet mark (ShapeMark). The mark is set by the nftables
// table of ShapeScript only on the connections of that user (client IP and
// port, learned from the access log), so another user behind the same IP
// never lands in the class. The class is an aggregate over all of the
// user's connections.
func TCScript(dev string, plan []Shape) string {
	var b strings.Builder
	head := false
	for i, s := range plan {
		mark, ok := ShapeMark(s.UserID)
		if !ok || s.DownKbps <= 0 {
			continue
		}
		if !head {
			fmt.Fprintf(&b, "qdisc add dev %s root handle 1: htb default 1\n", dev)
			fmt.Fprintf(&b, "class add dev %s parent 1: classid 1:1 htb rate 10gbit quantum %d\n", dev, htbQuantum)
			fmt.Fprintf(&b, "qdisc add dev %s parent 1:1 fq_codel\n", dev)
			head = true
		}
		n := i + 16 // class minor (1:1 is the default class)
		bb := htbBurst(s.DownKbps)
		fmt.Fprintf(&b, "class add dev %s parent 1: classid 1:%x htb rate %dkbit ceil %dkbit burst %d cburst %d quantum %d\n", dev, n, s.DownKbps, s.DownKbps, bb, bb, htbQuantum)
		fmt.Fprintf(&b, "qdisc add dev %s parent 1:%x fq_codel\n", dev, n)
		fmt.Fprintf(&b, "filter add dev %s parent 1: protocol all prio 1 handle 0x%08x fw flowid 1:%x\n", dev, mark, n)
	}
	return b.String()
}

// --- per-connection marking (speed limits) -----------------------------------

// ShapeTable is the nftables table that marks the connections of users
// with a speed limit and polices their upload.
const ShapeTable = "veilon_shape"

// shapeMarkBase tags the conntrack marks the guard sets (top byte); the
// low 24 bits are the user id.
const (
	shapeMarkBase = 0x5e000000
	shapeMarkMask = 0xff000000
)

// connElemTTL is how long a connection stays in the nft map. It only has
// to outlive the gap until the connection's next packet, which copies the
// mark into the connection's conntrack entry for the rest of its life; a
// short TTL keeps a client port reused by another user (NAT) from
// inheriting the mark.
const connElemTTL = 60 * time.Second

// ShapeMark returns the packet/conntrack mark of a user (ok = false for ids
// that do not fit in 24 bits; those users are not shaped).
func ShapeMark(id int64) (uint32, bool) {
	if id <= 0 || id >= 1<<24 {
		return 0, false
	}
	return shapeMarkBase | uint32(id), true
}

// ShapeScript returns the nft script that (re)creates the marking table
// for plan:
//
//   - maps conn4/conn6 (client IP . client port -> user mark), filled by
//     ConnElemScript from the access log;
//   - a connection with no mark yet takes its mark from the map on any
//     packet (prerouting: source, output: destination) and keeps it in
//     conntrack; connections not in the map stay unmarked (no limit);
//   - output copies the guard's conntrack mark to the packet mark, which
//     the tc fw filters of TCScript classify;
//   - one policing rule per user with an upload limit drops the user's
//     packets above the rate (one rule = one aggregate per user).
func ShapeScript(plan []Shape) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\n", ShapeTable, ShapeTable)
	fmt.Fprintf(&b, "table inet %s {\n", ShapeTable)
	ttl := int(connElemTTL.Seconds())
	fmt.Fprintf(&b, "\tmap conn4 { type ipv4_addr . inet_service : mark; flags timeout; timeout %ds; }\n", ttl)
	fmt.Fprintf(&b, "\tmap conn6 { type ipv6_addr . inet_service : mark; flags timeout; timeout %ds; }\n", ttl)
	b.WriteString("\tchain pre {\n\t\ttype filter hook prerouting priority -150; policy accept;\n")
	for _, l4 := range []string{"tcp", "udp"} {
		fmt.Fprintf(&b, "\t\tct mark 0 meta l4proto %s ct mark set ip saddr . %s sport map @conn4\n", l4, l4)
		fmt.Fprintf(&b, "\t\tct mark 0 meta l4proto %s ct mark set ip6 saddr . %s sport map @conn6\n", l4, l4)
	}
	for _, s := range plan {
		if mark, ok := ShapeMark(s.UserID); ok && s.UpKbps > 0 {
			fmt.Fprintf(&b, "\t\tct mark 0x%08x limit rate over %d bytes/second burst %d bytes drop\n", mark, s.UpKbps*125, policeBurst(s.UpKbps))
		}
	}
	b.WriteString("\t}\n")
	b.WriteString("\tchain out {\n\t\ttype filter hook output priority -150; policy accept;\n")
	for _, l4 := range []string{"tcp", "udp"} {
		fmt.Fprintf(&b, "\t\tct mark 0 meta l4proto %s ct mark set ip daddr . %s dport map @conn4\n", l4, l4)
		fmt.Fprintf(&b, "\t\tct mark 0 meta l4proto %s ct mark set ip6 daddr . %s dport map @conn6\n", l4, l4)
	}
	fmt.Fprintf(&b, "\t\tmeta mark 0 ct mark and 0x%08x == 0x%08x meta mark set ct mark\n", shapeMarkMask, shapeMarkBase)
	b.WriteString("\t}\n}\n")
	return b.String()
}

// ShapeDeleteScript removes the marking table.
func ShapeDeleteScript() string {
	return fmt.Sprintf("add table inet %s\ndelete table inet %s\n", ShapeTable, ShapeTable)
}

// Conn is one client connection of a user (client IP and port).
type Conn struct {
	UserID int64
	IP     string
	Port   int
}

// ConnElemScript returns the nft commands that put conns in the marking
// maps. An existing element is deleted and added again in the same atomic
// batch, because "add" never replaces the mark of a key another user held.
func ConnElemScript(conns []Conn) string {
	var v4, v6 []string // "ip . port"
	var m4, m6 []string // ": mark"
	seen := map[string]bool{}
	for _, c := range conns {
		mark, ok := ShapeMark(c.UserID)
		p := net.ParseIP(c.IP)
		if !ok || p == nil || c.Port < 1 || c.Port > 65535 {
			continue
		}
		var key string
		if p.To4() != nil {
			key = fmt.Sprintf("%s . %d", p.To4(), c.Port)
		} else {
			key = fmt.Sprintf("%s . %d", p, c.Port)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if p.To4() != nil {
			v4, m4 = append(v4, key), append(m4, fmt.Sprintf("0x%08x", mark))
		} else {
			v6, m6 = append(v6, key), append(m6, fmt.Sprintf("0x%08x", mark))
		}
	}
	var b strings.Builder
	for _, set := range []struct {
		name        string
		keys, marks []string
	}{{"conn4", v4, m4}, {"conn6", v6, m6}} {
		if len(set.keys) == 0 {
			continue
		}
		elems := make([]string, len(set.keys))
		for i := range set.keys {
			elems[i] = set.keys[i] + " : " + set.marks[i]
		}
		fmt.Fprintf(&b, "add element inet %s %s { %s }\n", ShapeTable, set.name, strings.Join(elems, ", "))
		fmt.Fprintf(&b, "delete element inet %s %s { %s }\n", ShapeTable, set.name, strings.Join(set.keys, ", "))
		fmt.Fprintf(&b, "add element inet %s %s { %s }\n", ShapeTable, set.name, strings.Join(elems, ", "))
	}
	return b.String()
}

// DefaultInterface returns the interface of the IPv4 default route.
func DefaultInterface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) > 2 && fs[1] == "00000000" && fs[0] != "Iface" {
			return fs[0]
		}
	}
	return ""
}
