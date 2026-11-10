package nodes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

type fakeCore struct {
	mu      sync.Mutex
	applied [][]byte
	reject  error
	traffic xray.Traffic
	running bool
}

func (f *fakeCore) Apply(_ context.Context, cfg []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reject != nil {
		return f.reject
	}
	f.applied = append(f.applied, append([]byte(nil), cfg...))
	f.running = true
	return nil
}
func (f *fakeCore) Status() (bool, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running, "26.10.31", ""
}
func (f *fakeCore) Collect(context.Context) (xray.Traffic, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.traffic
	f.traffic = xray.Traffic{Users: map[int64][2]int64{}}
	return t, nil
}
func (f *fakeCore) Stop() {}
func (f *fakeCore) applies() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

// fakeBuild renders a tiny deterministic config naming the inbound ids.
func fakeBuild(_ *gorm.DB, _ string, ids []int64, apiPort int) ([]byte, []string, error) {
	if ids == nil {
		ids = []int64{}
	}
	b, _ := json.Marshal(map[string]any{"api": apiPort, "inbounds": ids})
	return b, nil, nil
}

type rig struct {
	gdb  *gorm.DB
	pki  *PKI
	m    *Manager
	svc  *Service
	core *fakeCore
	ag   *Agent
	node *db.Node
}

// startAgent enrolls node n and serves its agent on 127.0.0.1.
func startAgent(t *testing.T, r *rig, n *db.Node) (*Agent, *fakeCore) {
	t.Helper()
	e, pin, err := r.pki.Enroll(n.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	core := &fakeCore{traffic: xray.Traffic{Users: map[int64][2]int64{}}}
	a := &Agent{Enrollment: e, Dir: t.TempDir(), Core: core, Version: "test"}
	ts := httptest.NewUnstartedServer(a.Handler())
	tc, err := ServerTLS(e)
	if err != nil {
		t.Fatal(err)
	}
	ts.TLS = tc
	ts.StartTLS()
	t.Cleanup(ts.Close)
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	n.APIPort, _ = strconv.Atoi(port)
	n.Address = "127.0.0.1"
	n.CertSHA256 = pin
	r.gdb.Model(&db.Node{}).Where("id = ?", n.ID).Updates(map[string]any{"api_port": n.APIPort, "address": n.Address, "cert_sha256": pin})
	return a, core
}

func newRig(t *testing.T) *rig {
	t.Helper()
	gdb := testutil.DB(t)
	pki := &PKI{Dir: t.TempDir()}
	m := &Manager{DB: gdb, DataDir: t.TempDir(), PKI: pki, BuildConfig: fakeBuild, RequestTimeout: 2 * time.Second, Timeout: 5 * time.Second}
	r := &rig{gdb: gdb, pki: pki, m: m, svc: &Service{DB: gdb, PKI: pki, Manager: m}}
	now := time.Now().UTC()
	for i, name := range []string{"alice", "bob"} {
		u := db.User{Username: name, Status: db.StatusActive, UUID: fmt.Sprintf("u-%d", i), Password: "p", SubToken: "t" + name, CreatedAt: now, UpdatedAt: now}
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, p := range []string{"vless", "vmess", "mtproto"} {
		ib := db.Inbound{Remark: p, Protocol: p, Port: 4000 + i, Network: "tcp", Security: "none", Enabled: true, CreatedAt: now, UpdatedAt: now}
		if err := gdb.Create(&ib).Error; err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *rig) addNode(t *testing.T, name string, ids ...int64) *db.Node {
	t.Helper()
	n := db.Node{Name: name, Address: "127.0.0.1", APIPort: 1, Enabled: true}
	if err := r.gdb.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		r.gdb.Create(&db.NodeInbound{NodeID: n.ID, InboundID: id})
	}
	return &n
}

func reload(t *testing.T, gdb *gorm.DB, id int64) *db.Node {
	t.Helper()
	var n db.Node
	if err := gdb.First(&n, id).Error; err != nil {
		t.Fatal(err)
	}
	return &n
}

func TestTokenRoundTrip(t *testing.T) {
	p := &PKI{Dir: t.TempDir()}
	e, pin, err := p.Enroll(7, 62050)
	if err != nil {
		t.Fatal(err)
	}
	tok := e.Token()
	if !strings.HasPrefix(tok, "vn1.") || strings.ContainsAny(tok, " \n'\"$") {
		t.Fatalf("token is not shell-safe: %q", tok)
	}
	got, err := ParseToken(tok)
	if err != nil || got.ID != 7 || got.Port != 62050 || got.Cert != e.Cert {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if len(pin) != 64 {
		t.Fatalf("pin %q", pin)
	}
	for _, bad := range []string{"", "vn1.", "vn1.!!!", "xx." + tok[4:], "vn1.e30"} {
		if _, err := ParseToken(bad); err == nil {
			t.Fatalf("bad token %q accepted", bad)
		}
	}
	// the CA survives a restart: a second PKI on the same dir signs with it
	p2 := &PKI{Dir: p.Dir}
	e2, _, err := p2.Enroll(8, 1)
	if err != nil || e2.CA != e.CA {
		t.Fatal("CA was not reused")
	}
}

// Only the master's client certificate opens the agent; the master only
// trusts the certificate it issued for that node.
func TestMutualTLS(t *testing.T) {
	r := newRig(t)
	n := r.addNode(t, "n1")
	ag, _ := startAgent(t, r, n)
	url := "https://127.0.0.1:" + strconv.Itoa(n.APIPort) + "/v1/info"
	get := func(tc *tls.Config) error {
		c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: tc}}
		resp, err := c.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}
	good, err := r.pki.ClientTLS(n.ID, n.CertSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := get(good); err != nil {
		t.Fatalf("master rejected: %v", err)
	}
	// no client certificate
	noCert := good.Clone()
	noCert.Certificates = nil
	if get(noCert) == nil {
		t.Fatal("agent accepted a client without a certificate")
	}
	// another node's certificate (same CA, server-only usage) as a client
	other, _, _ := r.pki.Enroll(99, 1)
	pair, _ := tls.X509KeyPair([]byte(other.Cert), []byte(other.Key))
	asNode := good.Clone()
	asNode.Certificates = []tls.Certificate{pair}
	if get(asNode) == nil {
		t.Fatal("agent accepted a node certificate as a client")
	}
	// a master of another installation (different CA)
	foreign := &PKI{Dir: t.TempDir()}
	ftc, _ := foreign.ClientTLS(n.ID, "")
	ftc.RootCAs = good.RootCAs
	if get(ftc) == nil {
		t.Fatal("agent accepted a foreign master")
	}
	// the master refuses a node certificate it did not issue for this node
	wrongPin, _ := r.pki.ClientTLS(n.ID, strings.Repeat("0", 64))
	if err := get(wrongPin); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("pinned certificate not enforced: %v", err)
	}
	_ = ag
}

func TestSyncPushesConfigAndCountsTrafficOnce(t *testing.T) {
	r := newRig(t)
	n := r.addNode(t, "n1", 1, 2)
	ag, core := startAgent(t, r, n)
	ctx := context.Background()

	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st := r.m.Status(n)
	if st.Status != StateOnline || !st.Synced || core.applies() != 1 {
		t.Fatalf("first sync: %+v applies=%d", st, core.applies())
	}
	if want, _, _ := fakeBuild(nil, "", []int64{1, 2}, DefaultAPIPort); string(core.applied[0]) != string(want) {
		t.Fatalf("pushed %s", core.applied[0])
	}
	// traffic: read by the agent, collected by the master once
	core.mu.Lock()
	core.traffic = xray.Traffic{Users: map[int64][2]int64{1: {100, 200}, 2: {5, 6}}, SysUp: 300, SysDown: 400}
	core.mu.Unlock()
	ag.collect(ctx)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	var a, b db.User
	r.gdb.First(&a, 1)
	r.gdb.First(&b, 2)
	if a.Up != 100 || a.Down != 200 || b.Up != 5 || b.Down != 6 {
		t.Fatalf("user traffic %d/%d %d/%d", a.Up, a.Down, b.Up, b.Down)
	}
	nn := reload(t, r.gdb, n.ID)
	if nn.Up != 300 || nn.Down != 400 || nn.LastSeenAt == nil {
		t.Fatalf("node totals %+v", nn)
	}
	if core.applies() != 1 {
		t.Fatal("unchanged config was pushed again")
	}
	// assignment change -> new config
	r.gdb.Where("node_id = ? AND inbound_id = ?", n.ID, 2).Delete(&db.NodeInbound{})
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if core.applies() != 2 || !strings.Contains(string(core.applied[1]), "[1]") {
		t.Fatalf("assignment change not pushed: %d", core.applies())
	}
	// disabled: the node keeps running but serves nothing
	r.gdb.Model(&db.Node{}).Where("id = ?", n.ID).Update("enabled", false)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if core.applies() != 3 || !strings.Contains(string(core.applied[2]), "[]") {
		t.Fatal("disabled node still serves inbounds")
	}
	if st := r.m.Status(reload(t, r.gdb, n.ID)); st.Status != StateDisabled {
		t.Fatalf("disabled state %+v", st)
	}
}

// A stats answer that never reached the master is sent again (not lost);
// one that did is acknowledged and never counted twice.
func TestStatsBatchesAreAcknowledged(t *testing.T) {
	core := &fakeCore{running: true, traffic: xray.Traffic{Users: map[int64][2]int64{1: {10, 20}}}}
	a := &Agent{Enrollment: &Enrollment{ID: 1}, Core: core, Dir: t.TempDir()}
	h := a.Handler()
	get := func(ack string) StatsReply {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats?ack="+ack, nil))
		var s StatsReply
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		return s
	}
	a.collect(context.Background())
	b1 := get("")
	if b1.Users[1] != [2]int64{10, 20} {
		t.Fatalf("batch 1 %+v", b1)
	}
	core.traffic = xray.Traffic{Users: map[int64][2]int64{1: {1, 1}}}
	a.collect(context.Background())
	// answer lost: the master asks again with its old ack
	again := get("")
	if again.Seq != b1.Seq || again.Users[1] != [2]int64{10, 20} {
		t.Fatalf("unacknowledged batch not resent: %+v", again)
	}
	b2 := get(b1.Seq)
	if b2.Seq == b1.Seq || b2.Users[1] != [2]int64{1, 1} {
		t.Fatalf("batch 2 %+v", b2)
	}
}

func TestRejectedConfigIsReportedAndNotHammered(t *testing.T) {
	r := newRig(t)
	n := r.addNode(t, "n1", 1)
	_, core := startAgent(t, r, n)
	core.reject = errors.New("xray rejected the config: bad outbound")
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st := r.m.Status(n)
	if st.Status != StateDegraded || !strings.Contains(st.Error, "bad outbound") {
		t.Fatalf("state %+v", st)
	}
	core.reject = nil
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if core.applies() != 0 {
		t.Fatal("rejected config pushed again right away")
	}
	// a new config is tried at once
	r.gdb.Create(&db.NodeInbound{NodeID: n.ID, InboundID: 2})
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if core.applies() != 1 || r.m.Status(n).Status != StateOnline {
		t.Fatalf("new config not pushed: %d %+v", core.applies(), r.m.Status(n))
	}
}

// Unreachable, hanging and panicking nodes never block the master: a round
// returns at once, each node is bounded by its timeout, the healthy node is
// synced, and the others are reported offline or degraded.
func TestBrokenNodesNeverBlockTheMaster(t *testing.T) {
	r := newRig(t)
	r.m.RequestTimeout = 400 * time.Millisecond
	r.m.Timeout = 1500 * time.Millisecond

	healthy := r.addNode(t, "healthy", 1)
	_, core := startAgent(t, r, healthy)

	// closed port
	down := r.addNode(t, "down", 1)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, p, _ := net.SplitHostPort(l.Addr().String())
	l.Close()
	r.gdb.Model(&db.Node{}).Where("id = ?", down.ID).Update("api_port", p)

	// accepts TCP but never answers
	hang := r.addNode(t, "hang", 1)
	bh, _ := net.Listen("tcp", "127.0.0.1:0")
	defer bh.Close()
	var conns []net.Conn
	var cmu sync.Mutex
	go func() {
		for {
			c, err := bh.Accept()
			if err != nil {
				return
			}
			cmu.Lock()
			conns = append(conns, c)
			cmu.Unlock()
		}
	}()
	defer func() {
		cmu.Lock()
		for _, c := range conns {
			c.Close()
		}
		cmu.Unlock()
	}()
	_, p2, _ := net.SplitHostPort(bh.Addr().String())
	r.gdb.Model(&db.Node{}).Where("id = ?", hang.ID).Update("api_port", p2)

	// a domain that does not resolve
	nxd := r.addNode(t, "nxdomain", 1)
	r.gdb.Model(&db.Node{}).Where("id = ?", nxd.ID).Update("address", "node.invalid")
	// down and hang were reached before; nxdomain never was
	r.gdb.Model(&db.Node{}).Where("id IN ?", []int64{down.ID, hang.ID}).Update("last_seen_at", time.Now().UTC())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t0 := time.Now()
	r.m.Round(ctx)
	r.m.Round(ctx) // a second round while the first is in flight starts nothing new
	if d := time.Since(t0); d > 200*time.Millisecond {
		t.Fatalf("Round blocked for %v", d)
	}
	// status reads never wait for the network
	t1 := time.Now()
	if _, err := r.svc.List(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t1); d > 200*time.Millisecond {
		t.Fatalf("List blocked for %v", d)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		vs, _ := r.svc.List()
		done := 0
		for _, v := range vs {
			if v.State.CheckedAt != nil {
				done++
			}
		}
		if done == 4 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if d := time.Since(t0); d > 3500*time.Millisecond {
		t.Fatalf("nodes took %v; timeouts not enforced", d)
	}
	want := map[string]string{"healthy": StateOnline, "down": StateOffline, "hang": StateOffline, "nxdomain": StateWaiting}
	vs, _ := r.svc.List()
	for _, v := range vs {
		if v.State.Status != want[v.Name] {
			t.Fatalf("%s: %+v", v.Name, v.State)
		}
		if v.Name != "healthy" && v.State.Error == "" {
			t.Fatalf("%s has no error", v.Name)
		}
	}
	if core.applies() != 1 {
		t.Fatal("healthy node not synced")
	}

	// a panic while rendering a node config is contained
	r.m.BuildConfig = func(*gorm.DB, string, []int64, int) ([]byte, []string, error) { panic("boom") }
	r.m.SyncNode(ctx, reload(t, r.gdb, healthy.ID))
	if st := r.m.Status(healthy); st.Status != StateDegraded {
		t.Fatalf("panic not reported: %+v", st)
	}
	// deleting an unreachable node is quick and complete
	r.m.BuildConfig = fakeBuild
	t2 := time.Now()
	if err := r.svc.Delete(hang.ID); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t2); d > 7*time.Second {
		t.Fatalf("delete took %v", d)
	}
	var left int64
	r.gdb.Model(&db.NodeInbound{}).Where("node_id = ?", hang.ID).Count(&left)
	if left != 0 {
		t.Fatal("assignments left behind")
	}
}

func TestServiceValidationAndEnrollment(t *testing.T) {
	r := newRig(t)
	s := func(v string) *string { return &v }
	ids := func(v ...int64) *[]int64 { return &v }
	for _, in := range []Input{
		{Name: s(""), Address: s("1.2.3.4")},
		{Name: s("a"), Address: s("https://x.example.com")},
		{Name: s("a"), Address: s("1.2.3.4:443")},
		{Name: s("a"), Address: s("nodot")},
		{Name: s("a"), Address: s("1.2.3.4"), InboundIDs: ids(3)},  // mtproto
		{Name: s("a"), Address: s("1.2.3.4"), InboundIDs: ids(99)}, // missing
	} {
		if _, _, err := r.svc.Create(in); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
	v, inst, err := r.svc.Create(Input{Name: s("Frankfurt"), Address: s("de.example.com"), InboundIDs: ids(1, 2, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if v.APIPort != DefaultPort || len(v.InboundIDs) != 2 || v.State.Status != StateWaiting {
		t.Fatalf("created %+v", v)
	}
	if !strings.Contains(inst.Command, InstallScript) || !strings.Contains(inst.Command, " node --token "+inst.Token) {
		t.Fatalf("command %q", inst.Command)
	}
	if _, err := ParseToken(inst.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.svc.Create(Input{Name: s("frankfurt"), Address: s("1.2.3.4")}); !errors.As(err, new(*ConflictError)) {
		t.Fatalf("duplicate name: %v", err)
	}
	pin := reload(t, r.gdb, v.ID).CertSHA256
	if _, err := r.svc.Enroll(v.ID); err != nil {
		t.Fatal(err)
	}
	if reload(t, r.gdb, v.ID).CertSHA256 == pin {
		t.Fatal("new install command must issue a new certificate")
	}
	v2, err := r.svc.Update(v.ID, Input{Address: s("2001:db8::1"), InboundIDs: ids()})
	if err != nil || v2.Address != "2001:db8::1" || len(v2.InboundIDs) != 0 {
		t.Fatalf("update %+v %v", v2, err)
	}
}

func TestAgentServerErrorLogDropsHandshakeNoise(t *testing.T) {
	var buf strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)
	now := time.Unix(1000, 0)
	l := &serverErrLog{now: func() time.Time { return now }}
	w := func(s string) { _, _ = l.Write([]byte(s + "\n")) }
	w("http: TLS handshake error from 127.0.0.1:5000: EOF")
	w("http: TLS handshake error from 1.2.3.4:5000: read: connection reset by peer")
	w("http: TLS handshake error from 1.2.3.4:5000: tls: client didn't provide a certificate")
	if buf.Len() != 0 {
		t.Fatalf("scanner noise was logged: %q", buf.String())
	}
	w("http: TLS handshake error from 5.6.7.8:1: tls: failed to verify certificate: x509: certificate signed by unknown authority")
	w("http: TLS handshake error from 5.6.7.8:2: tls: failed to verify certificate: x509: certificate signed by unknown authority")
	if n := strings.Count(buf.String(), "refused a client certificate"); n != 1 {
		t.Fatalf("want 1 certificate refusal in a minute, got %d: %q", n, buf.String())
	}
	now = now.Add(61 * time.Second)
	w("http: TLS handshake error from 5.6.7.8:3: tls: failed to verify certificate: x509: certificate signed by unknown authority")
	if n := strings.Count(buf.String(), "refused a client certificate"); n != 2 {
		t.Fatalf("want the refusal again after a minute, got %d", n)
	}
	w("http: panic serving 1.2.3.4:1: boom")
	if !strings.Contains(buf.String(), "panic serving") {
		t.Fatal("other server errors must still be logged")
	}
}
