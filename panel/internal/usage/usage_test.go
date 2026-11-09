package usage

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestMeterTakeAndRate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewMeter()
	m.now = func() time.Time { return now }
	m.Add(300, 600)
	m.Add(-5, 0) // ignored
	now = now.Add(10 * time.Second)
	m.Add(300, 600)
	if u, d := m.Rate(30 * time.Second); u != 20 || d != 40 {
		t.Fatalf("rate %v %v", u, d)
	}
	if u, d := m.Take(); u != 600 || d != 1200 {
		t.Fatalf("take %d %d", u, d)
	}
	if u, d := m.Take(); u != 0 || d != 0 {
		t.Fatalf("second take %d %d", u, d)
	}
	m.Restore(1, 2)
	if u, d := m.Take(); u != 1 || d != 2 {
		t.Fatalf("restore %d %d", u, d)
	}
	now = now.Add(5 * time.Minute) // old samples leave the live window
	if u, d := m.Rate(30 * time.Second); u != 0 || d != 0 {
		t.Fatalf("stale rate %v %v", u, d)
	}
}

func TestTotalsEqualSumOfUsers(t *testing.T) {
	gdb := testutil.DB(t)
	if u, d, err := Totals(gdb); err != nil || u != 0 || d != 0 {
		t.Fatalf("empty: %d %d %v", u, d, err)
	}
	vals := [][2]int64{{560918630, 862071430}, {2052293, 62598970}, {0, 0}, {1 << 40, 3 << 40}}
	var wu, wd int64
	for i, v := range vals {
		u := db.User{Username: string(rune('a' + i)), Status: db.StatusActive, ResetStrategy: db.ResetNone, UUID: "u", Password: "p", SubToken: string(rune('a' + i)), Up: v[0], Down: v[1]}
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		wu += v[0]
		wd += v[1]
	}
	u, d, err := Totals(gdb)
	if err != nil || u != wu || d != wd {
		t.Fatalf("totals %d/%d want %d/%d (%v)", u, d, wu, wd, err)
	}
}
