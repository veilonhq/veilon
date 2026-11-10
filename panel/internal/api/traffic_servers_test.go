package api

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/jobs"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Totals reconcile across servers: a user's usage is the sum of what the
// main server and every node carried; the history keeps each server apart
// (node traffic never shows as the main server's) and the per-server
// totals add up exactly to the all-servers total.
func TestTrafficBreakdownByServer(t *testing.T) {
	c, srv := setup(t)
	api := "/secret/api/v1"
	_, lm, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = lm["token"].(string)
	now := time.Now().UTC()
	de := db.Node{Name: "DE", Address: "10.0.0.1", APIPort: 62050, Enabled: true, CreatedAt: now, UpdatedAt: now}
	fr := db.Node{Name: "FR", Address: "10.0.0.2", APIPort: 62050, Enabled: true, CreatedAt: now, UpdatedAt: now}
	srv.DB.Create(&de)
	srv.DB.Create(&fr)
	alice := db.User{Username: "alice", Status: db.StatusActive, ResetStrategy: db.ResetNone, UUID: "a", Password: "p", SubToken: "a", TrafficMultiplier: 1}
	bob := db.User{Username: "bob", Status: db.StatusActive, ResetStrategy: db.ResetNone, UUID: "b", Password: "p", SubToken: "b", TrafficMultiplier: 2}
	srv.DB.Create(&alice)
	srv.DB.Create(&bob)
	usage.TakeAll() // clean meters

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(xray.ApplyUserTrafficFrom(srv.DB, map[int64][2]int64{alice.ID: {10, 100}}, now, 0))
	must(xray.ApplyUserTrafficFrom(srv.DB, map[int64][2]int64{alice.ID: {20, 200}, bob.ID: {1, 2}}, now, de.ID))
	must(xray.ApplyUserTrafficFrom(srv.DB, map[int64][2]int64{bob.ID: {3, 4}}, now, fr.ID))
	r := &jobs.Runner{DB: srv.DB, TakeServers: usage.TakeAll, RestoreServers: usage.RestoreAll}
	r.Tick()

	srv.DB.First(&alice, alice.ID)
	srv.DB.First(&bob, bob.ID)
	if alice.Up != 30 || alice.Down != 300 || bob.Up != 8 || bob.Down != 12 {
		t.Fatalf("user totals: alice %d/%d bob %d/%d", alice.Up, alice.Down, bob.Up, bob.Down)
	}

	type srvT struct{ up, down float64 }
	get := func(q string) (float64, float64, map[string]srvT) {
		t.Helper()
		st, body, _ := c.do("GET", api+"/system/traffic?range=24h&bucket=1h"+q, nil)
		if st != 200 {
			t.Fatalf("%s: %d %v", q, st, body)
		}
		m := map[string]srvT{}
		for _, x := range body["servers"].([]any) {
			s := x.(map[string]any)
			name := s["name"].(string)
			if s["id"].(float64) == 0 {
				name = "main"
			}
			m[name] = srvT{s["up"].(float64), s["down"].(float64)}
		}
		return body["total_up"].(float64), body["total_down"].(float64), m
	}
	tu, td, by := get("")
	if tu != 38 || td != 312 {
		t.Fatalf("all servers %v/%v", tu, td)
	}
	if by["main"] != (srvT{10, 100}) || by["DE"] != (srvT{20 + 2, 200 + 4}) || by["FR"] != (srvT{6, 8}) {
		t.Fatalf("breakdown %+v", by)
	}
	var su, sd float64
	for _, v := range by {
		su += v.up
		sd += v.down
	}
	if su != tu || sd != td {
		t.Fatalf("servers sum %v/%v != total %v/%v", su, sd, tu, td)
	}
	if u, d, _ := get("&server=main"); u != 10 || d != 100 {
		t.Fatalf("main only %v/%v", u, d)
	}
	if u, d, _ := get("&server=" + itoa(int(de.ID))); u != 22 || d != 204 {
		t.Fatalf("DE only %v/%v", u, d)
	}
	if st, _, _ := c.do("GET", api+"/system/traffic?range=24h&server=x", nil); st != 400 {
		t.Fatal("bad server accepted")
	}
	// the dashboard total is the sum of the users (all servers)
	_, body, _ := c.do("GET", api+"/system/stats", nil)
	if body["traffic_total_up"].(float64) != 38 || body["traffic_total_down"].(float64) != 312 {
		t.Fatalf("dashboard total %v/%v", body["traffic_total_up"], body["traffic_total_down"])
	}
	// a deleted node's history stays, marked as removed
	srv.DB.Delete(&db.Node{}, fr.ID)
	_, _, by = get("")
	if by["#"+itoa(int(fr.ID))] != (srvT{6, 8}) {
		t.Fatalf("removed node %+v", by)
	}
}
