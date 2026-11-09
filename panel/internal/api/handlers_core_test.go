package api

import (
	"context"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/xray"
)

type fakeXray struct {
	on       bool
	installs []string
	busy     bool
}

func (f *fakeXray) Status() xray.Status {
	return xray.Status{Name: "xray", Running: f.on, Stopped: !f.on}
}
func (f *fakeXray) SetPower(_ context.Context, on bool) error {
	f.on = on
	return nil
}
func (f *fakeXray) StartInstall(v string) error {
	if f.busy {
		return xray.ErrBusy
	}
	if v == "vbad" {
		return xray.ErrBadVersion
	}
	f.installs = append(f.installs, v)
	return nil
}
func (f *fakeXray) StartGeoUpdate() error { return nil }
func (f *fakeXray) Releases(context.Context, bool) (xray.ReleaseList, error) {
	return xray.ReleaseList{Releases: []xray.Release{{Tag: "v26.11.9"}}}, nil
}
func (f *fakeXray) Job() *xray.JobState       { return nil }
func (f *fakeXray) GeoStatus() []xray.GeoInfo { return nil }

type fakeCoreX struct {
	fakeCore
	x *fakeXray
}

func (f *fakeCoreX) XrayController() xray.Controller { return f.x }

func TestCoreXrayEndpoints(t *testing.T) {
	c, srv := setup(t)
	const api = "/secret/api/v1"
	// Without xray controls the endpoints answer 501.
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)
	if code, _, _ := c.do("POST", api+"/core/stop", nil); code != 501 {
		t.Fatalf("want 501, got %d", code)
	}
	fx := &fakeXray{on: true}
	srv.Core = &fakeCoreX{x: fx}

	code, m, _ := c.do("POST", api+"/core/stop", nil)
	if code != 200 || fx.on || m["status"].(map[string]any)["stopped"] != true {
		t.Fatalf("stop %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/core/start", nil); code != 200 || !fx.on {
		t.Fatalf("start %d", code)
	}
	code, m, _ = c.do("GET", api+"/core/xray/releases", nil)
	if code != 200 || len(m["releases"].([]any)) != 1 {
		t.Fatalf("releases %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/core/xray/install", map[string]string{"version": "26.11.9"}); code != 202 || len(fx.installs) != 1 || fx.installs[0] != "v26.11.9" {
		t.Fatalf("install %d %v", code, fx.installs)
	}
	if code, _, _ := c.do("POST", api+"/core/xray/install", map[string]string{"version": "bad"}); code != 400 {
		t.Fatalf("bad version %d", code)
	}
	fx.busy = true
	if code, _, _ := c.do("POST", api+"/core/xray/install", map[string]string{"version": "v26.11.9"}); code != 409 {
		t.Fatalf("busy %d", code)
	}
	if code, _, _ := c.do("POST", api+"/core/xray/geo", nil); code != 202 {
		t.Fatalf("geo %d", code)
	}
	if code, m, _ := c.do("GET", api+"/core/xray", nil); code != 200 || m["status"] == nil {
		t.Fatalf("info %d %v", code, m)
	}
}

// fakePowerSet switches named engines; it mirrors cores.Set.SetPower.
type fakePowerSet struct {
	fakeCore
	off map[string]bool
}

var fakeEngines = []string{"xray", "sing-box", "mtg", "amneziawg"}

func (f *fakePowerSet) Status() xray.Status {
	return xray.Status{Name: "xray", Running: !f.off["xray"], Stopped: f.off["xray"]}
}
func (f *fakePowerSet) Statuses() []xray.Status {
	out := []xray.Status{}
	for _, n := range fakeEngines {
		out = append(out, xray.Status{Name: n, Running: !f.off[n], Stopped: f.off[n]})
	}
	return out
}
func (f *fakePowerSet) SetPower(_ context.Context, scope string, on bool) ([]string, error) {
	names := fakeEngines
	if scope == "xray" {
		names = names[:1]
	}
	changed := []string{}
	for _, n := range names {
		if f.off[n] == !on {
			continue
		}
		f.off[n] = !on
		changed = append(changed, n)
	}
	return changed, nil
}
func (f *fakePowerSet) StoppedNames() []string {
	out := []string{}
	for _, n := range fakeEngines {
		if f.off[n] {
			out = append(out, n)
		}
	}
	return out
}

func TestCorePowerScope(t *testing.T) {
	c, srv := setup(t)
	const api = "/secret/api/v1"
	_, m, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = m["token"].(string)
	fp := &fakePowerSet{off: map[string]bool{}}
	srv.Core = fp
	stopped := func(m map[string]any) int { return len(m["stopped_cores"].([]any)) }

	// No scope: stop touches only Xray.
	code, m, _ := c.do("POST", api+"/core/stop", nil)
	if code != 200 || !fp.off["xray"] || fp.off["mtg"] || m["scope"] != "xray" || stopped(m) != 1 {
		t.Fatalf("stop %d %v", code, m)
	}
	if code, _, _ := c.do("POST", api+"/core/stop", map[string]string{"scope": "nope"}); code != 400 {
		t.Fatalf("bad scope %d", code)
	}
	// scope=all in the body stops every core.
	code, m, _ = c.do("POST", api+"/core/stop", map[string]string{"scope": "all"})
	if code != 200 || stopped(m) != 4 || len(m["changed"].([]any)) != 3 {
		t.Fatalf("stop all %d %v", code, m)
	}
	code, m, _ = c.do("GET", api+"/system/stats", nil)
	if code != 200 || stopped(m) != 4 {
		t.Fatalf("stats %d %v", code, m["stopped_cores"])
	}
	// scope=xray in the query starts only Xray.
	code, m, _ = c.do("POST", api+"/core/start?scope=xray", nil)
	if code != 200 || fp.off["xray"] || !fp.off["sing-box"] || stopped(m) != 3 {
		t.Fatalf("start xray %d %v", code, m)
	}
	// No scope: start brings back everything that was stopped.
	code, m, _ = c.do("POST", api+"/core/start", nil)
	if code != 200 || stopped(m) != 0 || len(m["changed"].([]any)) != 3 {
		t.Fatalf("start %d %v", code, m)
	}
}
