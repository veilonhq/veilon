// Package stats samples host metrics (CPU, memory, disk, network) and
// aggregates traffic points for charts.
package stats

import (
	"bufio"
	"context"
	"os"
	"runtime"
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
}

// Sampler keeps rolling CPU and network rates.
type Sampler struct {
	DiskPath string

	mu                  sync.RWMutex
	cpu, up, down       float64
	lastIdle, lastTotal uint64
	lastRx, lastTx      uint64
	lastNet             time.Time
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
		}
	}
	s.lastRx, s.lastTx, s.lastNet = rx, tx, now
}

// Snapshot returns current host metrics.
func (s *Sampler) Snapshot() Host {
	s.mu.RLock()
	h := Host{CPUPercent: round2(s.cpu), NetUpBps: round2(s.up), NetDownBps: round2(s.down)}
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

func readNet() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" {
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

// Point is one chart point.
type Point struct {
	T    time.Time `json:"t"`
	Up   int64     `json:"up"`
	Down int64     `json:"down"`
}

// RangeSpec returns the window and bucket size for a range name.
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

// Traffic aggregates stored points into buckets covering the window,
// returning zero-filled points in chronological order.
func Traffic(gdb *gorm.DB, window, bucket time.Duration, now time.Time) ([]Point, error) {
	now = now.UTC()
	end := now.Truncate(bucket).Add(bucket)
	start := end.Add(-window)
	var rows []db.TrafficPoint
	if err := gdb.Where("t >= ? AND t < ?", start, end).Order("t").Find(&rows).Error; err != nil {
		return nil, err
	}
	return Bucketize(rows, start, end, bucket), nil
}

// Bucketize groups points into fixed buckets between start and end.
func Bucketize(rows []db.TrafficPoint, start, end time.Time, bucket time.Duration) []Point {
	n := int(end.Sub(start) / bucket)
	pts := make([]Point, n)
	for i := range pts {
		pts[i].T = start.Add(time.Duration(i) * bucket)
	}
	for _, r := range rows {
		t := r.T.UTC()
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
