package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/cores"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/presence"
)

// engineInbounds adds an AmneziaWG (port 4100) and an MTProto (port 4101)
// inbound with alice (1) and bob (2) on them, and returns their ids.
func engineInbounds(t *testing.T, r *rig) (awg, mtg int64) {
	t.Helper()
	svc := &inbounds.Service{DB: r.gdb}
	ids := []int64{}
	for i, p := range []string{inbounds.AmneziaWG, inbounds.MTProto} {
		v := inbounds.Defaults()
		v.Remark, v.Protocol, v.Port = "e-"+p, p, 4100+i
		out, err := svc.Create(&v)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, out.ID)
		for _, u := range []int64{1, 2} {
			r.gdb.Create(&db.UserInbound{UserID: u, InboundID: out.ID})
		}
	}
	return ids[0], ids[1]
}

// An agent before v0.3.8 announces no engine capability: its AmneziaWG and
// MTProto inbounds are skipped and listed for an update, nothing is sent to
// it, and its Xray inbounds keep working as before.
func TestEnginesSkippedForOldAgent(t *testing.T) {
	r := newRig(t)
	awg, mtg := engineInbounds(t, r)
	n := r.addNode(t, "old", 1, awg, mtg)
	_, core := startAgent(t, r, n)
	r.m.SyncNode(context.Background(), reload(t, r.gdb, n.ID))
	st := r.m.Status(n)
	if st.Status != StateOnline || !st.Synced || core.applies() != 1 {
		t.Fatalf("old agent: %+v applies=%d", st, core.applies())
	}
	if len(st.NeedsUpdate) != 2 || st.NeedsUpdate[0].Protocol != inbounds.AmneziaWG || st.NeedsUpdate[1].Protocol != inbounds.MTProto {
		t.Fatalf("needs_update %+v", st.NeedsUpdate)
	}
	if r.enginePuts.Load() != 0 {
		t.Fatal("engines sent to an agent without the capability")
	}
	if caps, known := r.m.Caps(n.ID); !known || len(caps) != 0 {
		t.Fatalf("caps %v %v", caps, known)
	}
	// The Xray config does not carry them either.
	if strings.Contains(string(core.applied[0]), "4100") || strings.Contains(string(core.applied[0]), "4101") {
		t.Fatalf("engine inbound in the Xray config: %s", core.applied[0])
	}
}

// A current agent gets its AmneziaWG and MTProto inbounds once, again only
// when they change, an empty spec when they are gone, and guards their
// ports with the IP-limit firewall.
func TestEnginesPushedToNewAgent(t *testing.T) {
	r := newRig(t)
	r.engines = true
	awg, mtg := engineInbounds(t, r)
	n := r.addNode(t, "new", 1, awg, mtg)
	ag, _ := startAgent(t, r, n)
	rec := &nftRec{}
	ag.Firewall = &guard.NodeFirewall{Run: rec.run, LookPath: func(string) (string, error) { return "nft", nil }}
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st := r.m.Status(n)
	if st.Status != StateOnline || len(st.NeedsUpdate) != 0 || r.enginePuts.Load() != 1 {
		t.Fatalf("first sync %+v puts=%d", st, r.enginePuts.Load())
	}
	b, err := os.ReadFile(filepath.Join(ag.Dir, "engines.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec cores.NodeSpec
	if err := json.Unmarshal(b, &spec); err != nil || len(spec.AWG) != 1 || len(spec.MTG) != 1 || len(spec.AWG[0].Iface.Peers) != 2 || len(spec.MTG[0].Secrets) != 2 {
		t.Fatalf("spec %s %v", b, err)
	}
	if caps, _ := r.m.Caps(n.ID); strings.Join(caps, ",") != "amneziawg,mtproto" {
		t.Fatalf("caps %v", caps)
	}
	// Unchanged: not pushed again.
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if r.enginePuts.Load() != 1 {
		t.Fatalf("unchanged spec pushed again (%d)", r.enginePuts.Load())
	}
	// Firewall: Xray's and the engines' ports.
	if err := ag.Firewall.Apply([]guard.Ban{{IP: "198.51.100.3", TTL: 60}}); err != nil {
		t.Fatal(err)
	}
	if s := rec.all(); !strings.Contains(s, "4100") || !strings.Contains(s, "4101") {
		t.Fatalf("firewall script:\n%s", s)
	}
	// A user leaves the MTProto inbound: pushed again with one secret.
	r.gdb.Where("user_id = ? AND inbound_id = ?", 2, mtg).Delete(&db.UserInbound{})
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	b, _ = os.ReadFile(filepath.Join(ag.Dir, "engines.json"))
	spec = cores.NodeSpec{}
	_ = json.Unmarshal(b, &spec)
	if r.enginePuts.Load() != 2 || len(spec.MTG) != 1 || len(spec.MTG[0].Secrets) != 1 {
		t.Fatalf("user change: puts=%d spec=%s", r.enginePuts.Load(), b)
	}
	// Both leave the node: an empty spec clears them.
	r.gdb.Where("node_id = ? AND inbound_id IN ?", n.ID, []int64{awg, mtg}).Delete(&db.NodeInbound{})
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	b, _ = os.ReadFile(filepath.Join(ag.Dir, "engines.json"))
	if r.enginePuts.Load() != 3 || string(b) != "{}" {
		t.Fatalf("cleared: puts=%d spec=%s", r.enginePuts.Load(), b)
	}
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if r.enginePuts.Load() != 3 {
		t.Fatal("empty spec pushed again")
	}
	// A restarted agent runs the last spec again (before the master calls).
	ag2 := &Agent{Enrollment: ag.Enrollment, Dir: ag.Dir, Core: &fakeCore{}, Engines: &cores.NodeEngines{Dir: ag.Dir}}
	ag2.Restore(ctx)
	if h := ag2.info().EnginesHash; h != Hash([]byte("{}")) {
		t.Fatalf("restored hash %q", h)
	}
}

// Traffic of the engines reaches the users and the node like Xray's, and
// makes the user online by the traffic rule.
func TestEngineTrafficCountedAndOnline(t *testing.T) {
	r := newRig(t)
	r.engines = true
	tr := presence.New()
	r.m.Presence = tr
	awg, _ := engineInbounds(t, r)
	n := r.addNode(t, "n", awg)
	ag, _ := startAgent(t, r, n)
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	ag.engineTraffic(map[int64][2]int64{1: {1000, 4000}}, 1000, 4000)
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	var u db.User
	r.gdb.First(&u, 1)
	if u.Up != 1000 || u.Down != 4000 {
		t.Fatalf("user %d/%d", u.Up, u.Down)
	}
	if nn := reload(t, r.gdb, n.ID); nn.Up != 1000 || nn.Down != 4000 {
		t.Fatalf("node %d/%d", nn.Up, nn.Down)
	}
	now := time.Now().UTC()
	if !tr.Online(1, now) || tr.Online(2, now) {
		t.Fatal("online by engine traffic")
	}
	if tr.Online(1, now.Add(2*presence.TrafficWindow)) {
		t.Fatal("still online long after the traffic")
	}
}

// The agent checks what it is given.
func TestPutEnginesRefusesBadInput(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Enrollment: &Enrollment{ID: 1}, Dir: dir, Core: &fakeCore{}, Engines: &cores.NodeEngines{Dir: dir}}
	h := a.Handler()
	put := func(p EnginesPush) int {
		b, _ := json.Marshal(p)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/v1/engines", bytes.NewReader(b)))
		return w.Code
	}
	spec := []byte(`{"mtg":[{"inbound_id":4,"port":443,"secrets":[]}]}`)
	if c := put(EnginesPush{Hash: "x", Spec: spec}); c != 400 {
		t.Fatalf("bad hash: %d", c)
	}
	if c := put(EnginesPush{Hash: Hash(spec), Spec: spec}); c != 422 {
		t.Fatalf("invalid spec: %d", c)
	}
	if _, err := os.Stat(filepath.Join(dir, "engines.json")); err == nil {
		t.Fatal("refused spec was saved")
	}
	// An agent without engines answers 404, which the master never asks.
	old := (&Agent{Enrollment: &Enrollment{ID: 1}, Dir: dir, Core: &fakeCore{}}).Handler()
	w := httptest.NewRecorder()
	old.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/v1/engines", strings.NewReader("{}")))
	if w.Code != 404 {
		t.Fatalf("no engines: %d", w.Code)
	}
}
