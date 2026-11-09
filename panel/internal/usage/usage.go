// Package usage keeps the panel's single source of truth for traffic: the
// counted bytes added to user counters. The dashboard total, the history
// chart and the live user-traffic rate are all derived from these deltas, so
// they always agree with the per-user numbers.
package usage

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Meter accumulates counted user-traffic deltas. Pending deltas are taken by
// the minute job and stored as history points; recent deltas are kept for
// the live rate.
type Meter struct {
	mu       sync.Mutex
	up, down int64 // pending, not yet stored
	recent   []sample
	keep     time.Duration
	now      func() time.Time
}

type sample struct {
	t        time.Time
	up, down int64
}

// LiveWindow is the window of the live user-traffic rate on the dashboard.
const LiveWindow = 30 * time.Second

// Default is the meter fed by every engine's traffic collector.
var Default = NewMeter()

// NewMeter returns an empty meter.
func NewMeter() *Meter { return &Meter{keep: 2 * time.Minute, now: time.Now} }

// Add records bytes that were just added to user counters.
func (m *Meter) Add(up, down int64) {
	if up <= 0 && down <= 0 {
		return
	}
	if up < 0 {
		up = 0
	}
	if down < 0 {
		down = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.up += up
	m.down += down
	now := m.now()
	m.recent = append(m.recent, sample{now, up, down})
	m.trim(now)
}

// Take returns and clears the pending deltas.
func (m *Meter) Take() (up, down int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	up, down = m.up, m.down
	m.up, m.down = 0, 0
	return up, down
}

// Restore puts deltas back (e.g. when storing them failed).
func (m *Meter) Restore(up, down int64) {
	m.mu.Lock()
	m.up += up
	m.down += down
	m.mu.Unlock()
}

// Rate returns the average user traffic in bytes per second over the last
// window. Collectors poll the engines every 10 s, so windows shorter than
// about 20 s are bursty.
func (m *Meter) Rate(window time.Duration) (upBps, downBps float64) {
	if window <= 0 {
		return 0, 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.trim(now)
	from := now.Add(-window)
	var u, d int64
	for _, s := range m.recent {
		if s.t.After(from) {
			u += s.up
			d += s.down
		}
	}
	sec := window.Seconds()
	return float64(u) / sec, float64(d) / sec
}

func (m *Meter) trim(now time.Time) {
	cut := now.Add(-m.keep)
	i := 0
	for i < len(m.recent) && !m.recent[i].t.After(cut) {
		i++
	}
	if i > 0 {
		m.recent = append(m.recent[:0], m.recent[i:]...)
	}
}

// Totals returns the sum of all users' counted upload and download, the same
// counters the users list shows.
func Totals(gdb *gorm.DB) (up, down int64, err error) {
	var r struct{ Up, Down int64 }
	err = gdb.Model(&db.User{}).Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").Scan(&r).Error
	return r.Up, r.Down, err
}
