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
