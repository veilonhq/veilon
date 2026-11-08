package users

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func tp(t time.Time) *time.Time { return &t }
func ip(i int) *int             { return &i }

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		u       db.User
		want    string
		changed bool
	}{
		{"active stays", db.User{Status: db.StatusActive, DataLimit: 100, Up: 10}, db.StatusActive, false},
		{"limited", db.User{Status: db.StatusActive, DataLimit: 100, Up: 60, Down: 40}, db.StatusLimited, true},
		{"unlimited never limited", db.User{Status: db.StatusActive, Up: 1 << 40}, db.StatusActive, false},
		{"expired", db.User{Status: db.StatusActive, ExpireAt: tp(now.Add(-time.Second))}, db.StatusExpired, true},
		{"expires exactly now", db.User{Status: db.StatusActive, ExpireAt: tp(now)}, db.StatusExpired, true},
		{"future expiry", db.User{Status: db.StatusActive, ExpireAt: tp(now.Add(time.Hour))}, db.StatusActive, false},
		{"disabled untouched", db.User{Status: db.StatusDisabled, DataLimit: 1, Up: 5}, db.StatusDisabled, false},
		{"limited recovers after limit raise", db.User{Status: db.StatusLimited, DataLimit: 1000, Up: 5}, db.StatusActive, true},
		{"expired recovers after extend", db.User{Status: db.StatusExpired, ExpireAt: tp(now.Add(24 * time.Hour))}, db.StatusActive, true},
		{"on_hold waits", db.User{Status: db.StatusOnHold, OnHoldDays: ip(30)}, db.StatusOnHold, false},
	}
	for _, c := range cases {
		u := c.u
		ch := Evaluate(&u, now)
		if u.Status != c.want || ch != c.changed {
			t.Errorf("%s: got %s changed=%v, want %s changed=%v", c.name, u.Status, ch, c.want, c.changed)
		}
	}
}

func TestEvaluateOnHoldStart(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	first := now.Add(-30 * time.Second)
	u := db.User{Status: db.StatusOnHold, OnHoldDays: ip(10), LastOnlineAt: &first}
	if !Evaluate(&u, now) {
		t.Fatal("expected change")
	}
	if u.Status != db.StatusActive || u.OnHoldDays != nil || u.ExpireAt == nil {
		t.Fatalf("bad transition: %+v", u)
	}
	if want := first.Add(10 * 24 * time.Hour); !u.ExpireAt.Equal(want) {
		t.Fatalf("expire %v want %v", u.ExpireAt, want)
	}
}

func TestDecideStatus(t *testing.T) {
	now := time.Now().UTC()
	u := db.User{Status: db.StatusActive, OnHoldDays: ip(5), LastOnlineAt: tp(now)}
	decideStatus(&u, "", now)
	if u.Status != db.StatusOnHold || u.LastOnlineAt != nil {
		t.Fatalf("entering on_hold should reset last_online: %+v", u)
	}
	u2 := db.User{Status: db.StatusDisabled, DataLimit: 1, Up: 10}
	decideStatus(&u2, "", now)
	if u2.Status != db.StatusDisabled {
		t.Fatal("disabled user changed without request")
	}
	decideStatus(&u2, db.StatusActive, now)
	if u2.Status != db.StatusLimited {
		t.Fatalf("re-enabled over-limit user should be limited, got %s", u2.Status)
	}
	u3 := db.User{Status: db.StatusActive, OnHoldDays: ip(5), ExpireAt: tp(now.Add(time.Hour))}
	decideStatus(&u3, "", now)
	if u3.Status != db.StatusActive || u3.OnHoldDays != nil {
		t.Fatalf("explicit expiry should win over on_hold: %+v", u3)
	}
}
