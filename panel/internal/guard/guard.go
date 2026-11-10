package guard

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// lookPath finds nft and tc (replaced in tests).
var lookPath = exec.LookPath

// BanTTL is how long an IP over a user's IP limit is dropped.
const BanTTL = 3 * time.Minute

// Guard enforces IP limits and speed limits and writes the connection log.
type Guard struct {
	DB *gorm.DB
	// GeoIP is the path of geoip.dat for countries ("" = none).
	GeoIP   string
	Tracker *Tracker
	Run     Runner
	// Dev is the interface to shape ("" = default route interface).
	Dev string
	Now func() time.Time

	mu        sync.Mutex
	limits    map[int64]limit
	ports     []int
	fwReady   bool
	fwFailed  bool
	tcSig     string
	tcFailed  bool
	banned    map[string]time.Time
	countries map[string]string
	// blocked holds the IPs an admin blocked per user ("id ip"); they are
	// refused by the cores, so they never count as live for the limits.
	blocked  map[string]bool
	sbParser *SingboxParser
}

type limit struct {
	ipLimit  int
	down, up int64
}

// New creates a guard.
func New(gdb *gorm.DB, geoip string) *Guard {
	return &Guard{DB: gdb, GeoIP: geoip, Tracker: NewTracker(), Run: ExecRunner,
		banned: map[string]time.Time{}, countries: map[string]string{}}
}

func (g *Guard) now() time.Time {
	if g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

// ObserveEmail records an access log hit for an Xray email ("ID.name").
func (g *Guard) ObserveEmail(email, ip string) {
	idStr, _, _ := strings.Cut(email, ".")
	var id int64
	if _, err := fmt.Sscan(idStr, &id); err != nil || id <= 0 {
		return
	}
	g.Tracker.Observe(id, ip, g.now())
}

// ObserveLine parses an Xray access log line.
func (g *Guard) ObserveLine(line string) {
	if email, ip, ok := ParseAccessLine(line); ok {
		g.ObserveEmail(email, ip)
	}
}

// RunLoop enforces every 10 seconds and refreshes limits and the
// connection log every minute until ctx is done.
func (g *Guard) RunLoop(ctx context.Context) {
	g.safe("refresh", g.Refresh)
	fast := time.NewTicker(10 * time.Second)
	slow := time.NewTicker(time.Minute)
	defer fast.Stop()
	defer slow.Stop()
	for {
		select {
		case <-ctx.Done():
			g.safe("flush", g.Flush)
			g.safe("clear", g.clearTC)
			return
		case <-fast.C:
			g.safe("enforce", g.Enforce)
		case <-slow.C:
			g.safe("flush", g.Flush)
			g.safe("refresh", g.Refresh)
		}
	}
}

// safe runs fn and logs instead of crashing the service if it panics.
func (g *Guard) safe(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("guard: recovered from panic", "step", what, "panic", r)
		}
	}()
	fn()
}

// Refresh reloads per-user limits and the inbound ports.
func (g *Guard) Refresh() {
	var us []db.User
	g.DB.Select("id", "status", "ip_limit", "speed_down_kbps", "speed_up_kbps").
		Where("ip_limit > 0 OR speed_down_kbps > 0 OR speed_up_kbps > 0").Find(&us)
	lim := make(map[int64]limit, len(us))
	for _, u := range us {
		lim[u.ID] = limit{ipLimit: u.IPLimit, down: u.SpeedDownKbps, up: u.SpeedUpKbps}
	}
	// Only the ports this server listens on (inbounds that run on nodes
	// only are enforced by their nodes).
	var ports []int
	g.DB.Model(&db.Inbound{}).Where("enabled = ? AND NOT (serve_main = ? AND id IN (?))", true, false,
		g.DB.Table("node_inbounds").Select("inbound_id")).Pluck("port", &ports)
	var bl []db.UserIP
	g.DB.Select("user_id", "ip").Where("blocked = ?", true).Find(&bl)
	blocked := make(map[string]bool, len(bl))
	for _, b := range bl {
		blocked[blockKey(b.UserID, b.IP)] = true
	}
	g.mu.Lock()
	g.limits = lim
	g.blocked = blocked
	changed := !sameInts(ports, g.ports)
	g.ports = ports
	g.mu.Unlock()
	if changed {
		g.mu.Lock()
		g.fwReady = false
		g.mu.Unlock()
	}
}

// Limit sets one user's limits directly (used after edits and in tests).
func (g *Guard) Limit(id int64, ipLimit int, down, up int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.limits == nil {
		g.limits = map[int64]limit{}
	}
	if ipLimit == 0 && down == 0 && up == 0 {
		delete(g.limits, id)
		return
	}
	g.limits[id] = limit{ipLimit: ipLimit, down: down, up: up}
}

func blockKey(id int64, ip string) string { return fmt.Sprintf("%d %s", id, ip) }

// SetBlocked replaces the per-user blocked IPs (used in tests).
func (g *Guard) SetBlocked(m map[int64][]string) {
	b := map[string]bool{}
	for id, ips := range m {
		for _, ip := range ips {
			b[blockKey(id, ip)] = true
		}
	}
	g.mu.Lock()
	g.blocked = b
	g.mu.Unlock()
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Excess returns the live IPs beyond limit (the newest sessions).
func Excess(live []string, limit int) []string {
	if limit <= 0 || len(live) <= limit {
		return nil
	}
	return live[limit:]
}

// Plan returns the bans and the shaping plan for the current state.
func (g *Guard) Plan() (bans []string, shapes []Shape) {
	now := g.now()
	g.mu.Lock()
	lim := make(map[int64]limit, len(g.limits))
	for k, v := range g.limits {
		lim[k] = v
	}
	blocked := g.blocked
	g.mu.Unlock()
	for _, id := range g.Tracker.Users(now) {
		l, ok := lim[id]
		if !ok {
			continue
		}
		live := g.Tracker.Live(id, now)
		if len(blocked) > 0 {
			// A blocked address is refused by the cores but may keep
			// retrying; it must not take a slot of the IP limit.
			kept := live[:0:0]
			for _, ip := range live {
				if !blocked[blockKey(id, ip)] {
					kept = append(kept, ip)
				}
			}
			live = kept
		}
		ex := Excess(live, l.ipLimit)
		bans = append(bans, ex...)
		if l.down > 0 || l.up > 0 {
			keep := live[:len(live)-len(ex)]
			if len(keep) > 0 {
				shapes = append(shapes, Shape{UserID: id, DownKbps: l.down, UpKbps: l.up, IPs: append([]string(nil), keep...)})
			}
		}
	}
	return bans, shapes
}

// Enforce applies bans and shaping.
func (g *Guard) Enforce() {
	bans, shapes := g.Plan()
	g.applyBans(bans)
	g.applyTC(shapes)
}

func (g *Guard) applyBans(bans []string) {
	now := g.now()
	g.mu.Lock()
	var fresh []string
	for _, ip := range bans {
		if until, ok := g.banned[ip]; !ok || now.After(until.Add(-30*time.Second)) {
			fresh = append(fresh, ip)
			g.banned[ip] = now.Add(BanTTL)
		}
	}
	for ip, until := range g.banned {
		if now.After(until) {
			delete(g.banned, ip)
		}
	}
	ready, failed, ports := g.fwReady, g.fwFailed, append([]int(nil), g.ports...)
	g.mu.Unlock()
	if len(fresh) == 0 && ready {
		return
	}
	if !ready {
		if len(fresh) == 0 {
			return // build the table lazily, on the first ban
		}
		if _, err := lookPath("nft"); err != nil {
			if !failed {
				slog.Warn("ip limit: nft not found; install nftables to enforce IP limits")
			}
			g.mu.Lock()
			g.fwFailed = true
			g.mu.Unlock()
			return
		}
		if out, err := g.Run(FirewallScript(ports), "nft", "-f", "-"); err != nil {
			if !failed {
				slog.Warn("ip limit: nft setup failed", "err", err, "out", strings.TrimSpace(out))
			}
			g.mu.Lock()
			g.fwFailed = true
			g.mu.Unlock()
			return
		}
		g.mu.Lock()
		g.fwReady, g.fwFailed = true, false
		g.mu.Unlock()
	}
	if out, err := g.Run(BanScript(fresh, BanTTL), "nft", "-f", "-"); err != nil {
		slog.Warn("ip limit: ban failed", "err", err, "out", strings.TrimSpace(out))
		// Retry next round, rebuilding the table in case it was removed
		// (e.g. a firewall reload flushed the ruleset).
		g.mu.Lock()
		for _, ip := range fresh {
			delete(g.banned, ip)
		}
		g.fwReady = false
		g.mu.Unlock()
		return
	}
	slog.Info("ip limit: dropped extra client IPs", "ips", fresh, "for", BanTTL)
}

func (g *Guard) dev() string {
	if g.Dev != "" {
		return g.Dev
	}
	return DefaultInterface()
}

func shapeSig(shapes []Shape) string {
	var b strings.Builder
	for _, s := range shapes {
		fmt.Fprintf(&b, "%d:%d:%d:%s;", s.UserID, s.DownKbps, s.UpKbps, strings.Join(s.IPs, ","))
	}
	return b.String()
}

func (g *Guard) applyTC(shapes []Shape) {
	sig := shapeSig(shapes)
	g.mu.Lock()
	same := sig == g.tcSig
	failed := g.tcFailed
	g.mu.Unlock()
	if same {
		return
	}
	dev := g.dev()
	if dev == "" {
		return
	}
	if _, err := lookPath("tc"); err != nil {
		if !failed {
			slog.Warn("speed limit: tc not found; install iproute2 to enforce speed limits")
		}
		g.mu.Lock()
		g.tcFailed = true
		g.mu.Unlock()
		return
	}
	// Deleting a qdisc that is not there is an error, so the deletes run on
	// their own and their result is ignored; only the adds are checked.
	_, _ = g.Run(TCDeleteScript(dev), "tc", "-force", "-batch", "-")
	if len(shapes) > 0 {
		if out, err := g.Run(TCScript(dev, shapes), "tc", "-batch", "-"); err != nil {
			if !failed {
				slog.Warn("speed limit: tc failed", "dev", dev, "err", err, "out", strings.TrimSpace(out))
			}
			// Leave no half-built tree behind and retry next round.
			_, _ = g.Run(TCDeleteScript(dev), "tc", "-force", "-batch", "-")
			g.mu.Lock()
			g.tcSig, g.tcFailed = "", true
			g.mu.Unlock()
			return
		}
	}
	g.mu.Lock()
	g.tcSig, g.tcFailed = sig, false
	g.mu.Unlock()
	if len(shapes) > 0 {
		slog.Info("speed limit: shaping updated", "dev", dev, "users", len(shapes))
	}
}

func (g *Guard) clearTC() {
	g.mu.Lock()
	had := g.tcSig != ""
	g.mu.Unlock()
	if had {
		if dev := g.dev(); dev != "" {
			_, _ = g.Run(TCDeleteScript(dev), "tc", "-force", "-batch", "-")
		}
	}
}

// Flush writes IPs seen since the last flush to the connection log.
func (g *Guard) Flush() {
	pend := g.Tracker.Drain(g.now(), 10*time.Minute)
	if len(pend) == 0 {
		return
	}
	var unknown []string
	g.mu.Lock()
	for _, p := range pend {
		if _, ok := g.countries[p.IP]; !ok {
			unknown = append(unknown, p.IP)
		}
	}
	g.mu.Unlock()
	if len(unknown) > 0 && g.GeoIP != "" {
		found := Countries(g.GeoIP, unknown)
		g.mu.Lock()
		for _, ip := range unknown {
			g.countries[ip] = found[ip]
		}
		if len(g.countries) > 50000 {
			g.countries = map[string]string{}
		}
		g.mu.Unlock()
	}
	g.mu.Lock()
	cc := make(map[string]string, len(pend))
	for _, p := range pend {
		cc[p.IP] = g.countries[p.IP]
	}
	g.mu.Unlock()
	err := g.DB.Transaction(func(tx *gorm.DB) error {
		for _, p := range pend {
			row := db.UserIP{UserID: p.UserID, IP: p.IP, Country: cc[p.IP], FirstSeen: p.First, LastSeen: p.Last, Hits: p.Hits}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "user_id"}, {Name: "ip"}},
				DoUpdates: clause.Assignments(map[string]any{
					"last_seen": p.Last, "hits": gorm.Expr("user_ips.hits + ?", p.Hits),
				}),
			}).Create(&row).Error; err != nil {
				return err
			}
			if cc[p.IP] != "" {
				tx.Model(&db.UserIP{}).Where("user_id = ? AND ip = ? AND country = ''", p.UserID, p.IP).Update("country", cc[p.IP])
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("connection log write failed", "err", err)
	}
}
