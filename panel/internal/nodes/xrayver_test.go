package nodes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// A node whose Xray-core is too old for an inbound's advanced settings
// keeps running its other inbounds: the inbound is left out of the pushed
// config and listed in the node's state until the node reports a newer
// Xray, then it is pushed again.
func TestOldNodeXraySkipsInbound(t *testing.T) {
	r := newRig(t)
	ml, err := inbounds.GenerateMLDSA65("")
	if err != nil {
		t.Fatal(err)
	}
	svc := &inbounds.Service{DB: r.gdb}
	v := inbounds.Defaults()
	v.Remark, v.Protocol, v.Port, v.Security = "pq", "vless", 4443, "reality"
	v.Reality = &inbounds.Reality{Dest: "dl.google.com"}
	v.Advanced = &inbounds.Advanced{Reality: &inbounds.RealityExtra{MLDSA65Seed: ml.Server}}
	pq, err := svc.Create(&v)
	if err != nil {
		t.Fatal(err)
	}
	n := r.addNode(t, "old-xray", 1, 2, pq.ID)
	_, core := startAgent(t, r, n)
	core.version = "25.6.8"
	ctx := context.Background()
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st := r.m.Status(n)
	pushed := func() []int64 {
		var c struct {
			Inbounds []int64 `json:"inbounds"`
		}
		_ = json.Unmarshal(core.applied[len(core.applied)-1], &c)
		return c.Inbounds
	}
	if st.Status != StateOnline || !st.Synced || len(core.applied) != 1 {
		t.Fatalf("sync %+v applies=%d", st, len(core.applied))
	}
	if got := pushed(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("pushed %v", got)
	}
	if len(st.XrayTooOld) != 1 || st.XrayTooOld[0].ID != pq.ID || st.XrayTooOld[0].Needs[0].Min != inbounds.MinXrayMLDSA65 {
		t.Fatalf("xray_too_old %+v", st.XrayTooOld)
	}
	if len(st.Skipped) != 1 || !strings.Contains(st.Skipped[0], "Reality ML-DSA-65 (25.7.26+)") || !strings.Contains(st.Skipped[0], "25.6.8") {
		t.Fatalf("skipped %v", st.Skipped)
	}
	// The inbound API reports the server where it is left out.
	svc.XrayVersion = func(id int64) string {
		if id == n.ID {
			return r.m.XrayVersion(id)
		}
		return "26.10.31"
	}
	got, _ := svc.Get(pq.ID)
	if len(got.XrayTooOld) != 1 || got.XrayTooOld[0].NodeID != n.ID || got.XrayTooOld[0].Version != "25.6.8" {
		t.Fatalf("view %+v", got.XrayTooOld)
	}
	// It can still be edited (the need was already there) ...
	got.Remark = "pq2"
	if _, err := svc.Update(got); err != nil {
		t.Fatalf("edit refused: %v", err)
	}
	// ... but a new need on that node is refused.
	got.Advanced.Reverse = &inbounds.Reverse{Enabled: true, RouteSelf: true}
	if _, err := svc.Update(got); err == nil || !strings.Contains(err.Error(), `node "old-xray"`) || !strings.Contains(err.Error(), "25.9.11") {
		t.Fatalf("new need on old node: %v", err)
	}
	// The node updates Xray: the inbound is pushed again.
	core.version = "26.10.31"
	r.m.SyncNode(ctx, reload(t, r.gdb, n.ID))
	st = r.m.Status(n)
	if got := pushed(); len(got) != 3 || len(st.XrayTooOld) != 0 || len(st.Skipped) != 0 {
		t.Fatalf("after update pushed %v %+v", got, st)
	}
}

// Unknown versions let everything through.
func TestXrayFilterUnknownVersion(t *testing.T) {
	r := newRig(t)
	ids, old := xrayFilter(r.gdb, []int64{1, 2}, "")
	if len(ids) != 2 || old != nil {
		t.Fatal(ids, old)
	}
	var cnt int64
	r.gdb.Model(&db.Inbound{}).Count(&cnt)
	if cnt == 0 {
		t.Fatal("no inbounds")
	}
}
