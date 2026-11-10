package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/logbuf"
	"github.com/veilonhq/veilon/panel/internal/stats"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/users"
)

func (s *Server) systemStats(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	h := stats.Host{}
	if s.Sampler != nil {
		h = s.Sampler.Snapshot()
	}
	type cnt struct {
		Status string
		N      int64
	}
	var cs []cnt
	s.DB.Model(&db.User{}).Select("status, COUNT(*) AS n").Group("status").Scan(&cs)
	by := map[string]int64{}
	var total int64
	for _, c := range cs {
		by[c.Status] = c.N
		total += c.N
	}
	var online int64
	s.DB.Model(&db.User{}).Where("last_online_at > ?", time.Now().UTC().Add(-users.OnlineWindow)).Count(&online)
	// The total is the sum of every user's counters: the same source and
	// units (counted bytes) as the users list.
	up, down, err := usage.Totals(s.DB)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	uUp, uDown := usage.RateAll(usage.LiveWindow)
	writeJSON(w, 200, map[string]any{
		"cpu_percent": h.CPUPercent, "mem_used": h.MemUsed, "mem_total": h.MemTotal,
		"disk_used": h.DiskUsed, "disk_total": h.DiskTotal, "uptime_sec": h.UptimeSec,
		"swap_used": h.SwapUsed, "swap_total": h.SwapTotal,
		"cpu_cores": h.CPUCores, "cpu_model": h.CPUModel, "load_avg": h.LoadAvg,
		"net_up_bps": h.NetUpBps, "net_down_bps": h.NetDownBps,
		"traffic_up_bps": round2(uUp), "traffic_down_bps": round2(uDown),
		"net_sampled_at": h.NetSampledAt, "net_interval_ms": h.NetIntervalMs,
		"users_total": total, "users_active": by[db.StatusActive], "users_online": online,
		"users_expired": by[db.StatusExpired], "users_limited": by[db.StatusLimited],
		"users_disabled": by[db.StatusDisabled], "users_on_hold": by[db.StatusOnHold],
		"traffic_total_up": up, "traffic_total_down": down,
		"core": s.Core.Status(), "cores": s.Core.Statuses(), "stopped_cores": s.stoppedCores(),
	})
}

func (s *Server) systemTraffic(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	q := r.URL.Query()
	window, bucket, ok := stats.RangeSpec(q.Get("range"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_request", "range must be 24h, 7d or 30d")
		return
	}
	b, ok := stats.BucketSpec(q.Get("bucket"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_request", "bucket must be 5m, 15m, 1h, 6h or 1d")
		return
	}
	if b > 0 {
		bucket = b
	}
	if bucket > window || window/bucket > stats.MaxPoints {
		writeErr(w, http.StatusBadRequest, "bad_request", "bucket does not fit the range")
		return
	}
	// server: "" or "all" = every server, "main" (or 0) = the main server,
	// a node id = that node.
	var server *int64
	switch v := q.Get("server"); v {
	case "", "all":
	case "main":
		server = new(int64)
	default:
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", "server must be all, main or a node id")
			return
		}
		server = &id
	}
	loc := users.LoadLocation(s.Settings().Timezone)
	pts, servers, err := stats.TrafficByServer(s.DB, window, bucket, time.Now(), loc, server)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	var tu, td int64
	for _, p := range pts {
		tu += p.Up
		td += p.Down
	}
	writeJSON(w, 200, map[string]any{
		"points": pts, "bucket_sec": int(bucket.Seconds()), "range": window.String(),
		"timezone": loc.String(), "total_up": tu, "total_down": td,
		// each server's share of the window (sums to the all-servers total)
		"servers": servers,
	})
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func (s *Server) coreRestart(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	s.Core.Restart()
	audit.Log(s.DB, a, "core.restart", "xray", s.clientIP(r))
	writeJSON(w, 200, map[string]any{"ok": true})
}

// maxLogLines is the most lines one source keeps (and returns).
const maxLogLines = 2000

// coreLogs returns recent log lines of one source (an engine, "panel") or
// of every source ("all"), oldest first. entries carry the RFC 3339 UTC time
// each line was written, its level and source; lines is the same as plain
// text (kept for older clients).
func (s *Server) coreLogs(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 {
		n = 200
	}
	if n > maxLogLines {
		n = maxLogLines
	}
	src := r.URL.Query().Get("core")
	var entries []logbuf.Entry
	switch src {
	case "panel":
		entries = s.panelLogs(n)
	case "all":
		names := []string{"xray"}
		if nl, ok := s.Core.(interface{ Names() []string }); ok {
			names = nl.Names()
		}
		for _, name := range names {
			es, err := s.engineLogs(name, n)
			if err == nil {
				entries = append(entries, es...)
			}
		}
		entries = append(entries, s.panelLogs(n)...)
		logbuf.SortByTime(entries)
		if len(entries) > n {
			entries = entries[len(entries)-n:]
		}
	default:
		es, err := s.engineLogs(src, n)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		entries = es
	}
	if entries == nil {
		entries = []logbuf.Entry{}
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.Msg
		if e.T != nil {
			lines[i] = e.T.UTC().Format(time.RFC3339Nano) + " " + e.Msg
		}
	}
	writeJSON(w, 200, map[string]any{"lines": lines, "entries": entries})
}

func (s *Server) panelLogs(n int) []logbuf.Entry {
	if s.PanelLogs == nil {
		return nil
	}
	return s.PanelLogs.Last(n)
}

func (s *Server) engineLogs(name string, n int) ([]logbuf.Entry, error) {
	if le, ok := s.Core.(interface {
		LogEntries(string, int) ([]logbuf.Entry, error)
	}); ok {
		return le.LogEntries(name, n)
	}
	lines, err := s.Core.Logs(name, n)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "xray"
	}
	return logbuf.FromLines(name, lines, time.Local), nil
}

func (s *Server) coreConfig(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	b, err := s.Core.GenerateConfig(r.URL.Query().Get("core"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(b) == 0 {
		b = []byte("{}")
	}
	if json.Valid(b) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	_, _ = w.Write(b)
}
