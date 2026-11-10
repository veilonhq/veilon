// Package jobs runs periodic maintenance: user status transitions, traffic
// resets, traffic chart points and certificate renewal.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// Runner holds job dependencies.
type Runner struct {
	DB *gorm.DB
	// TakeUsage returns the counted user traffic (bytes added to user
	// counters) since the last call; it is stored as history points so the
	// chart matches the users' totals (may be nil).
	TakeUsage func() (up, down int64)
	// RestoreUsage puts deltas back when storing them failed (may be nil).
	RestoreUsage func(up, down int64)
	// TakeServers / RestoreServers are TakeUsage / RestoreUsage per server
	// (0 = main server, otherwise a node id); when set they are used
	// instead, and each server gets its own history points.
	TakeServers    func() map[int64][2]int64
	RestoreServers func(map[int64][2]int64)
	// OnUsersChanged is called when served users changed (Xray reload).
	OnUsersChanged func()
	// RenewCerts renews due certificates (may be nil).
	RenewCerts func(ctx context.Context)
	Now        func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// CertCheckInterval is how often certificates are checked for renewal.
// Short-lived (IP) certificates are renewed at about 3 days of age, so the
// check must run well inside that window; the check itself is a cheap query.
const CertCheckInterval = time.Hour

// Run executes jobs until ctx is done: user jobs every minute, certificate
// renewal checks hourly (first check one minute after start).
func (r *Runner) Run(ctx context.Context) {
	minute := time.NewTicker(time.Minute)
	defer minute.Stop()
	certT := time.NewTimer(time.Minute)
	defer certT.Stop()
	util.Safe("jobs", r.Tick)
	for {
		select {
		case <-ctx.Done():
			util.Safe("traffic flush", r.recordTraffic) // flush pending traffic
			return
		case <-minute.C:
			util.Safe("jobs", r.Tick)
		case <-certT.C:
			if r.RenewCerts != nil {
				util.Safe("certificate renewal", func() { r.RenewCerts(ctx) })
			}
			certT.Reset(CertCheckInterval)
		}
	}
}

// Tick runs all per-minute jobs once.
func (r *Runner) Tick() {
	changed, err := ProcessUsers(r.DB, r.now())
	if err != nil {
		slog.Warn("user job failed", "err", err)
	}
	if changed && r.OnUsersChanged != nil {
		r.OnUsersChanged()
	}
	r.recordTraffic()
	r.prune()
}

func (r *Runner) recordTraffic() {
	if r.TakeServers != nil {
		r.recordServers()
		return
	}
	if r.TakeUsage == nil {
		return
	}
	up, down := r.TakeUsage()
	if up <= 0 && down <= 0 {
		return // the chart zero-fills empty minutes
	}
	if err := r.DB.Create(&db.TrafficPoint{T: r.now().Truncate(time.Minute), Up: up, Down: down}).Error; err != nil {
		slog.Warn("store traffic point failed", "err", err)
		if r.RestoreUsage != nil {
			r.RestoreUsage(up, down)
		}
	}
}

// recordServers stores one history point per server that carried traffic
// this minute, all in one transaction (all or nothing, so a failure puts
// everything back and nothing is counted twice).
func (r *Runner) recordServers() {
	v := r.TakeServers()
	if len(v) == 0 {
		return
	}
	t := r.now().Truncate(time.Minute)
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		for id, x := range v {
			if err := tx.Create(&db.TrafficPoint{T: t, Up: x[0], Down: x[1], NodeID: id}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("store traffic points failed", "err", err)
		if r.RestoreServers != nil {
			r.RestoreServers(v)
		}
	}
}

func (r *Runner) prune() {
	now := r.now()
	if now.Minute() != 0 {
		return
	}
	r.DB.Where("t < ?", now.Add(-31*24*time.Hour)).Delete(&db.TrafficPoint{})
	r.DB.Where("created_at < ?", now.Add(-180*24*time.Hour)).Delete(&db.AuditLog{})
}

// ProcessUsers applies periodic resets (traffic and the daily cap), status
// transitions and suspensions (time window, daily cap) to all users, in the
// panel's timezone. It returns true when the set of users served by the
// cores changed.
func ProcessUsers(gdb *gorm.DB, now time.Time) (bool, error) {
	loc := users.LoadLocation(db.GetSetting(gdb, db.KeyTimezone))
	var rows []db.User
	if err := gdb.Where("status <> ? OR reset_strategy <> ? OR daily_used <> 0", db.StatusDisabled, db.ResetNone).Find(&rows).Error; err != nil {
		return false, err
	}
	servedChanged := false
	err := gdb.Transaction(func(tx *gorm.DB) error {
		for i := range rows {
			u := &rows[i]
			before := users.Served(u)
			dirty, reset, dreset := false, false, false
			if users.ResetDue(u, now, loc) {
				u.Up, u.Down = 0, 0
				t := now
				u.LastResetAt = &t
				dirty, reset = true, true
			}
			if users.DailyResetDue(u, now, loc) || (u.DailyResetAt == nil && u.DailyLimit > 0) {
				u.DailyUsed = 0
				t := now
				u.DailyResetAt = &t
				dirty, dreset = true, true
			}
			if users.Evaluate(u, now) {
				dirty = true
			}
			susp := ""
			if users.IsServed(u.Status) {
				susp = users.Suspension(u, now, loc)
			}
			if susp != u.SuspendReason {
				u.SuspendReason = susp
				dirty = true
			}
			if !dirty {
				continue
			}
			fields := map[string]any{
				"status": u.Status, "expire_at": u.ExpireAt, "on_hold_days": u.OnHoldDays,
				"suspend_reason": u.SuspendReason, "updated_at": now,
			}
			if reset {
				// Only overwrite counters on reset so concurrent traffic
				// increments from the stats collector are not lost.
				fields["up"], fields["down"], fields["last_reset_at"] = int64(0), int64(0), u.LastResetAt
			}
			if dreset {
				fields["daily_used"], fields["daily_reset_at"] = int64(0), u.DailyResetAt
			}
			if err := tx.Model(&db.User{}).Where("id = ?", u.ID).Updates(fields).Error; err != nil {
				return err
			}
			if before != users.Served(u) {
				servedChanged = true
			}
		}
		return nil
	})
	return servedChanged, err
}
