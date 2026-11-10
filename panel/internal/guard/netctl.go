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

// Shape is the speed limit of one user and its live IPs.
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

// TCScript returns the "tc -batch -" input that builds the shaping tree on
// dev for plan, after TCDeleteScript removed the old one. An empty plan
// returns "".
//
// Download: root HTB qdisc; unmatched traffic goes to class 1:1 (no limit);
// each user gets class 1:<n> at its rate, fed by one u32 filter per live IP
// (destination). Upload: ingress qdisc with one police action per user
// (index n), shared by a u32 filter per live IP (source), dropping above the
// rate. Both are aggregates per user over all of its IPs. The policer's mtu
// is 64 KiB because GRO hands ingress aggregated packets far larger than the
// 2 KiB default, which a policer always treats as exceeding.
func TCScript(dev string, plan []Shape) string {
	var b strings.Builder
	if len(plan) == 0 {
		return ""
	}
	anyDown, anyUp := false, false
	for _, s := range plan {
		anyDown = anyDown || (s.DownKbps > 0 && len(s.IPs) > 0)
		anyUp = anyUp || (s.UpKbps > 0 && len(s.IPs) > 0)
	}
	if anyDown {
		fmt.Fprintf(&b, "qdisc add dev %s root handle 1: htb default 1\n", dev)
		fmt.Fprintf(&b, "class add dev %s parent 1: classid 1:1 htb rate 10gbit quantum %d\n", dev, htbQuantum)
		fmt.Fprintf(&b, "qdisc add dev %s parent 1:1 fq_codel\n", dev)
	}
	if anyUp {
		fmt.Fprintf(&b, "qdisc add dev %s handle ffff: ingress\n", dev)
	}
	for i, s := range plan {
		if len(s.IPs) == 0 {
			continue
		}
		n := i + 16 // class minor / police index (1:1 is the default class)
		if s.DownKbps > 0 {
			bb := htbBurst(s.DownKbps)
			fmt.Fprintf(&b, "class add dev %s parent 1: classid 1:%x htb rate %dkbit ceil %dkbit burst %d cburst %d quantum %d\n", dev, n, s.DownKbps, s.DownKbps, bb, bb, htbQuantum)
			fmt.Fprintf(&b, "qdisc add dev %s parent 1:%x fq_codel\n", dev, n)
			for _, ip := range s.IPs {
				if p := net.ParseIP(ip); p != nil && p.To4() != nil {
					fmt.Fprintf(&b, "filter add dev %s parent 1: protocol ip prio 1 u32 match ip dst %s/32 flowid 1:%x\n", dev, p.To4(), n)
				} else if p != nil {
					fmt.Fprintf(&b, "filter add dev %s parent 1: protocol ipv6 prio 2 u32 match ip6 dst %s/128 flowid 1:%x\n", dev, p, n)
				}
			}
		}
		if s.UpKbps > 0 {
			police := fmt.Sprintf("action police rate %dkbit burst %d mtu 64kb conform-exceed drop/ok index %d", s.UpKbps, policeBurst(s.UpKbps), n)
			first := true
			for _, ip := range s.IPs {
				p := net.ParseIP(ip)
				if p == nil {
					continue
				}
				act := police
				if !first {
					act = fmt.Sprintf("action police index %d", n) // share the user's policer
				}
				first = false
				if p.To4() != nil {
					fmt.Fprintf(&b, "filter add dev %s parent ffff: protocol ip prio 1 u32 match ip src %s/32 %s\n", dev, p.To4(), act)
				} else {
					fmt.Fprintf(&b, "filter add dev %s parent ffff: protocol ipv6 prio 2 u32 match ip6 src %s/128 %s\n", dev, p, act)
				}
			}
		}
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
