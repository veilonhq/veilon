// Package stats samples host metrics (CPU, memory, disk, network) and
// aggregates traffic points for charts.
package stats

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Host is a snapshot of host metrics.
type Host struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	DiskUsed   uint64  `json:"disk_used"`
	DiskTotal  uint64  `json:"disk_total"`
	SwapUsed   uint64  `json:"swap_used"`
	SwapTotal  uint64  `json:"swap_total"`
	CPUCores   int     `json:"cpu_cores"`
	CPUModel   string  `json:"cpu_model"`
	// LoadAvg is the 1, 5 and 15 minute load average.
	LoadAvg    [3]float64 `json:"load_avg"`
	UptimeSec  int64      `json:"uptime_sec"`
	NetUpBps   float64    `json:"net_up_bps"`
	NetDownBps float64    `json:"net_down_bps"`
	// NetSampledAt is when the network rates were measured (UTC; zero
	// before the second sample) and NetIntervalMs the window they cover:
	// rate = bytes moved in the window / window.
	NetSampledAt  *time.Time `json:"net_sampled_at"`
	NetIntervalMs int64      `json:"net_interval_ms"`
}

// Sampler keeps rolling CPU and network rates.
type Sampler struct {
	DiskPath string

	mu                  sync.RWMutex
	cpu, up, down       float64
	lastIdle, lastTotal uint64
	lastRx, lastTx      uint64
	lastNet             time.Time
	netAt               time.Time
	netWindow           time.Duration
}

// Run samples every interval until ctx is done.
func (s *Sampler) Run(ctx context.Context, every time.Duration) {
	s.sample()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample()
		}
	}
}

func (s *Sampler) sample() {
	idle, total := readCPU()
	rx, tx := readNet()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastTotal > 0 && total > s.lastTotal {
		dt := float64(total - s.lastTotal)
		di := float64(idle - s.lastIdle)
		s.cpu = (1 - di/dt) * 100
		if s.cpu < 0 {
			s.cpu = 0
		}
	}
	s.lastIdle, s.lastTotal = idle, total
	if !s.lastNet.IsZero() {
		sec := now.Sub(s.lastNet).Seconds()
		if sec > 0 && rx >= s.lastRx && tx >= s.lastTx {
			s.down = float64(rx-s.lastRx) / sec
			s.up = float64(tx-s.lastTx) / sec
			s.netAt, s.netWindow = now.UTC(), now.Sub(s.lastNet)
		}
	}
	s.lastRx, s.lastTx, s.lastNet = rx, tx, now
}

// Snapshot returns current host metrics.
func (s *Sampler) Snapshot() Host {
	s.mu.RLock()
	h := Host{CPUPercent: round2(s.cpu), NetUpBps: round2(s.up), NetDownBps: round2(s.down), NetIntervalMs: s.netWindow.Milliseconds()}
	if !s.netAt.IsZero() {
		at := s.netAt
		h.NetSampledAt = &at
	}
	s.mu.RUnlock()
	m := readMemInfo()
	h.MemTotal, h.MemUsed, h.SwapTotal, h.SwapUsed = m.total, m.used, m.swapTotal, m.swapUsed
	h.CPUCores, h.CPUModel = cpuInfo()
	h.LoadAvg = readLoadAvg()
	p := s.DiskPath
	if p == "" {
		p = "/"
	}
	var fs syscall.Statfs_t
	if syscall.Statfs(p, &fs) == nil {
		h.DiskTotal = fs.Blocks * uint64(fs.Bsize)
		h.DiskUsed = (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			v, _ := strconv.ParseFloat(f[0], 64)
			h.UptimeSec = int64(v)
		}
	}
	return h
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func readCPU() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0
	}
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	return idle, total
}

type memInfo struct{ total, used, swapTotal, swapUsed uint64 }

func readMemInfo() memInfo {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memInfo{}
	}
	defer f.Close()
	return parseMemInfo(bufio.NewScanner(f))
}

func parseMemInfo(sc *bufio.Scanner) memInfo {
	var m memInfo
	var avail, swapFree uint64
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			m.total = v * 1024
		case "MemAvailable:":
			avail = v * 1024
		case "SwapTotal:":
			m.swapTotal = v * 1024
		case "SwapFree:":
			swapFree = v * 1024
		}
	}
	if m.total > avail {
		m.used = m.total - avail
	}
	if m.swapTotal > swapFree {
		m.swapUsed = m.swapTotal - swapFree
	}
	return m
}

var (
	cpuOnce  sync.Once
	cpuCores int
	cpuModel string
)

// cpuInfo returns the logical CPU count and model name (read once).
func cpuInfo() (int, string) {
	cpuOnce.Do(func() {
		cpuCores = runtime.NumCPU()
		if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			n, model := parseCPUInfo(string(b))
			if n > 0 {
				cpuCores = n
			}
			cpuModel = model
		}
	})
	return cpuCores, cpuModel
}

func parseCPUInfo(s string) (cores int, model string) {
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			cores++
		case "model name", "Model", "Hardware", "cpu model":
			if model == "" {
				model = strings.Join(strings.Fields(v), " ")
			}
		}
	}
	return cores, model
}

func readLoadAvg() (l [3]float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return l
	}
	f := strings.Fields(string(b))
	for i := 0; i < 3 && i < len(f); i++ {
		v, _ := strconv.ParseFloat(f[i], 64)
		l[i] = v
	}
	return l
}

// readNet sums the byte counters of the physical interfaces. Virtual ones
// (lo, AmneziaWG/WireGuard, TUN, veth, bridges, docker) carry the same
// packets a second time, so counting them would double the proxied traffic.
func readNet() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	return parseNetDev(bufio.NewScanner(f), isVirtualIface)
}

func isVirtualIface(name string) bool {
	if name == "lo" {
		return true
	}
	p, err := filepath.EvalSymlinks(filepath.Join("/sys/class/net", name))
	if err != nil {
		return false
	}
	return strings.HasPrefix(p, "/sys/devices/virtual/")
}

func parseNetDev(sc *bufio.Scanner, virtual func(string) bool) (rx, tx uint64) {
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" || (virtual != nil && virtual(name)) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx
}

// Point is one chart point: counted user traffic (bytes) in the bucket that
// starts at T.
type Point struct {
	T    time.Time `json:"t"`
	Up   int64     `json:"up"`
	Down int64     `json:"down"`
}

// RangeSpec returns the window and default bucket size for a range name.
func RangeSpec(r string) (window, bucket time.Duration, ok bool) {
	switch r {
	case "", "24h":
		return 24 * time.Hour, 15 * time.Minute, true
	case "7d":
		return 7 * 24 * time.Hour, time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, 6 * time.Hour, true
	}
	return 0, 0, false
}

// MaxPoints bounds the number of buckets one request may ask for.
const MaxPoints = 2000

// BucketSpec parses an optional bucket override ("" keeps the default).
// Every bucket divides a day, so buckets align to local midnight.
func BucketSpec(b string) (time.Duration, bool) {
	switch b {
	case "":
		return 0, true
	case "5m":
		return 5 * time.Minute, true
	case "15m":
		return 15 * time.Minute, true
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "1d":
		return 24 * time.Hour, true
	}
	return 0, false
}

// alignStart returns the start of the bucket containing t, with buckets
// counted from local midnight in loc (so hours and days follow the panel's
// timezone, e.g. Tehran's +03:30).
func alignStart(t time.Time, bucket time.Duration, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	mid := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
	return mid.Add(lt.Sub(mid) / bucket * bucket)
}

// Edges returns the n+1 bucket boundaries of a window of n = window/bucket
// buckets that ends with the bucket containing now. Buckets follow the wall
// clock of loc and restart at every local midnight, so a "day" is one
// calendar day (23 or 25 hours on a daylight-saving change) and 6-hour
// buckets start at 00:00, 06:00, 12:00 and 18:00 local time every day.
func Edges(now time.Time, window, bucket time.Duration, loc *time.Location) []time.Time {
	if loc == nil {
		loc = time.UTC
	}
	n := int(window / bucket)
	if n < 1 || bucket <= 0 {
		return nil
	}
	lt := now.In(loc)
	days := int(window/(24*time.Hour)) + 2
	first := time.Date(lt.Year(), lt.Month(), lt.Day()-days, 0, 0, 0, 0, loc)
	var all []time.Time
	for day := first; ; {
		next := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
		for off := time.Duration(0); off < 24*time.Hour; off += bucket {
			e := time.Date(day.Year(), day.Month(), day.Day(), int(off/time.Hour), int(off%time.Hour/time.Minute), 0, 0, loc)
			if e.Before(day) || !e.Before(next) {
				continue
			}
			if k := len(all); k > 0 && !e.After(all[k-1]) {
				continue // a wall time skipped by a clock change
			}
			all = append(all, e)
		}
		if next.After(now) && len(all) > 0 && all[len(all)-1].After(now) {
			all = append(all, next)
			break
		}
		day = next
		if len(all) > 0 && all[len(all)-1].After(now) {
			break
		}
	}
	// the bucket containing now ends at the first edge after now
	endIdx := len(all) - 1
	for i, e := range all {
		if e.After(now) {
			endIdx = i
			break
		}
	}
	all = all[:endIdx+1]
	if len(all) < n+1 {
		return all
	}
	return all[len(all)-(n+1):]
}

// Traffic aggregates stored points into the buckets of Edges, zero-filled
// and in chronological order.
func Traffic(gdb *gorm.DB, window, bucket time.Duration, now time.Time, loc *time.Location) ([]Point, error) {
	pts, _, err := TrafficByServer(gdb, window, bucket, now, loc, nil)
	return pts, err
}

// ServerTotal is one server's share of the traffic in a window (counted
// bytes, the same units as users' usage). ID 0 is the main server.
type ServerTotal struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Removed: the node was deleted since.
	Removed bool  `json:"removed,omitempty"`
	Up      int64 `json:"up"`
	Down    int64 `json:"down"`
}

// TrafficByServer is Traffic for one server (server != nil: 0 = main
// server, otherwise a node id) or for all, plus every server's total over
// the same window. The totals add up exactly to the sum of all points.
func TrafficByServer(gdb *gorm.DB, window, bucket time.Duration, now time.Time, loc *time.Location, server *int64) ([]Point, []ServerTotal, error) {
	edges := Edges(now, window, bucket, loc)
	if len(edges) < 2 {
		return []Point{}, []ServerTotal{}, nil
	}
	var rows []db.TrafficPoint
	if err := gdb.Where("t >= ? AND t < ?", edges[0].UTC(), edges[len(edges)-1].UTC()).Order("t").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	by := map[int64]*ServerTotal{0: {ID: 0}}
	var ns []db.Node
	gdb.Select("id", "name").Order("id").Find(&ns)
	for _, n := range ns {
		by[n.ID] = &ServerTotal{ID: n.ID, Name: n.Name}
	}
	kept := rows[:0:0]
	for _, r := range rows {
		st := by[r.NodeID]
		if st == nil {
			st = &ServerTotal{ID: r.NodeID, Name: fmt.Sprintf("#%d", r.NodeID), Removed: true}
			by[r.NodeID] = st
		}
		st.Up += r.Up
		st.Down += r.Down
		if server == nil || *server == r.NodeID {
			kept = append(kept, r)
		}
	}
	out := make([]ServerTotal, 0, len(by))
	for _, v := range by {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return BucketizeEdges(kept, edges), out, nil
}

// BucketizeEdges groups points into the buckets between consecutive edges.
func BucketizeEdges(rows []db.TrafficPoint, edges []time.Time) []Point {
	if len(edges) < 2 {
		return []Point{}
	}
	pts := make([]Point, len(edges)-1)
	for i := range pts {
		pts[i].T = edges[i].UTC()
	}
	for _, r := range rows {
		i := sort.Search(len(edges), func(k int) bool { return edges[k].After(r.T) }) - 1
		if i >= 0 && i < len(pts) {
			pts[i].Up += r.Up
			pts[i].Down += r.Down
		}
	}
	return pts
}

// Bucketize groups points into fixed buckets between start and end.
func Bucketize(rows []db.TrafficPoint, start, end time.Time, bucket time.Duration) []Point {
	n := int(end.Sub(start) / bucket)
	pts := make([]Point, n)
	for i := range pts {
		pts[i].T = start.Add(time.Duration(i) * bucket).UTC()
	}
	for _, r := range rows {
		t := r.T
		if t.Before(start) || !t.Before(end) {
			continue
		}
		if i := int(t.Sub(start) / bucket); i < n {
			pts[i].Up += r.Up
			pts[i].Down += r.Down
		}
	}
	return pts
}
