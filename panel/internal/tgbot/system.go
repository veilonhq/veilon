package tgbot

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

type coreStatus struct {
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Running   bool       `json:"running"`
	Enabled   bool       `json:"enabled"`
	Stopped   bool       `json:"stopped"`
	Error     string     `json:"error"`
	StartedAt *time.Time `json:"started_at"`
}

type sysStats struct {
	CPU          float64      `json:"cpu_percent"`
	Cores        int          `json:"cpu_cores"`
	Load         []float64    `json:"load_avg"`
	MemUsed      int64        `json:"mem_used"`
	MemTotal     int64        `json:"mem_total"`
	DiskUsed     int64        `json:"disk_used"`
	DiskTotal    int64        `json:"disk_total"`
	Uptime       int64        `json:"uptime_sec"`
	NetUp        float64      `json:"net_up_bps"`
	NetDown      float64      `json:"net_down_bps"`
	TotalUp      int64        `json:"traffic_total_up"`
	TotalDown    int64        `json:"traffic_total_down"`
	UsersTotal   int64        `json:"users_total"`
	UsersActive  int64        `json:"users_active"`
	UsersOnline  int64        `json:"users_online"`
	UsersExpired int64        `json:"users_expired"`
	UsersLimited int64        `json:"users_limited"`
	CoreList     []coreStatus `json:"cores"`
}

var coreNames = map[string]string{"xray": "Xray", "singbox": "sing-box", "sing-box": "sing-box", "mtg": "MTProto", "awg": "AmneziaWG", "amneziawg": "AmneziaWG"}

func coreLabel(n string) string {
	if l, ok := coreNames[n]; ok {
		return l
	}
	return n
}

func pct(used, total int64) string {
	if total <= 0 {
		return ""
	}
	return fa(strconv.FormatFloat(float64(used)*100/float64(total), 'f', 0, 64)) + T("٪")
}

func uptime(sec int64) string {
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	switch {
	case d > 0:
		return faInt(d) + T(" روز و ") + faInt(h) + T(" ساعت")
	case h > 0:
		return faInt(h) + T(" ساعت و ") + faInt(m) + T(" دقیقه")
	default:
		return faInt(m) + T(" دقیقه")
	}
}

func speed(bps float64) string { return size(int64(bps)) + "/s" }

const logLinesShown = 25

func init() {
	on("sys", sysView)
	on("sl", sysLogs)
	on("slf", sysLogsFile)
	on("sr", func(r *req, _ []string) {
		confirm(r, T("راه اندازی دوباره هسته"),
			T("هسته ها (Xray و بقیه) دوباره راه اندازی شوند؟\nاتصال همه کاربران چند ثانیه قطع می شود."), "sry", "sys")
	})
	on("sry", func(r *req, _ []string) {
		r.progress(T("در حال راه اندازی دوباره هسته"))
		if err := r.call("POST", "/core/restart", nil, nil); err != nil {
			r.fail(err, "sys")
			return
		}
		r.toast(T("♻️ هسته دوباره راه اندازی شد"))
		// The engines take a moment to come back.
		time.Sleep(2 * time.Second)
		sysView(r, nil)
	})
}

func sysView(r *req, _ []string) {
	var s sysStats
	if err := r.call("GET", "/system/stats", nil, &s); err != nil {
		r.fail(err, "home")
		return
	}
	t := header("📊", T("وضعیت سرور"))
	t += T("🧠 پردازنده: <b>") + fa(strconv.FormatFloat(s.CPU, 'f', 0, 64)) + T("٪</b>")
	if s.Cores > 0 {
		t += " · " + faInt(s.Cores) + T(" هسته")
	}
	if len(s.Load) > 0 {
		t += T(" · بار: ") + fa(strconv.FormatFloat(s.Load[0], 'f', 2, 64))
	}
	t += "\n"
	t += T("💾 رم: ") + bar(s.MemUsed, s.MemTotal) + " " + pct(s.MemUsed, s.MemTotal) + " · " + size(s.MemUsed) + T(" از ") + size(s.MemTotal) + "\n"
	t += T("🗄 دیسک: ") + bar(s.DiskUsed, s.DiskTotal) + " " + pct(s.DiskUsed, s.DiskTotal) + " · " + size(s.DiskUsed) + T(" از ") + size(s.DiskTotal) + "\n"
	t += T("⏱ روشن از: ") + uptime(s.Uptime) + "\n"
	t += T("🌐 سرعت شبکه: ⬆️ ") + speed(s.NetUp) + " · ⬇️ " + speed(s.NetDown) + "\n"
	t += T("📶 ترافیک کل کاربران: ⬆️ ") + size(s.TotalUp) + " · ⬇️ " + size(s.TotalDown) + "\n"
	t += T("👥 کاربران: ") + faInt(s.UsersTotal) + T(" · 🟢 آنلاین: <b>") + faInt(s.UsersOnline) + T("</b> · فعال: ") + faInt(s.UsersActive) + "\n"
	if s.UsersExpired+s.UsersLimited > 0 {
		t += T("⌛️ منقضی: ") + faInt(s.UsersExpired) + T(" · 📛 حجم تمام: ") + faInt(s.UsersLimited) + "\n"
	}
	if len(s.CoreList) > 0 {
		t += T("\n<b>هسته ها</b>\n")
		for _, c := range s.CoreList {
			mark := "🟢"
			switch {
			case c.Stopped:
				mark = "⛔️"
			case !c.Enabled && !c.Running:
				mark = "▫️"
			case !c.Running:
				mark = "🔴"
			}
			t += mark + " " + esc(coreLabel(c.Name))
			if c.Version != "" {
				t += " " + code(c.Version)
			}
			if !c.Enabled && !c.Running && !c.Stopped {
				t += T(" · بدون ورودی")
			}
			if c.Error != "" && !c.Running {
				t += "\n   ⚠️ " + esc(cut(c.Error, 200))
			}
			t += "\n"
		}
	}
	t += T("\nپنل: ") + code(r.b.Version) + " · " + jdate(r.b.now(), r.b.loc())
	r.show(view{text: t, kb: Keyboard{
		{btn(T("🔄 تازه کردن"), "sys"), btn(T("📜 لاگ ها"), "sl", "all")},
		{btn(T("♻️ راه اندازی دوباره هسته"), "sr")},
		nav("home"),
	}})
}

func (r *req) logLines(src string, n int) ([]string, error) {
	var res struct {
		Lines []string `json:"lines"`
	}
	q := url.Values{"core": {src}, "lines": {strconv.Itoa(n)}}
	if err := r.call("GET", "/core/logs?"+q.Encode(), nil, &res); err != nil {
		return nil, err
	}
	return res.Lines, nil
}

func logSources(r *req) []string {
	srcs := []string{"all"}
	var s sysStats
	if r.call("GET", "/system/stats", nil, &s) == nil {
		for _, c := range s.CoreList {
			if c.Enabled || c.Running {
				srcs = append(srcs, c.Name)
			}
		}
	}
	return append(srcs, "panel")
}

func srcLabel(s string) string {
	switch s {
	case "all":
		return T("همه")
	case "panel":
		return T("پنل")
	}
	return coreLabel(s)
}

func sysLogs(r *req, a []string) {
	src := argStr(a, 0)
	if src == "" {
		src = "all"
	}
	lines, err := r.logLines(src, logLinesShown)
	if err != nil {
		r.fail(err, "sys")
		return
	}
	var srcRow []Button
	for _, s := range logSources(r) {
		label := srcLabel(s)
		if s == src {
			label = "• " + label
		}
		srcRow = append(srcRow, btn(label, "sl", s))
	}
	kb := chunk(srcRow, 4)
	kb = append(kb,
		[]Button{btn(T("🔄 تازه کردن"), "sl", src), btn(T("📄 فایل ۱۰۰۰ خط"), "slf", src)},
		nav("sys"))
	t := header("📜", T("لاگ ها · ")+srcLabel(src))
	if len(lines) == 0 {
		r.show(view{text: t + T("لاگی ثبت نشده است."), kb: kb})
		return
	}
	body := strings.Join(lines, "\n")
	// A screen holds at most 4096 characters: longer logs go as a file.
	if len([]rune(t+body)) > 3500 {
		if err := r.cl.SendDocument(r.ctx, r.chat, logFileName(src, r), []byte(body+"\n"), T("📜 آخرین ")+faInt(len(lines))+T(" خط لاگ · ")+esc(srcLabel(src))); err != nil {
			r.fail(err, "sys")
			return
		}
		r.fresh(view{text: t + T("آخرین ") + faInt(len(lines)) + T(" خط لاگ طولانی بود و به صورت فایل فرستاده شد."), kb: kb})
		return
	}
	r.show(view{text: t + T("آخرین ") + faInt(len(lines)) + T(" خط:\n<pre>") + esc(body) + "</pre>", kb: kb})
}

func logFileName(src string, r *req) string {
	return "veilon-" + src + "-" + r.b.now().Format("20060102-150405") + ".log"
}

func sysLogsFile(r *req, a []string) {
	src := argStr(a, 0)
	r.progress(T("در حال آماده کردن فایل لاگ"))
	lines, err := r.logLines(src, 1000)
	if err != nil {
		r.fail(err, cbd("sl", src))
		return
	}
	if len(lines) == 0 {
		lines = []string{"(empty)"}
	}
	if err := r.cl.SendDocument(r.ctx, r.chat, logFileName(src, r), []byte(strings.Join(lines, "\n")+"\n"),
		"📜 "+Tf("%s خط لاگ · %s", faInt(len(lines)), esc(srcLabel(src)))); err != nil {
		r.fail(err, cbd("sl", src))
		return
	}
	r.fresh(view{text: header("📜", T("لاگ ها")) + T("فایل لاگ فرستاده شد."), kb: Keyboard{{btn(T("📜 لاگ ها"), "sl", src)}, nav("sys")}})
}
