package jobs

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/usage"
)

func TestProcessUsers(t *testing.T) {
	gdb := testutil.DB(t)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	monthAgo := now.AddDate(0, -1, 0)
	online := now.Add(-20 * time.Second)
	days := 7
	mk := func(name, status string, f func(u *db.User)) {
		u := db.User{Username: name, Status: status, ResetStrategy: db.ResetNone, UUID: "u", Password: "p", SubToken: name, CreatedAt: past, UpdatedAt: past}
		if f != nil {
			f(&u)
		}
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk("limited", db.StatusActive, func(u *db.User) { u.DataLimit, u.Down = 100, 100 })
	mk("expired", db.StatusActive, func(u *db.User) { u.ExpireAt = &past })
	mk("ok", db.StatusActive, nil)
	mk("hold", db.StatusOnHold, func(u *db.User) { u.OnHoldDays, u.LastOnlineAt = &days, &online })
	mk("holdwait", db.StatusOnHold, func(u *db.User) { u.OnHoldDays = &days })
	mk("reset", db.StatusLimited, func(u *db.User) {
		u.DataLimit, u.Up, u.ResetStrategy, u.LastResetAt = 100, 150, db.ResetMonth, &monthAgo
	})

	changed, err := ProcessUsers(gdb, now)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("served set should have changed")
	}
	want := map[string]string{
		"limited": db.StatusLimited, "expired": db.StatusExpired, "ok": db.StatusActive,
		"hold": db.StatusActive, "holdwait": db.StatusOnHold, "reset": db.StatusActive,
	}
	var rows []db.User
	gdb.Find(&rows)
	for _, u := range rows {
		if u.Status != want[u.Username] {
			t.Errorf("%s: status %s want %s", u.Username, u.Status, want[u.Username])
		}
		switch u.Username {
		case "hold":
			if u.ExpireAt == nil || !u.ExpireAt.Equal(online.Add(7*24*time.Hour)) {
				t.Errorf("hold: expire %v", u.ExpireAt)
			}
		case "reset":
			if u.Up != 0 || u.LastResetAt == nil || !u.LastResetAt.Equal(now) {
				t.Errorf("reset: up=%d last=%v", u.Up, u.LastResetAt)
			}
		}
	}
	// Second run is a no-op.
	changed, err = ProcessUsers(gdb, now)
	if err != nil || changed {
		t.Fatalf("second run: changed=%v err=%v", changed, err)
	}
}

func TestTickRecordsTraffic(t *testing.T) {
	gdb := testutil.DB(t)
	m := usage.NewMeter()
	r := &Runner{DB: gdb, TakeUsage: m.Take, RestoreUsage: m.Restore}
	m.Add(10, 20)
	r.Tick()
	r.Tick() // nothing new: no empty point is stored
	m.Add(1, 2)
	r.Tick()
	var pts []db.TrafficPoint
	gdb.Order("id").Find(&pts)
	if len(pts) != 2 || pts[0].Up != 10 || pts[0].Down != 20 || pts[1].Up != 1 || pts[1].Down != 2 {
		t.Fatalf("points %+v", pts)
	}
}
