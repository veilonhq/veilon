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

// Per-server meters: 0 is the main server, any other id a node. A user's
// usage is the sum over all servers; the history keeps the servers apart.
var (
	srcMu   sync.Mutex
	sources = map[int64]*Meter{0: Default}
)

// For returns the meter of a server (0 = main server).
func For(id int64) *Meter {
	srcMu.Lock()
	defer srcMu.Unlock()
	m := sources[id]
	if m == nil {
		m = NewMeter()
		sources[id] = m
	}
	return m
}

func all() map[int64]*Meter {
	srcMu.Lock()
	defer srcMu.Unlock()
	out := make(map[int64]*Meter, len(sources))
	for k, v := range sources {
		out[k] = v
	}
	return out
}

// TakeAll returns and clears the pending usage of every server.
func TakeAll() map[int64][2]int64 {
	out := map[int64][2]int64{}
	for id, m := range all() {
		if u, d := m.Take(); u > 0 || d > 0 {
			out[id] = [2]int64{u, d}
		}
	}
	return out
}

// RestoreAll puts back what TakeAll returned (when storing failed).
func RestoreAll(v map[int64][2]int64) {
	for id, x := range v {
		For(id).Restore(x[0], x[1])
	}
}

// RateAll is the live usage rate summed over all servers.
func RateAll(window time.Duration) (upBps, downBps float64) {
	for _, m := range all() {
		u, d := m.Rate(window)
		upBps += u
		downBps += d
	}
	return
}

// Totals returns the sum of all users' counted upload and download, the same
// counters the users list shows.
func Totals(gdb *gorm.DB) (up, down int64, err error) {
	var r struct{ Up, Down int64 }
	err = gdb.Model(&db.User{}).Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").Scan(&r).Error
	return r.Up, r.Down, err
}
