package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/stats"
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
	up, _ := strconv.ParseInt(db.GetSetting(s.DB, db.KeyTrafficUp), 10, 64)
	down, _ := strconv.ParseInt(db.GetSetting(s.DB, db.KeyTrafficDown), 10, 64)
	writeJSON(w, 200, map[string]any{
		"cpu_percent": h.CPUPercent, "mem_used": h.MemUsed, "mem_total": h.MemTotal,
		"disk_used": h.DiskUsed, "disk_total": h.DiskTotal, "uptime_sec": h.UptimeSec,
		"swap_used": h.SwapUsed, "swap_total": h.SwapTotal,
		"cpu_cores": h.CPUCores, "cpu_model": h.CPUModel, "load_avg": h.LoadAvg,
		"net_up_bps": h.NetUpBps, "net_down_bps": h.NetDownBps,
		"users_total": total, "users_active": by[db.StatusActive], "users_online": online,
		"users_expired": by[db.StatusExpired], "users_limited": by[db.StatusLimited],
		"users_disabled": by[db.StatusDisabled], "users_on_hold": by[db.StatusOnHold],
		"traffic_total_up": up, "traffic_total_down": down,
		"core": s.Core.Status(), "cores": s.Core.Statuses(),
	})
}

func (s *Server) systemTraffic(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	window, bucket, ok := stats.RangeSpec(r.URL.Query().Get("range"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_request", "range must be 24h, 7d or 30d")
		return
	}
	pts, err := stats.Traffic(s.DB, window, bucket, time.Now())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"points": pts, "bucket_sec": int(bucket.Seconds())})
}

func (s *Server) coreRestart(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	s.Core.Restart()
	audit.Log(s.DB, a, "core.restart", "xray", s.clientIP(r))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) coreLogs(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 {
		n = 200
	}
	if n > 2000 {
		n = 2000
	}
	lines, err := s.Core.Logs(r.URL.Query().Get("core"), n)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"lines": lines})
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
