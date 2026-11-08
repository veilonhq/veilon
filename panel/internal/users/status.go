package users

import (
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Evaluate computes the status a user should have at time now, applying the
// on_hold → active transition. It mutates u (Status, ExpireAt) and reports
// whether anything changed. Disabled users are never touched.
func Evaluate(u *db.User, now time.Time) bool {
	before := u.Status
	var beforeExp *time.Time
	if u.ExpireAt != nil {
		t := *u.ExpireAt
		beforeExp = &t
	}
	switch u.Status {
	case db.StatusDisabled:
		return false
	case db.StatusOnHold:
		// On-hold users start their period on first connection.
		if u.LastOnlineAt == nil {
			return false
		}
		days := 0
		if u.OnHoldDays != nil {
			days = *u.OnHoldDays
		}
		start := *u.LastOnlineAt
		if start.After(now) {
			start = now
		}
		exp := start.Add(time.Duration(days) * 24 * time.Hour).UTC()
		u.ExpireAt = &exp
		u.OnHoldDays = nil
		u.Status = db.StatusActive
	}
	switch {
	case u.DataLimit > 0 && u.Used() >= u.DataLimit:
		u.Status = db.StatusLimited
	case u.ExpireAt != nil && !now.Before(*u.ExpireAt):
		u.Status = db.StatusExpired
	default:
		u.Status = db.StatusActive
	}
	expChanged := (beforeExp == nil) != (u.ExpireAt == nil) || (beforeExp != nil && !beforeExp.Equal(*u.ExpireAt))
	return before != u.Status || expChanged
}

// IsServed reports whether a user with this status must be present in the
// Xray config (on_hold users must be able to connect to start their period).
func IsServed(status string) bool {
	return status == db.StatusActive || status == db.StatusOnHold
}

// Served reports whether the user must be present in the cores now: its
// status is served and it is not suspended (time window or daily cap).
func Served(u *db.User) bool {
	return IsServed(u.Status) && u.SuspendReason == ""
}
