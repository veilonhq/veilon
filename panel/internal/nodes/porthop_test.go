package nodes

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/porthop"
)

type hopRec struct {
	mu      sync.Mutex
	scripts []string
	deletes int
}

func (h *hopRec) applier() *porthop.Applier {
	return &porthop.Applier{
		Run:    func(s string) error { h.mu.Lock(); defer h.mu.Unlock(); h.scripts = append(h.scripts, s); return nil },
		Delete: func() error { h.mu.Lock(); defer h.mu.Unlock(); h.deletes++; return nil },
		Exists: func() bool { return true },
	}
}

func (h *hopRec) last() (string, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.scripts) == 0 {
		return "", h.deletes
	}
	return h.scripts[len(h.scripts)-1], h.deletes
}

// A Hysteria 2 inbound with port hopping on a node: an agent with
// CapPortHop redirects the ranges; removing hopping (or the inbound)
// removes the rules.
func TestNodePortHopping(t *testing.T) {
	r := newRig(t)
	r.engines = true
	rec := &hopRec{}
	r.agentHook = func(a *Agent) { a.Hop = rec.applier() }
	r.gdb.Create(&db.Cert{ID: 5, Domain: "hy.example.com", Source: "upload"})
	svc := &inbounds.Service{DB: r.gdb}
	v := inbounds.Defaults()
	cid := int64(5)
	v.Remark, v.Protocol, v.Port, v.Security = "hop", inbounds.Hysteria, 4443, "tls"
	v.TLS = &inbounds.TLS{CertID: &cid}
	v.Settings.HopPorts = "20000-20010"
	ib, err := svc.Create(&v)
	if err != nil {
		t.Fatal(err)
	}
	r.gdb.Create(&db.UserInbound{UserID: 1, InboundID: ib.ID})
	n := r.addNode(t, "new", ib.ID)
	ag, _ := startAgent(t, r, n)
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if caps, _ := r.m.Caps(n.ID); !strings.Contains(strings.Join(caps, ","), CapPortHop) {
		t.Fatalf("caps %v", caps)
	}
	if s, _ := rec.last(); !strings.Contains(s, "udp dport 20000-20010 redirect to :4443") {
		t.Fatalf("node script %q", s)
	}
	// Hopping switched off: the table goes.
	if _, err := svc.Update(func() *inbounds.View { x, _ := svc.Get(ib.ID); x.Settings.HopPorts = ""; return x }()); err != nil {
		t.Fatal(err)
	}
	_, before := rec.last()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	if _, after := rec.last(); after != before+1 {
		t.Fatalf("rules not removed (deletes %d -> %d)", before, after)
	}
	_ = ag
}

// An agent without CapPortHop gets no hop rules (older agents never see
// PUT /v1/engines changes for it) and its links carry no hop ports.
func TestNodePortHoppingOldAgent(t *testing.T) {
	r := newRig(t)
	r.engines = true // engines but no Hop: an agent between v0.3.8 and this release
	r.gdb.Create(&db.Cert{ID: 5, Domain: "hy.example.com", Source: "upload"})
	svc := &inbounds.Service{DB: r.gdb}
	v := inbounds.Defaults()
	cid := int64(5)
	v.Remark, v.Protocol, v.Port, v.Security = "hop", inbounds.Hysteria, 4443, "tls"
	v.TLS = &inbounds.TLS{CertID: &cid}
	v.Settings.HopPorts = "20000-20010"
	ib, err := svc.Create(&v)
	if err != nil {
		t.Fatal(err)
	}
	n := r.addNode(t, "old", ib.ID)
	startAgent(t, r, n)
	r.m.SyncNode(context.Background(), reload(t, r.gdb, n.ID))
	inbounds.NodeLacksCap = func(id int64, c string) bool {
		caps, known := r.m.Caps(id)
		return known && !strings.Contains(strings.Join(caps, ","), c)
	}
	defer func() { inbounds.NodeLacksCap = nil }()
	ts := inbounds.NodeTargets(r.gdb, []int64{ib.ID})
	if len(ts[ib.ID]) != 1 || !ts[ib.ID][0].NoHop {
		t.Fatalf("targets %+v", ts)
	}
}
