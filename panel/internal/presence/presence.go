// Package presence decides which users are online right now.
//
// Sources report what they see: Xray cores that track live connections
// (statsUserOnline) report the set of users with at least one open
// connection every few seconds (ReportLive); every core also reports the
// users whose client sent bytes (Traffic). A user is online while any source
// sees them:
//
//   - a live source that reported recently: the user is in its last sets
//     (OfflineGrace absorbs one missed poll or a core restart), or the user
//     sends traffic the live list cannot see (clients reaching Xray through a
//     loopback hop, such as the sing-box bridge or a local reverse proxy);
//   - any other source (older node agents, AmneziaWG, MTProto, an Xray that
//     does not track connections): uplink traffic within TrafficWindow, the
//     panel's earlier rule.
//
// users.last_online_at is the moment the user was last seen online. Flush
// writes it when a user comes online or goes offline, and at most every
// WriteEvery while they stay online, never on every poll.
package presence

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Defaults of a Tracker.
const (
	// TrafficWindow: a user of a traffic-only source is online while their
	// client sent bytes this recently (cores are read every 10 s).
	TrafficWindow = 60 * time.Second
	// LiveStale: a live source that has not reported for this long no
	// longer counts as live (its core stopped, the node is unreachable).
	LiveStale = 20 * time.Second
	// OfflineGrace: a user missing from a live source's set stays online
	// this long, so one missed poll or a core restart does not flicker.
	OfflineGrace = 8 * time.Second
	// HiddenGap: traffic on a live source from a user its set has not shown
	// for this long (or ever) came through a hop the live list cannot see,
	// so the traffic rule applies to them.
	HiddenGap = 30 * time.Second
	// WriteEvery: while a user stays online, last_online_at moves at most
	// this often.
	WriteEvery = 60 * time.Second
)

// MainXray is the main server's Xray core, AWG and MTG its other engines.
const (
	MainXray = "main:xray"
	MainAWG  = "main:awg"
	MainMTG  = "main:mtg"
)

type seen struct {
	live    time.Time // last time a live report listed the user
	traffic time.Time // last time the user's client sent bytes
}

type source struct {
	liveAt time.Time // last live report ("" = traffic only)
	users  map[int64]*seen
}

type flushed struct {
	online  bool      // online at the last flush
	written time.Time // last value written to last_online_at
	at      time.Time // when it was written
}

// Tracker keeps who is online, in memory.
type Tracker struct {
	TrafficWindow time.Duration
	LiveStale     time.Duration
	OfflineGrace  time.Duration
	HiddenGap     time.Duration
	WriteEvery    time.Duration

	mu      sync.Mutex
	sources map[string]*source
	state   map[int64]*flushed
}

// New returns a Tracker with the default timings.
func New() *Tracker {
	return &Tracker{TrafficWindow: TrafficWindow, LiveStale: LiveStale, OfflineGrace: OfflineGrace,
		HiddenGap: HiddenGap, WriteEvery: WriteEvery}
}

// Default is the panel's tracker.
var Default = New()

func (t *Tracker) src(name string) *source {
	if t.sources == nil {
		t.sources = map[string]*source{}
	}
	s := t.sources[name]
	if s == nil {
		s = &source{users: map[int64]*seen{}}
		t.sources[name] = s
	}
	return s
}

func (s *source) user(id int64) *seen {
	u := s.users[id]
	if u == nil {
		u = &seen{}
		s.users[id] = u
	}
	return u
}

// ReportLive records the users with at least one open connection on a
// source that tracks connections, as seen at now.
func (t *Tracker) ReportLive(name string, ids []int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.src(name)
	s.liveAt = now
	for _, id := range ids {
		s.user(id).live = now
	}
}

// NotLive marks a source as traffic only from now on (its core stopped
// tracking connections, or never did).
func (t *Tracker) NotLive(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.sources[name]; s != nil {
		s.liveAt = time.Time{}
	}
}

// Traffic records traffic deltas ({up, down} bytes) a source counted at
// now. Only uplink, bytes received from the client, means the client is
// there: downlink can still be written to a client that already left.
func (t *Tracker) Traffic(name string, deltas map[int64][2]int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var s *source
	for id, d := range deltas {
		if d[0] <= 0 {
			continue
		}
		if s == nil {
			s = t.src(name)
		}
		s.user(id).traffic = now
	}
}

// Forget drops a source (a deleted node).
func (t *Tracker) Forget(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sources, name)
}

func (t *Tracker) liveFresh(s *source, now time.Time) bool {
	return !s.liveAt.IsZero() && now.Sub(s.liveAt) < t.LiveStale
}

func (t *Tracker) onlineIn(s *source, u *seen, now time.Time) bool {
	recentTraffic := !u.traffic.IsZero() && now.Sub(u.traffic) < t.TrafficWindow
	if !t.liveFresh(s, now) {
		// Only traffic newer than the last live report: what that report
		// already covered (an empty set when the core stopped) stands.
		return recentTraffic && u.traffic.After(s.liveAt)
	}
	if !u.live.IsZero() && now.Sub(u.live) < t.OfflineGrace {
		return true
	}
	// Traffic the live list does not show (a loopback hop).
	return recentTraffic && (u.live.IsZero() || u.traffic.Sub(u.live) > t.HiddenGap)
}

func (t *Tracker) online(id int64, now time.Time) bool {
	for _, s := range t.sources {
		if u := s.users[id]; u != nil && t.onlineIn(s, u, now) {
			return true
		}
	}
	return false
}

// lastSeen is the latest moment any source saw the user (zero = none).
func (t *Tracker) lastSeen(id int64) time.Time {
	var last time.Time
	for _, s := range t.sources {
		if u := s.users[id]; u != nil {
			if u.live.After(last) {
				last = u.live
			}
			if u.traffic.After(last) {
				last = u.traffic
			}
		}
	}
	return last
}

// Online reports whether the user is online at now.
func (t *Tracker) Online(id int64, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.online(id, now)
}

// OnlineIDs lists the users online at now, sorted.
func (t *Tracker) OnlineIDs(now time.Time) []int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.onlineIDs(now)
}

func (t *Tracker) onlineIDs(now time.Time) []int64 {
	set := map[int64]bool{}
	for _, s := range t.sources {
		for id, u := range s.users {
			if !set[id] && t.onlineIn(s, u, now) {
				set[id] = true
			}
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// LastOnline is the later of the stored last_online_at and what the tracker
// saw since its last write (nil when neither exists).
func (t *Tracker) LastOnline(id int64, stored *time.Time) *time.Time {
	t.mu.Lock()
	last := t.lastSeen(id)
	t.mu.Unlock()
	if last.IsZero() || (stored != nil && !last.After(*stored)) {
		return stored
	}
	l := last.UTC()
	return &l
}

// Write is one last_online_at update.
type Write struct {
	At  time.Time
	IDs []int64
}

// Due works out which last_online_at values to store at now and marks them
// written: a user who came online or went offline at once, a user who stays
// online once per WriteEvery, and a user seen briefly between two polls
// (a short connection) once as well.
func (t *Tracker) Due(now time.Time) []Write {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state == nil {
		t.state = map[int64]*flushed{}
	}
	t.prune(now)
	ids := map[int64]bool{}
	for _, s := range t.sources {
		for id := range s.users {
			ids[id] = true
		}
	}
	for id := range t.state {
		ids[id] = true
	}
	by := map[time.Time][]int64{}
	for id := range ids {
		on := t.online(id, now)
		last := t.lastSeen(id)
		f := t.state[id]
		if f == nil {
			f = &flushed{}
			t.state[id] = f
		}
		var at time.Time
		seenAt := last // online users: the latest report that saw them
		if seenAt.IsZero() || seenAt.After(now) {
			seenAt = now
		}
		switch {
		case on && !f.online: // came online
			at = seenAt
		case on && now.Sub(f.at) >= t.WriteEvery:
			at = seenAt
		case !on && f.online: // went offline: the moment last seen
			at = last
		case !on && last.After(f.written): // seen between polls only
			at = last
		}
		f.online = on
		if !at.IsZero() && at.After(f.written) {
			at = at.UTC()
			f.written, f.at = at, now
			by[at] = append(by[at], id)
		}
		if !on && last.IsZero() { // no source remembers the user any more
			delete(t.state, id)
		}
	}
	return writes(by)
}

// prune drops users a source has not seen for a long time, so memory stays
// bounded by the users online recently.
func (t *Tracker) prune(now time.Time) {
	keep := t.TrafficWindow + t.LiveStale + time.Minute
	for name, s := range t.sources {
		for id, u := range s.users {
			l := u.live
			if u.traffic.After(l) {
				l = u.traffic
			}
			if now.Sub(l) > keep {
				delete(s.users, id)
			}
		}
		if len(s.users) == 0 && (s.liveAt.IsZero() || now.Sub(s.liveAt) > keep) {
			delete(t.sources, name)
		}
	}
}

// Run stores the due values every `every` until ctx ends, then stores the
// last moment every user was seen.
func (t *Tracker) Run(ctx context.Context, gdb *gorm.DB, every time.Duration) {
	tk := time.NewTicker(every)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := Store(gdb, t.Final()); err != nil {
				slog.Warn("store last online times", "err", err)
			}
			return
		case <-tk.C:
			if err := Store(gdb, t.Due(time.Now().UTC())); err != nil {
				slog.Warn("store last online times", "err", err)
			}
		}
	}
}

// Final lists, for every user seen since their last write, the moment they
// were last seen (on shutdown).
func (t *Tracker) Final() []Write {
	t.mu.Lock()
	defer t.mu.Unlock()
	by := map[time.Time][]int64{}
	ids := map[int64]bool{}
	for _, s := range t.sources {
		for id := range s.users {
			ids[id] = true
		}
	}
	for id := range ids {
		last := t.lastSeen(id)
		if f := t.state[id]; f != nil && !last.After(f.written) {
			continue
		}
		if !last.IsZero() {
			l := last.UTC()
			by[l] = append(by[l], id)
		}
	}
	return writes(by)
}

func writes(by map[time.Time][]int64) []Write {
	out := make([]Write, 0, len(by))
	for at, l := range by {
		sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })
		out = append(out, Write{At: at, IDs: l})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Store writes the due last_online_at values. A value never moves back.
func Store(gdb *gorm.DB, ws []Write) error {
	for _, w := range ws {
		for i := 0; i < len(w.IDs); i += 500 {
			j := min(i+500, len(w.IDs))
			err := gdb.Model(&db.User{}).Where("id IN ?", w.IDs[i:j]).
				Where("last_online_at IS NULL OR last_online_at < ?", w.At).
				UpdateColumn("last_online_at", w.At).Error
			if err != nil {
				return err
			}
		}
	}
	return nil
}
