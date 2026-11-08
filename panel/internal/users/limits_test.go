package users

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func TestResetDueCalendar(t *testing.T) {
	teh, _ := time.LoadLocation("Asia/Tehran")
	// 2026-11-07 is a Saturday.
	last := time.Date(2026, 11, 6, 23, 0, 0, 0, teh) // Friday 23:00 Tehran
	u := db.User{LastResetAt: &last, ResetStrategy: db.ResetDay}
	if ResetDue(&u, time.Date(2026, 11, 6, 23, 59, 0, 0, teh), teh) {
		t.Fatal("day: same day must not reset")
	}
	if !ResetDue(&u, time.Date(2026, 11, 7, 0, 0, 0, 0, teh), teh) {
		t.Fatal("day: midnight panel time must reset")
	}
	u.ResetStrategy = db.ResetWeek
	if !ResetDue(&u, time.Date(2026, 11, 7, 0, 1, 0, 0, teh), teh) {
		t.Fatal("week: Saturday starts the week in Tehran")
	}
	if ResetDue(&u, time.Date(2026, 11, 7, 0, 1, 0, 0, teh), time.UTC) {
		t.Fatal("week: in UTC the week starts on Monday")
	}
	u.ResetStrategy = db.ResetMonth
	if ResetDue(&u, time.Date(2026, 11, 30, 23, 0, 0, 0, teh), teh) || !ResetDue(&u, time.Date(2026, 12, 1, 0, 0, 0, 0, teh), teh) {
		t.Fatal("month reset wrong")
	}
	u.ResetStrategy = db.ResetNone
	if ResetDue(&u, last.AddDate(5, 0, 0), teh) {
		t.Fatal("no_reset should never reset")
	}
	// Never reset: counts from creation.
	u = db.User{CreatedAt: time.Date(2026, 11, 7, 10, 0, 0, 0, teh), ResetStrategy: db.ResetDay}
	if ResetDue(&u, time.Date(2026, 11, 7, 20, 0, 0, 0, teh), teh) || !ResetDue(&u, time.Date(2026, 11, 8, 0, 0, 0, 0, teh), teh) {
		t.Fatal("first reset wrong")
	}
}

func TestDailyReset(t *testing.T) {
	teh, _ := time.LoadLocation("Asia/Tehran")
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, teh)
	u := db.User{DailyResetAt: &at, DailyUsed: 5, DailyLimit: 10}
	if DailyResetDue(&u, time.Date(2026, 11, 7, 23, 59, 0, 0, teh), teh) {
		t.Fatal("same day")
	}
	if !DailyResetDue(&u, time.Date(2026, 11, 8, 0, 0, 0, 0, teh), teh) {
		t.Fatal("midnight must reset")
	}
	if Suspension(&u, at, teh) != "" {
		t.Fatal("under cap")
	}
	u.DailyUsed = 10
	if Suspension(&u, at, teh) != db.SuspendDaily {
		t.Fatal("cap reached must suspend")
	}
}

func TestInWindow(t *testing.T) {
	teh, _ := time.LoadLocation("Asia/Tehran")
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, teh) } // 7 = Sat
	u := db.User{WindowFrom: "08:00", WindowTo: "17:30"}
	for _, c := range []struct {
		t  time.Time
		in bool
	}{{at(7, 7, 59), false}, {at(7, 8, 0), true}, {at(7, 17, 29), true}, {at(7, 17, 30), false}} {
		if InWindow(&u, c.t, teh) != c.in {
			t.Fatalf("day window at %v want %v", c.t, c.in)
		}
	}
	// Crossing midnight, Fridays only (bit 5): Fri 22:00 - Sat 02:00.
	u = db.User{WindowFrom: "22:00", WindowTo: "02:00", WindowDays: 1 << uint(time.Friday)}
	for _, c := range []struct {
		t  time.Time
		in bool
	}{{at(6, 21, 59), false}, {at(6, 22, 0), true}, {at(7, 1, 59), true}, {at(7, 2, 0), false}, {at(7, 22, 30), false}} {
		if InWindow(&u, c.t, teh) != c.in {
			t.Fatalf("night window at %v want %v", c.t, c.in)
		}
	}
	// Weekdays only, no hours.
	u = db.User{WindowDays: 1<<uint(time.Sunday) | 1<<uint(time.Monday)}
	if InWindow(&u, at(7, 12, 0), teh) || !InWindow(&u, at(8, 12, 0), teh) {
		t.Fatal("weekday-only window wrong")
	}
	if Suspension(&u, at(7, 12, 0), teh) != db.SuspendWindow {
		t.Fatal("outside window must suspend")
	}
	// The window is evaluated in panel time, not UTC.
	u = db.User{WindowFrom: "00:00", WindowTo: "03:00"}
	if !InWindow(&u, time.Date(2026, 11, 6, 21, 0, 0, 0, time.UTC), teh) { // 00:30 Tehran
		t.Fatal("panel timezone ignored")
	}
}

func TestCountedMultiplier(t *testing.T) {
	cases := []struct {
		real int64
		m    float64
		want int64
	}{{1000, 1, 1000}, {1000, 0, 1000}, {1000, 1.1, 1100}, {1000, 1.5, 1500}, {1000, 2, 2000}, {1000, 0.5, 500}, {0, 2, 0}, {-5, 2, 0}}
	for _, c := range cases {
		if got := Counted(c.real, c.m); got != c.want {
			t.Errorf("Counted(%d, %v) = %d want %d", c.real, c.m, got, c.want)
		}
	}
}
