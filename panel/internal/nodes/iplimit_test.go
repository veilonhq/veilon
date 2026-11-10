package nodes

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
)

type nftRec struct {
	mu      sync.Mutex
	scripts []string
}

func (r *nftRec) run(stdin, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts = append(r.scripts, stdin)
	return "", nil
}
func (r *nftRec) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.scripts, "\n")
}

func accessLine(ip string, uid int64, name string) string {
	return fmt.Sprintf("2026/11/10 10:00:00.000000 from %s:50000 accepted tcp:example.com:443 [inbound-1 >> direct] email: %d.%s", ip, uid, name)
}

// The IP limit counts a user's IPs on the main server and on every node
// together, drops the newest ones over the limit on the master and on the
// nodes, and a node that goes down neither blocks the master nor keeps
// its IPs counted.
func TestIPLimitUnionAcrossNodes(t *testing.T) {
	r := newRig(t)
	clock := time.Now().UTC()
	var cmu sync.Mutex
	now := func() time.Time { cmu.Lock(); defer cmu.Unlock(); return clock }
	advance := func(d time.Duration) { cmu.Lock(); clock = clock.Add(d); cmu.Unlock() }

	g := guard.New(r.gdb, "")
	mainNft := &nftRec{}
	g.Run, g.Now = mainNft.run, now
	g.LookPath = func(string) (string, error) { return "/usr/sbin/nft", nil }
	g.Limit(1, 2, 0, 0) // alice: 2 IPs
	r.m.OnIPs, r.m.Bans = g.ObserveRemote, g.Bans

	type nodeRig struct {
		n   *db.Node
		a   *Agent
		nft *nftRec
	}
	mk := func(name string) nodeRig {
		n := r.addNode(t, name, 1)
		a, _ := startAgent(t, r, n)
		rec := &nftRec{}
		a.IPs = &guard.IPCollector{Now: now}
		a.Firewall = &guard.NodeFirewall{Run: rec.run, LookPath: func(string) (string, error) { return "/usr/sbin/nft", nil }}
		return nodeRig{n, a, rec}
	}
	n1, n2 := mk("de"), mk("fr")
	ctx := context.Background()
	sync1 := func(nr nodeRig) { r.m.SyncNode(ctx, reload(t, r.gdb, nr.n.ID)) }

	// alice: one IP on the main server, one on each node; bob: one IP on a
	// node (no limit, never dropped).
	g.ObserveLine(accessLine("198.51.100.1", 1, "alice"))
	advance(time.Second)
	n1.a.IPs.ObserveLine(accessLine("198.51.100.2", 1, "alice"))
	n1.a.IPs.ObserveLine(accessLine("203.0.113.9", 2, "bob"))
	advance(time.Second)
	n2.a.IPs.ObserveLine(accessLine("198.51.100.3", 1, "alice"))
	sync1(n1)
	sync1(n2)

	live := g.Tracker.Live(1, now())
	if strings.Join(live, ",") != "198.51.100.1,198.51.100.2,198.51.100.3" {
		t.Fatalf("union of live IPs: %v", live)
	}
	g.Enforce()
	bans := g.Bans()
	if len(bans) != 1 || bans[0].IP != "198.51.100.3" {
		t.Fatalf("bans %+v, want the newest IP only", bans)
	}
	if !strings.Contains(mainNft.all(), "198.51.100.3") {
		t.Fatal("main server did not drop the IP")
	}
	// the next round hands the ban to every node
	sync1(n1)
	sync1(n2)
	for _, nr := range []nodeRig{n1, n2} {
		s := nr.nft.all()
		if !strings.Contains(s, "table inet "+guard.NFTable) || !strings.Contains(s, "198.51.100.3") {
			t.Fatalf("node %s did not drop the IP:\n%s", nr.n.Name, s)
		}
		if strings.Contains(s, "203.0.113.9") || strings.Contains(s, "198.51.100.1") {
			t.Fatalf("node %s dropped an allowed IP:\n%s", nr.n.Name, s)
		}
	}
	// a resent (unacknowledged) batch is not counted twice
	var hits int64
	g.Flush()
	r.gdb.Model(&db.UserIP{}).Where("user_id = 1 AND ip = ?", "198.51.100.2").Select("hits").Scan(&hits)
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}

	// node "fr" goes down: the master's round for it ends quickly, and its
	// IP stops counting once it is no longer live.
	r.m.RequestTimeout = 300 * time.Millisecond
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, p, _ := net.SplitHostPort(l.Addr().String())
	l.Close()
	r.gdb.Model(&db.Node{}).Where("id = ?", n2.n.ID).Update("api_port", p)
	t0 := time.Now()
	sync1(n2)
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("down node blocked for %v", d)
	}
	if st := r.m.Status(n2.n); st.Status != StateOffline {
		t.Fatalf("down node state %+v", st)
	}
	advance(guard.LiveWindow + time.Second)
	g.ObserveLine(accessLine("198.51.100.1", 1, "alice"))
	n1.a.IPs.ObserveLine(accessLine("198.51.100.2", 1, "alice"))
	sync1(n1)
	if live := g.Tracker.Live(1, now()); strings.Join(live, ",") != "198.51.100.1,198.51.100.2" {
		t.Fatalf("live after node down: %v", live)
	}
	if b, _ := g.Plan(); len(b) != 0 {
		t.Fatalf("bans after node down: %v", b)
	}
}

// IPs from a node whose clock is off are placed in the master's time.
func TestObserveRemoteCorrectsClockSkew(t *testing.T) {
	r := newRig(t)
	now := time.Now().UTC()
	g := guard.New(r.gdb, "")
	g.Now = func() time.Time { return now }
	nodeNow := now.Add(-time.Hour) // node clock an hour behind
	g.ObserveRemote([]guard.SeenIP{{UserID: 1, IP: "198.51.100.7", First: nodeNow.Add(-10 * time.Second), Last: nodeNow, Hits: 3}}, time.Hour)
	if live := g.Tracker.Live(1, now); len(live) != 1 {
		t.Fatalf("skewed IP not live: %v", live)
	}
	// a node clock ahead never puts sightings in the future
	g.ObserveRemote([]guard.SeenIP{{UserID: 2, IP: "198.51.100.8", First: now.Add(time.Hour), Last: now.Add(time.Hour), Hits: 1}}, 0)
	if live := g.Tracker.Live(2, now.Add(guard.LiveWindow+time.Second)); len(live) != 0 {
		t.Fatalf("future sighting kept the IP live: %v", live)
	}
}

// An agent older than v0.3.2 (no /v1/bans) keeps syncing normally.
func TestBansToOldAgentDoNotFailTheRound(t *testing.T) {
	r := newRig(t)
	n := r.addNode(t, "old", 1)
	a, _ := startAgent(t, r, n)
	_ = a
	r.m.Bans = func() []guard.Ban { return []guard.Ban{{IP: "198.51.100.3", TTL: 100}} }
	r.m.SyncNode(context.Background(), reload(t, r.gdb, n.ID))
	if st := r.m.Status(n); st.Status != StateOnline {
		t.Fatalf("state %+v", st)
	}
}
