package api

import (
	"time"

	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/jobs"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// The dashboard total must be exactly the sum of all users' counters, and
// the history must hold exactly the bytes added to those counters.
func TestDashboardTotalEqualsSumOfUsers(t *testing.T) {
	c, srv := setup(t)
	api := "/secret/api/v1"
	_, lm, _ := c.do("POST", api+"/auth/login", map[string]string{"username": "root", "password": "password123"})
	c.tok = lm["token"].(string)

	mk := func(name string, up, down int64, mult float64) int64 {
		u := db.User{Username: name, Status: db.StatusActive, ResetStrategy: db.ResetNone, UUID: name, Password: "p", SubToken: name, Up: up, Down: down, TrafficMultiplier: mult}
		if err := srv.DB.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	a := mk("a", 560918630, 862071430, 1)
	b := mk("b", 2052293, 62598970, 1.5)
	mk("c", 0, 0, 1)

	usage.Default.Take() // start from a clean meter
	// Traffic from engines (Xray, AmneziaWG, ...) lands in user counters;
	// user 999 does not exist and must not be counted anywhere.
	if err := xray.ApplyUserTraffic(srv.DB, map[int64][2]int64{a: {1000, 4000}, b: {100, 200}, 999: {7, 7}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	r := &jobs.Runner{DB: srv.DB, TakeUsage: usage.Default.Take, RestoreUsage: usage.Default.Restore}
	r.Tick()

	var us []db.User
	srv.DB.Find(&us)
	var su, sd int64
	for _, u := range us {
		su += u.Up
		sd += u.Down
	}
	st, body, _ := c.do("GET", api+"/system/stats", nil)
	if st != 200 {
		t.Fatalf("stats %d %v", st, body)
	}
	if int64(body["traffic_total_up"].(float64)) != su || int64(body["traffic_total_down"].(float64)) != sd {
		t.Fatalf("total %v/%v, sum of users %d/%d", body["traffic_total_up"], body["traffic_total_down"], su, sd)
	}
	if _, ok := body["traffic_down_bps"]; !ok {
		t.Fatal("traffic_down_bps missing")
	}

	// History: exactly the counted deltas (b has a 1.5x multiplier).
	st, body, _ = c.do("GET", api+"/system/traffic?range=24h&bucket=1h", nil)
	if st != 200 {
		t.Fatalf("traffic %d %v", st, body)
	}
	if body["bucket_sec"].(float64) != 3600 || len(body["points"].([]any)) != 24 {
		t.Fatalf("buckets %v / %d", body["bucket_sec"], len(body["points"].([]any)))
	}
	if body["total_up"].(float64) != 1000+150 || body["total_down"].(float64) != 4000+300 {
		t.Fatalf("history totals %v/%v", body["total_up"], body["total_down"])
	}
	if st, _, _ := c.do("GET", api+"/system/traffic?range=30d&bucket=5m", nil); st != 400 {
		t.Fatalf("too many buckets accepted: %d", st)
	}
}
