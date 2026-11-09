package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/stats"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// The history chart must add up to exactly what was added to the users'
// counters (multiplier included), bucketed on the panel's clock.
func TestHistoryMatchesUserCounters(t *testing.T) {
	gdb := testutil.DB(t)
	a := db.User{Username: "a", SubToken: "ta", TrafficMultiplier: 1, Status: db.StatusActive}
	b := db.User{Username: "b", SubToken: "tb", TrafficMultiplier: 1.5, Status: db.StatusActive}
	if err := gdb.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	usage.Default.Take() // start clean
	tehran := time.FixedZone("Tehran", 3*3600+1800)
	now := time.Date(2026, 11, 9, 21, 50, 0, 0, tehran)
	r := &Runner{DB: gdb, TakeUsage: usage.Default.Take, RestoreUsage: usage.Default.Restore, Now: func() time.Time { return now }}
	for i := 0; i < 5; i++ {
		if err := xray.ApplyUserTraffic(gdb, map[int64][2]int64{a.ID: {1000, 50_000}, b.ID: {200, 1_000_000}}, now); err != nil {
			t.Fatal(err)
		}
		r.recordTraffic()
		now = now.Add(5 * time.Minute) // crosses the 22:00 Tehran bucket edge
	}
	var su, sd int64
	var us []db.User
	gdb.Find(&us)
	for _, u := range us {
		su += u.Up
		sd += u.Down
	}
	if su != 5*(1000+300) || sd != 5*(50_000+1_500_000) {
		t.Fatalf("user counters %d/%d", su, sd)
	}
	for _, rg := range []string{"24h", "7d", "30d"} {
		w, bk, _ := stats.RangeSpec(rg)
		pts, err := stats.Traffic(gdb, w, bk, now, tehran)
		if err != nil {
			t.Fatal(err)
		}
		var hu, hd int64
		for i, p := range pts {
			hu += p.Up
			hd += p.Down
			if i > 0 && !p.T.After(pts[i-1].T) {
				t.Fatalf("%s: points out of order", rg)
			}
		}
		if hu != su || hd != sd {
			t.Fatalf("%s: history %d/%d, users %d/%d", rg, hu, hd, su, sd)
		}
	}
	// 15-minute buckets follow Tehran's clock: 21:45 holds 21:50+21:55, 22:00 the rest.
	pts, _ := stats.Traffic(gdb, 24*time.Hour, 15*time.Minute, now, tehran)
	var b2145, b2200 int64
	for _, p := range pts {
		switch p.T.In(tehran).Format("15:04") {
		case "21:45":
			b2145 += p.Down
		case "22:00":
			b2200 += p.Down
		}
	}
	if b2145 != 2*1_550_000 || b2200 != 3*1_550_000 {
		t.Fatalf("buckets 21:45=%d 22:00=%d", b2145, b2200)
	}
}

// Bytes counted while the panel stops are flushed into the history.
func TestRunFlushesOnStop(t *testing.T) {
	gdb := testutil.DB(t)
	m := usage.NewMeter()
	r := &Runner{DB: gdb, TakeUsage: m.Take, RestoreUsage: m.Restore}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	m.Add(7, 9) // e.g. the cores' last counter read during shutdown
	cancel()
	<-done
	var p struct{ Up, Down int64 }
	gdb.Model(&db.TrafficPoint{}).Select("COALESCE(SUM(up),0) AS up, COALESCE(SUM(down),0) AS down").Scan(&p)
	if p.Up != 7 || p.Down != 9 {
		t.Fatalf("flushed %+v", p)
	}
}
