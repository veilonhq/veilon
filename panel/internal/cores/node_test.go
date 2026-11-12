package cores

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/awgd"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// nodeDB has a VLESS, an AmneziaWG, an MTProto and an MTProto inbound
// without users, and users alice (7) and bob (8) on all but the last.
func nodeDB(t *testing.T) (*gorm.DB, map[string]int64) {
	t.Helper()
	gdb := testutil.DB(t)
	svc := &inbounds.Service{DB: gdb}
	ids := map[string]int64{}
	for i, p := range []string{inbounds.VLESS, inbounds.AmneziaWG, inbounds.MTProto, "mt-empty"} {
		proto := p
		if p == "mt-empty" {
			proto = inbounds.MTProto
		}
		v := inbounds.Defaults()
		v.Remark, v.Protocol, v.Port = p, proto, 32000+i
		out, err := svc.Create(&v)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		ids[p] = out.ID
	}
	now := time.Now().UTC()
	for _, u := range []db.User{{ID: 7, Username: "alice", Password: "pw7"}, {ID: 8, Username: "bob", Password: "pw8"}} {
		u.Status, u.UUID, u.SubToken, u.CreatedAt, u.UpdatedAt = db.StatusActive, fmt.Sprintf("1111111%d-2222-4333-8444-555555555555", u.ID), "t"+u.Username, now, now
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{inbounds.VLESS, inbounds.AmneziaWG, inbounds.MTProto} {
			gdb.Create(&db.UserInbound{UserID: u.ID, InboundID: ids[p]})
		}
	}
	return gdb, ids
}

func TestBuildNodeSpec(t *testing.T) {
	gdb, ids := nodeDB(t)
	all := []int64{ids[inbounds.VLESS], ids[inbounds.AmneziaWG], ids[inbounds.MTProto], ids["mt-empty"]}
	spec, err := BuildNodeSpec(gdb, t.TempDir(), all)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.AWG) != 1 || len(spec.MTG) != 1 {
		t.Fatalf("want 1 awg + 1 mtg (vless and the mtproto inbound without users left out), got %+v", spec)
	}
	a := spec.AWG[0]
	if a.InboundID != ids[inbounds.AmneziaWG] || a.Iface.Name != AWGIfaceName(a.InboundID) || a.Iface.ListenPort != 32001 || len(a.Iface.Peers) != 2 {
		t.Fatalf("awg %+v", a)
	}
	owned := map[int64]bool{}
	for _, p := range a.Iface.Peers {
		owned[a.Owners[p.PublicKey]] = true
	}
	if !owned[7] || !owned[8] {
		t.Fatalf("owners %v", a.Owners)
	}
	m := spec.MTG[0]
	if m.InboundID != ids[inbounds.MTProto] || m.Port != 32002 || len(m.Secrets) != 2 || m.Secrets[0].Name != "7.alice" {
		t.Fatalf("mtg %+v", m)
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := spec.Ports(); len(got) != 2 || got[0] != 32001 || got[1] != 32002 {
		t.Fatalf("ports %v", got)
	}
	// Only the inbounds asked for.
	spec, _ = BuildNodeSpec(gdb, t.TempDir(), []int64{ids[inbounds.VLESS]})
	if !spec.Empty() {
		t.Fatalf("vless only: %+v", spec)
	}
	// The same users and keys as the main server renders.
	ps, owners, err := mainAWG(t, gdb)
	if err != nil || len(ps) != 1 {
		t.Fatal(ps, err)
	}
	var c awgd.Config
	_ = json.Unmarshal(ps[0].Config, &c)
	for _, p := range c.Interfaces[0].Peers {
		if owners[ps[0].Key+"/"+p.PublicKey] != a.Owners[p.PublicKey] {
			t.Fatalf("node and main disagree on peer owners")
		}
	}
}

func mainAWG(t *testing.T, gdb *gorm.DB) ([]Instance, map[string]int64, error) {
	t.Helper()
	e := NewAWG(Options{DB: gdb, DataDir: t.TempDir()}, "/bin/true")
	ps, err := e.spec.Plan()
	e.mu.Lock()
	defer e.mu.Unlock()
	return ps, e.owners, err
}

// The main server's AmneziaWG and MTProto engines leave out inbounds that
// run on nodes only, and keep the ones that also run on the main server.
func TestMainEnginesSkipNodeOnlyInbounds(t *testing.T) {
	gdb, ids := nodeDB(t)
	mtg := NewMTG(Options{DB: gdb, DataDir: t.TempDir(), BinPath: "/bin/true"})
	count := func() (int, int) {
		a, _, err := mainAWG(t, gdb)
		if err != nil {
			t.Fatal(err)
		}
		m, err := mtg.plan()
		if err != nil {
			t.Fatal(err)
		}
		return len(a), len(m)
	}
	if a, m := count(); a != 1 || m != 1 {
		t.Fatalf("before: awg %d mtg %d", a, m)
	}
	n := db.Node{Name: "n1", Address: "10.0.0.2", APIPort: 62050, Enabled: true}
	gdb.Create(&n)
	for _, p := range []string{inbounds.AmneziaWG, inbounds.MTProto} {
		gdb.Create(&db.NodeInbound{NodeID: n.ID, InboundID: ids[p]})
	}
	// Placed before placement existed (serve_main true): main keeps them.
	if a, m := count(); a != 1 || m != 1 {
		t.Fatalf("serve_main: awg %d mtg %d", a, m)
	}
	gdb.Model(&db.Inbound{}).Where("id IN ?", []int64{ids[inbounds.AmneziaWG], ids[inbounds.MTProto]}).Update("serve_main", false)
	if a, m := count(); a != 0 || m != 0 {
		t.Fatalf("node only: awg %d mtg %d", a, m)
	}
}

func TestNodeSpecValidate(t *testing.T) {
	_, pub, _ := inbounds.GenerateWGKeys()
	priv, _, _ := inbounds.GenerateWGKeys()
	good := NodeAWG{InboundID: 3, Iface: awgd.Iface{Name: "vawg3", ListenPort: 51820, PrivateKey: priv, Address: "10.66.0.1/24", MTU: 1280,
		Peers: []awgd.Peer{{PublicKey: pub, AllowedIP: "10.66.0.2/32"}}}}
	for name, s := range map[string]NodeSpec{
		"iface name":  {AWG: []NodeAWG{{InboundID: 3, Iface: awgd.Iface{Name: "eth0", ListenPort: 1, PrivateKey: priv}}}},
		"awg port":    {AWG: []NodeAWG{{InboundID: 3, Iface: awgd.Iface{Name: "vawg3", ListenPort: 0, PrivateKey: priv}}}},
		"awg key":     {AWG: []NodeAWG{{InboundID: 3, Iface: awgd.Iface{Name: "vawg3", ListenPort: 5, PrivateKey: "x"}}}},
		"mtg port":    {MTG: []NodeMTG{{InboundID: 4, Port: 70000, Secrets: []MTGSecret{{"7.a", "ee00"}}}}},
		"mtg users":   {MTG: []NodeMTG{{InboundID: 4, Port: 443}}},
		"mtg listen":  {MTG: []NodeMTG{{InboundID: 4, Port: 443, Listen: "a;b", Secrets: []MTGSecret{{"7.a", "ee00"}}}}},
		"double port": {AWG: []NodeAWG{good}, MTG: []NodeMTG{{InboundID: 4, Port: 51820, Secrets: []MTGSecret{{"7.a", "ee00"}}}}},
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok := NodeSpec{AWG: []NodeAWG{good}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}

type seenIPs struct {
	mu  sync.Mutex
	ips []string
}

func (s *seenIPs) add(id int64, ip string) {
	s.mu.Lock()
	s.ips = append(s.ips, strconv.FormatInt(id, 10)+" "+ip)
	s.mu.Unlock()
}

func (s *seenIPs) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.ips
	s.ips = nil
	sort.Strings(out)
	return out
}

// A node's AmneziaWG sidecar reports counters and endpoints: traffic goes
// to the peers' owners, and an endpoint counts as a client IP only while
// its peer sends traffic.
func TestNodeEnginesAWGTrafficAndIPs(t *testing.T) {
	var got [][2]int64
	var mu sync.Mutex
	seen := &seenIPs{}
	e := &NodeEngines{Dir: t.TempDir(), Self: "/bin/true", SeenIP: seen.add,
		Traffic: func(u map[int64][2]int64, up, down int64) {
			mu.Lock()
			got = append(got, u[7], u[8], [2]int64{up, down})
			mu.Unlock()
		}}
	e.Set(NodeSpec{AWG: []NodeAWG{{InboundID: 3, Iface: awgd.Iface{Name: "vawg3"}, Owners: map[string]int64{"PKA": 7, "PKB": 8}}}})
	if _, err := e.awgPlan(); err != nil {
		t.Fatal(err)
	}
	line := func(s string) {
		if !e.awgLine("vawg3", 1, s) {
			t.Fatalf("line not consumed: %s", s)
		}
	}
	line(awgd.StatsPrefix + `{"vawg3":{"PKA":[1000,5000],"PKB":[0,0]}}`)
	line(awgd.PeersPrefix + `{"vawg3":{"PKA":"203.0.113.7:51000","PKB":"198.51.100.9:4000"}}`)
	if e.awgLine("vawg3", 1, "interface vawg3 listening") {
		t.Fatal("ordinary line consumed")
	}
	e.awgCollect(context.Background(), nil)
	line(awgd.StatsPrefix + `{"vawg3":{"PKA":[1500,9000],"PKB":[0,0]}}`)
	e.awgCollect(context.Background(), nil)
	e.awgCollect(context.Background(), nil) // nothing new: no traffic, no IP
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 6 || got[0] != [2]int64{1000, 5000} || got[1] != [2]int64{} || got[3] != [2]int64{500, 4000} || got[5] != [2]int64{500, 4000} {
		t.Fatalf("traffic %v", got)
	}
	if ips := seen.take(); len(ips) != 2 || ips[0] != "7 203.0.113.7" || ips[1] != "7 203.0.113.7" {
		t.Fatalf("ips %v (bob sent nothing, so his old endpoint is not an IP)", ips)
	}
	// The sidecar is told to print endpoints on the node only.
	ps, _ := e.awgPlan()
	if len(ps) != 1 || strings.Join(ps[0].Env, " ") != awgd.EndpointsEnv+"=1" || ps[0].Args[0] != "awg-run" {
		t.Fatalf("instance %+v", ps)
	}
}

func TestNodeEnginesMTGTrafficAndIPs(t *testing.T) {
	var mu sync.Mutex
	in := int64(100)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(w, `{"users":{"7.alice":{"bytes_in":%d,"bytes_out":%d,"active_ips":["203.0.113.7","2001:db8::1","bad"]},"8.bob":{"bytes_in":0,"bytes_out":0,"active_ips":[]}}}`, in, in*3)
	}))
	defer ts.Close()
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	seen := &seenIPs{}
	var traffic []map[int64][2]int64
	e := &NodeEngines{Dir: t.TempDir(), SeenIP: seen.add, Traffic: func(u map[int64][2]int64, _, _ int64) { traffic = append(traffic, u) }}
	e.Set(NodeSpec{MTG: []NodeMTG{{InboundID: 4, Port: 443, Domain: "www.cloudflare.com", Secrets: []MTGSecret{{"7.alice", "ee00"}}}}})
	run := []Running{{Key: "inbound-4", Gen: 1, Meta: mtgMeta{InboundID: 4, APIPort: p}}}
	e.mtgCollect(context.Background(), run)
	mu.Lock()
	in = 250
	mu.Unlock()
	e.mtgCollect(context.Background(), run)
	if len(traffic) != 2 || traffic[0][7] != [2]int64{100, 300} || traffic[1][7] != [2]int64{150, 450} || len(traffic[0]) != 1 {
		t.Fatalf("traffic %v", traffic)
	}
	if ips := seen.take(); len(ips) != 4 || ips[0] != "7 2001:db8::1" || ips[2] != "7 203.0.113.7" {
		t.Fatalf("ips %v", ips)
	}
	// The rendered config is the main server's format.
	ps, err := e.mtgPlan()
	if err != nil || len(ps) != 1 || !strings.Contains(string(ps[0].Config), `bind-to = "0.0.0.0:443"`) ||
		!strings.Contains(string(ps[0].Config), `"7.alice" = "ee00"`) || ps[0].Args[0] != "run" {
		t.Fatalf("plan %+v %v", ps, err)
	}
}
