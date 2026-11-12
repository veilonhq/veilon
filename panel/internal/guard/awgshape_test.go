package guard

import (
	"regexp"
	"strings"
	"testing"
)

// awgSim reads the scripts the guard ran and answers how a packet of an
// AmneziaWG peer (interface, tunnel address) is treated.
type awgSim struct {
	down map[string]string // "dev addr" -> classid
	up   map[string]string // "dev addr" -> limit name
	rate map[string]string // limit name -> "N bytes/second"
}

var (
	reAWGFilter = regexp.MustCompile(`^filter add dev (\S+) parent 7a: protocol ip prio 1 u32 match ip dst (\S+)/32 flowid (7a:\S+)$`)
	reAWGRule   = regexp.MustCompile(`^iifname "(\S+)" ip saddr (\S+) limit name "(\S+)" drop$`)
	reAWGLimit  = regexp.MustCompile(`^limit (\S+) \{ rate over (\d+ bytes/second)`)
	reRootDel   = regexp.MustCompile(`^qdisc del dev (\S+) root$`)
)

func simulateAWG(calls []call) *awgSim {
	s := &awgSim{down: map[string]string{}, up: map[string]string{}, rate: map[string]string{}}
	for _, c := range calls {
		for _, line := range strings.Split(c.stdin, "\n") {
			line = strings.TrimSpace(line)
			if m := reRootDel.FindStringSubmatch(line); m != nil {
				for k := range s.down {
					if strings.HasPrefix(k, m[1]+" ") {
						delete(s.down, k)
					}
				}
			}
			if strings.HasPrefix(line, "delete table inet "+AWGShapeTable) {
				s.up, s.rate = map[string]string{}, map[string]string{}
			}
			if m := reAWGFilter.FindStringSubmatch(line); m != nil {
				s.down[m[1]+" "+m[2]] = m[3]
			}
			if m := reAWGRule.FindStringSubmatch(line); m != nil {
				s.up[m[1]+" "+m[2]] = m[3]
			}
			if m := reAWGLimit.FindStringSubmatch(line); m != nil {
				s.rate[m[1]] = m[2]
			}
		}
	}
	return s
}

func awgGuard(calls *[]call, idx map[string]int) *Guard {
	g := recordingGuard(calls, nil)
	g.IfIndex = func(d string) int { return idx[d] }
	return g
}

// Two AmneziaWG users on the same inbound: only the one with a limit is
// shaped (download class and upload policer on its own tunnel address);
// the other has no class and no rule and keeps full speed.
func TestAWGTwoUsersSameInboundOneLimited(t *testing.T) {
	var calls []call
	g := awgGuard(&calls, map[string]int{"vawg2": 7})
	g.Limit(5, 0, 2048, 1024) // alice: 2 Mbps down, 1 Mbps up
	g.SetAWGPeers([]AWGPeer{
		{Iface: "vawg2", Addr: "10.66.66.2/32", UserID: 5}, // alice
		{Iface: "vawg2", Addr: "10.66.66.3/32", UserID: 6}, // bob, no limit
	})
	s := simulateAWG(calls)
	if s.down["vawg2 10.66.66.2"] != "7a:10" {
		t.Fatalf("alice download not shaped: %+v\n%v", s.down, calls)
	}
	if s.up["vawg2 10.66.66.2"] != "up5" || s.rate["up5"] != "128000 bytes/second" {
		t.Fatalf("alice upload not policed: %+v %+v", s.up, s.rate)
	}
	if c, ok := s.down["vawg2 10.66.66.3"]; ok {
		t.Fatalf("bob (no limit) got a download class %s", c)
	}
	if r, ok := s.up["vawg2 10.66.66.3"]; ok {
		t.Fatalf("bob (no limit) got an upload rule %s", r)
	}
	all := ""
	for _, c := range calls {
		all += c.stdin
		if strings.Contains(c.stdin, "10.66.66.3") {
			t.Fatalf("bob's address appears in a script: %s", c.stdin)
		}
		if strings.Contains(c.stdin, "dev eth0") || strings.Contains(c.stdin, ShapeTable+"\n") {
			t.Fatalf("AmneziaWG shaping touched the Xray shaping: %s", c.stdin)
		}
	}
	if !strings.Contains(all, "qdisc add dev vawg2 root handle 7a: htb default 1") {
		t.Fatalf("no AmneziaWG tree:\n%s", all)
	}

	// Unchanged: nothing runs again.
	n := len(calls)
	g.ApplyAWG()
	if len(calls) != n {
		t.Fatalf("unchanged plan re-applied: %v", calls[n:])
	}

	// Limit removed: the tree and the table go away.
	g.Limit(5, 0, 0, 0)
	g.ApplyAWG()
	s = simulateAWG(calls)
	if len(s.down) != 0 || len(s.up) != 0 {
		t.Fatalf("shaping left after the limit was removed: %+v %+v", s.down, s.up)
	}
}

// The interface is shaped once the sidecar creates it, and again after
// it was recreated (new ifindex).
func TestAWGInterfaceAppearsAndIsRecreated(t *testing.T) {
	var calls []call
	idx := map[string]int{}
	g := awgGuard(&calls, idx)
	g.Limit(5, 0, 2048, 0)
	g.SetAWGPeers([]AWGPeer{{Iface: "vawg1", Addr: "10.8.0.2/32", UserID: 5}})
	for _, c := range calls {
		if strings.HasPrefix(c.cmd, "tc") {
			t.Fatalf("tc ran for a missing interface: %v", c)
		}
	}
	idx["vawg1"] = 4
	g.ApplyAWG()
	if s := simulateAWG(calls); s.down["vawg1 10.8.0.2"] == "" {
		t.Fatalf("not shaped once the interface appeared: %v", calls)
	}
	n := len(calls)
	g.ApplyAWG()
	if len(calls) != n {
		t.Fatalf("re-applied without a change")
	}
	idx["vawg1"] = 9 // sidecar restarted: new interface, empty qdisc
	g.ApplyAWG()
	if len(calls) == n || simulateAWG(calls[n:]).down["vawg1 10.8.0.2"] == "" {
		t.Fatalf("recreated interface not shaped again: %v", calls[n:])
	}
}

// A user with peers on two AmneziaWG inbounds shares one upload limit
// across both (one named limit), and each interface has its own tree.
func TestAWGUserOnTwoInbounds(t *testing.T) {
	var calls []call
	g := awgGuard(&calls, map[string]int{"vawg1": 3, "vawg2": 4})
	g.Limit(5, 0, 1000, 500)
	g.Limit(8, 0, 0, 300)
	g.SetAWGPeers([]AWGPeer{
		{Iface: "vawg1", Addr: "10.8.0.2", UserID: 5},
		{Iface: "vawg2", Addr: "10.9.0.2/32", UserID: 5},
		{Iface: "vawg2", Addr: "10.9.0.3/32", UserID: 8},
		{Iface: "vawg2", Addr: "10.9.0.4/32", UserID: 9},       // no limit
		{Iface: "eth0", Addr: "10.9.0.5/32", UserID: 5},        // not an AmneziaWG interface
		{Iface: "vawg2", Addr: "fd00::2/128", UserID: 5},       // not IPv4
		{Iface: "vawg2", Addr: "10.9.0.0/24", UserID: 5},       // not one address
		{Iface: "vawg2", Addr: "10.9.0.6/32; drop", UserID: 5}, // junk
	})
	s := simulateAWG(calls)
	if s.up["vawg1 10.8.0.2"] != "up5" || s.up["vawg2 10.9.0.2"] != "up5" || s.up["vawg2 10.9.0.3"] != "up8" {
		t.Fatalf("upload rules: %+v", s.up)
	}
	if len(s.up) != 3 {
		t.Fatalf("unexpected upload rules: %+v", s.up)
	}
	if s.down["vawg1 10.8.0.2"] == "" || s.down["vawg2 10.9.0.2"] == "" {
		t.Fatalf("download classes: %+v", s.down)
	}
	if _, ok := s.down["vawg2 10.9.0.3"]; ok {
		t.Fatalf("upload-only user got a download class: %+v", s.down)
	}
	if len(s.down) != 2 {
		t.Fatalf("unexpected download classes: %+v", s.down)
	}
	nLimits := 0
	for _, c := range calls {
		nLimits += strings.Count(c.stdin, "\tlimit up5 ")
	}
	if nLimits != 1 {
		t.Fatalf("user 5 should have one shared upload limit, got %d", nLimits)
	}
}

// On a node the limits come from the master and the Xray plan (live IPs)
// does not matter for AmneziaWG; the shutdown removes everything.
func TestAWGNodeLimitsFromMaster(t *testing.T) {
	var calls []call
	g := awgGuard(&calls, map[string]int{"vawg3": 2})
	g.SetAWGPeers([]AWGPeer{{Iface: "vawg3", Addr: "10.7.0.2/32", UserID: 11}, {Iface: "vawg3", Addr: "10.7.0.3/32", UserID: 12}})
	if len(calls) != 0 {
		t.Fatalf("shaped before any limit: %v", calls)
	}
	g.ApplyAWGLimits([]Shape{{UserID: 11, DownKbps: 4096, UpKbps: 2048}})
	s := simulateAWG(calls)
	if s.down["vawg3 10.7.0.2"] == "" || s.up["vawg3 10.7.0.2"] != "up11" || len(s.down) != 1 || len(s.up) != 1 {
		t.Fatalf("node shaping: %+v %+v", s.down, s.up)
	}
	g.ClearShapes()
	s = simulateAWG(calls)
	if len(s.down) != 0 || len(s.up) != 0 {
		t.Fatalf("shutdown left shaping: %+v %+v", s.down, s.up)
	}
}

// Xray shaping and AmneziaWG shaping live side by side: the Xray tree on
// the default interface (handle 1:, fw marks) and the AmneziaWG trees
// (handle 7a:, own devices) never share a class id or a table.
func TestAWGAndXrayShapingCoexist(t *testing.T) {
	var calls []call
	g := awgGuard(&calls, map[string]int{"vawg1": 3})
	g.Limit(5, 0, 2048, 1024)
	g.SetAWGPeers([]AWGPeer{{Iface: "vawg1", Addr: "10.8.0.2/32", UserID: 5}})
	g.ObserveLine("2026/01/01 12:00:00 from 203.0.113.9:40000 accepted tcp:x.com:443 email: 5.alice")
	g.Enforce()
	var xrayTC, awgTC string
	for _, c := range calls {
		if strings.HasPrefix(c.cmd, "tc") && strings.Contains(c.stdin, "dev eth0") {
			xrayTC += c.stdin
		}
		if strings.HasPrefix(c.cmd, "tc") && strings.Contains(c.stdin, "dev vawg1") {
			awgTC += c.stdin
		}
	}
	if !strings.Contains(xrayTC, "classid 1:") || strings.Contains(xrayTC, "7a:") {
		t.Fatalf("xray tree: %s", xrayTC)
	}
	if !strings.Contains(awgTC, "classid 7a:") || strings.Contains(awgTC, "classid 1:") {
		t.Fatalf("awg tree: %s", awgTC)
	}
	// Rebuilding the Xray shaping (a new rate) leaves the AmneziaWG
	// table alone, and the AmneziaWG table never deletes veilon_shape.
	for _, c := range calls {
		if strings.Contains(c.stdin, "delete table inet "+AWGShapeTable) && strings.Contains(c.stdin, "inet "+ShapeTable+"\n") {
			t.Fatalf("one script touches both tables: %s", c.stdin)
		}
	}
}
