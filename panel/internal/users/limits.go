package users

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Periods (traffic resets, the daily cap and time windows) follow the
// panel's timezone: a "day" starts at 00:00 panel time, a week on Saturday
// for Iranian and Afghan timezones and on Monday elsewhere, a month on the
// 1st (Gregorian calendar).

// WeekStart returns the first day of the week for a location.
func WeekStart(loc *time.Location) time.Weekday {
	switch loc.String() {
	case "Asia/Tehran", "Asia/Kabul":
		return time.Saturday
	}
	return time.Monday
}

// PeriodStart returns the start (in loc) of the reset period containing now.
// ok is false for strategies without periods.
func PeriodStart(strategy string, now time.Time, loc *time.Location) (time.Time, bool) {
	t := now.In(loc)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	switch strategy {
	case db.ResetDay:
		return day, true
	case db.ResetWeek:
		back := (int(t.Weekday()) - int(WeekStart(loc)) + 7) % 7
		return day.AddDate(0, 0, -back), true
	case db.ResetMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc), true
	}
	return time.Time{}, false
}

// ResetDue reports whether a periodic traffic reset is due at now: the
// current period (day, week or month in panel time) started after the last
// reset (or after the user was created when never reset).
func ResetDue(u *db.User, now time.Time, loc *time.Location) bool {
	start, ok := PeriodStart(u.ResetStrategy, now, loc)
	if !ok {
		return false
	}
	last := u.CreatedAt
	if u.LastResetAt != nil {
		last = *u.LastResetAt
	}
	return last.Before(start)
}

// DailyResetDue reports whether the daily counter belongs to an earlier
// panel-time day than now.
func DailyResetDue(u *db.User, now time.Time, loc *time.Location) bool {
	if u.DailyResetAt == nil {
		return u.DailyUsed != 0
	}
	start, _ := PeriodStart(db.ResetDay, now, loc)
	return u.DailyResetAt.Before(start)
}

// ParseClock parses "HH:MM" into minutes after midnight.
func ParseClock(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	return hh*60 + mm, nil
}

// HasWindow reports whether the user has any time restriction.
func HasWindow(u *db.User) bool {
	return (u.WindowFrom != "" && u.WindowTo != "") || (u.WindowDays != 0 && u.WindowDays&0x7f != 0x7f)
}

func dayAllowed(mask int, d time.Weekday) bool {
	return mask == 0 || mask&(1<<uint(d)) != 0
}

// InWindow reports whether now falls inside the user's allowed time window
// (panel time). Hours may cross midnight (22:00-02:00); the part after
// midnight belongs to the weekday the window started on. Without hours the
// whole day is allowed on the selected weekdays; equal from/to means all day.
func InWindow(u *db.User, now time.Time, loc *time.Location) bool {
	if !HasWindow(u) {
		return true
	}
	t := now.In(loc)
	if u.WindowFrom == "" || u.WindowTo == "" {
		return dayAllowed(u.WindowDays, t.Weekday())
	}
	from, err1 := ParseClock(u.WindowFrom)
	to, err2 := ParseClock(u.WindowTo)
	if err1 != nil || err2 != nil {
		return true // invalid stored window never locks a user out
	}
	cur := t.Hour()*60 + t.Minute()
	switch {
	case from == to:
		return dayAllowed(u.WindowDays, t.Weekday())
	case from < to:
		return dayAllowed(u.WindowDays, t.Weekday()) && cur >= from && cur < to
	default: // crosses midnight
		if cur >= from {
			return dayAllowed(u.WindowDays, t.Weekday())
		}
		if cur < to {
			return dayAllowed(u.WindowDays, t.AddDate(0, 0, -1).Weekday())
		}
		return false
	}
}

// Suspension returns why a served user must be kept out of the cores right
// now ("" = not suspended): outside its time window, or over its daily cap.
func Suspension(u *db.User, now time.Time, loc *time.Location) string {
	if !InWindow(u, now, loc) {
		return db.SuspendWindow
	}
	if u.DailyLimit > 0 && u.DailyUsed >= u.DailyLimit {
		return db.SuspendDaily
	}
	return ""
}

// Counted applies a traffic multiplier to real bytes.
func Counted(real int64, mult float64) int64 {
	if real <= 0 {
		return 0
	}
	if mult <= 0 || mult == 1 {
		return real
	}
	return int64(float64(real)*mult + 0.5)
}

// LoadLocation returns the panel timezone (UTC when unknown).
func LoadLocation(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
