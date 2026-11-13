package tgbot

import (
	"regexp"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

type updRelease struct {
	Tag   string `json:"tag"`
	Name  string `json:"name"`
	Notes string `json:"notes"`
	URL   string `json:"url"`
}

type updStatus struct {
	Tag        string     `json:"tag"`
	From       string     `json:"from"`
	Phase      string     `json:"phase"`
	Running    bool       `json:"running"`
	Message    string     `json:"message"`
	Error      string     `json:"error"`
	BackupDir  string     `json:"backup_dir"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type updInfo struct {
	Current   string      `json:"current"`
	Available *updRelease `json:"available"`
	CanUpdate bool        `json:"can_update"`
	CheckedAt *time.Time  `json:"checked_at"`
	Status    *updStatus  `json:"status"`
}

// Steps of a panel update as the updater reports them.
var updSteps = []struct{ key, label string }{
	{"queued", T("در صف")},
	{"download", T("دریافت نسخه تازه")},
	{"verify", T("بررسی checksum")},
	{"backup", T("پشتیبان از نسخه فعلی")},
	{"install", T("نصب")},
	{"restart", T("راه اندازی دوباره پنل")},
	{"health", T("بررسی سلامت")},
}

func init() {
	on("upd", updView)
	on("upc", func(r *req, _ []string) {
		r.progress(T("در حال بررسی نسخه تازه"))
		var info updInfo
		if err := r.call("POST", "/system/update/check", nil, &info); err != nil {
			r.fail(err, "upd")
			return
		}
		updDraw(r, &info)
	})
	on("upa", func(r *req, _ []string) {
		var info updInfo
		if err := r.call("GET", "/system/update", nil, &info); err != nil {
			r.fail(err, "upd")
			return
		}
		if info.Available == nil {
			updDraw(r, &info)
			return
		}
		confirm(r, T("بروزرسانی پنل"),
			T("پنل از ")+code(info.Current)+T(" به ")+code(info.Available.Tag)+T(" بروزرسانی شود؟\n\n")+
				T("پیش از نصب از برنامه و پایگاه داده پشتیبان گرفته می شود و اگر نسخه تازه سالم بالا نیاید، نسخه قبلی خودکار برمی گردد. ")+
				T("پنل و این ربات چند ثانیه از دسترس خارج می شوند و اتصال کاربران لحظه ای قطع می شود."),
			cbd("upy", info.Available.Tag), "upd")
	})
	on("upy", func(r *req, a []string) {
		tag := argStr(a, 0)
		r.progress(T("در حال شروع بروزرسانی"))
		if err := r.call("POST", "/system/update", map[string]any{"tag": tag}, nil); err != nil {
			r.fail(err, "upd")
			return
		}
		updWatch(r, nil)
	})
	on("upw", updWatch)
}

func updView(r *req, _ []string) {
	var info updInfo
	if err := r.call("GET", "/system/update", nil, &info); err != nil {
		r.fail(err, "home")
		return
	}
	if info.Status != nil && info.Status.Running {
		updWatch(r, nil)
		return
	}
	updDraw(r, &info)
}

func updDraw(r *req, info *updInfo) {
	t := header("⬆️", T("بروزرسانی پنل"))
	t += T("نسخه فعلی: ") + code(info.Current) + "\n"
	if info.CheckedAt != nil {
		t += T("آخرین بررسی: ") + jdate(*info.CheckedAt, r.b.loc()) + "\n"
	}
	if st := info.Status; st != nil && !st.Running && st.Tag != "" {
		switch st.Phase {
		case "done":
			t += T("آخرین بروزرسانی: ✅ ") + code(st.Tag) + "\n"
		case "failed", "rolled_back":
			t += T("آخرین بروزرسانی به ") + code(st.Tag) + T(": ⚠️ ناموفق")
			if st.Phase == "rolled_back" {
				t += T(" (نسخه قبلی برگشت)")
			}
			t += "\n"
		}
	}
	kb := Keyboard{}
	if rel := info.Available; rel != nil {
		t += T("\n🆕 نسخه تازه: <b>") + esc(rel.Tag) + "</b>\n"
		if notes := releaseNotes(rel.Notes, db.LoadSettings(r.b.DB).Language, 2500); notes != "" {
			t += "\n" + notes + "\n"
		}
		if info.CanUpdate {
			kb = append(kb, []Button{btn(T("⬆️ نصب ")+rel.Tag, "upa")})
		} else {
			t += T("\n⚠️ بروزرساننده روی این سرور نصب نیست. یک بار روی سرور اجرا کنید: ") + code("sudo veilon update")
		}
	} else {
		t += T("\n✅ پنل به روز است.")
	}
	kb = append(kb, []Button{btn(T("🔍 بررسی دوباره"), "upc")}, nav("home"))
	r.show(view{text: t, kb: kb})
}

// updWatch follows a running update. The panel (and this bot) restarts
// near the end; the screen then stops where it was and the admin presses
// refresh once the panel is back.
func updWatch(r *req, _ []string) {
	r.watch(5*time.Minute, func(w *req) (view, bool) {
		var info updInfo
		if err := w.call("GET", "/system/update", nil, &info); err != nil {
			return view{text: header("⚠️", T("انجام نشد")) + esc(errText(err)), kb: Keyboard{{btn(T("🔄 تازه کردن"), "upw")}, nav("home")}}, true
		}
		st := info.Status
		t := header("⬆️", T("بروزرسانی پنل"))
		if st == nil {
			return view{text: t + T("بروزرسانی ای در جریان نیست."), kb: Keyboard{nav("upd")}}, true
		}
		t += T("از ") + code(st.From) + T(" به ") + code(st.Tag) + "\n\n"
		cur := -1
		for i, s := range updSteps {
			if s.key == st.Phase {
				cur = i
			}
		}
		final := !st.Running
		for i, s := range updSteps {
			mark := "▫️"
			switch {
			case st.Phase == "done" || (cur >= 0 && i < cur):
				mark = "✅"
			case i == cur && !final:
				mark = "⏳"
			case i == cur:
				mark = "⚠️"
			}
			t += mark + " " + s.label + "\n"
		}
		switch {
		case st.Phase == "done":
			t += T("\n✅ پنل با نسخه ") + code(st.Tag) + T(" بالا آمد.")
		case final:
			t += T("\n⚠️ بروزرسانی انجام نشد")
			if st.Phase == "rolled_back" {
				t += T(" و نسخه قبلی برگردانده شد")
			}
			t += ".\n"
			if st.Error != "" {
				t += esc(cut(st.Error, 400)) + "\n"
			}
			if st.BackupDir != "" {
				t += T("پشتیبان: ") + code(st.BackupDir) + "\n"
			}
		default:
			if st.Message != "" {
				t += "\n" + esc(cut(st.Message, 200))
			}
			t += T("\n⏳ این صفحه خودکار تازه می شود. وقتی پنل دوباره راه اندازی شود، ربات چند ثانیه جواب نمی دهد؛ بعد «تازه کردن» را بزنید.")
		}
		kb := Keyboard{}
		if !final {
			kb = append(kb, []Button{btn(T("🔄 تازه کردن"), "upw")})
		}
		kb = append(kb, nav("upd"))
		return view{text: t, kb: kb}, final
	})
}

// --- release notes -------------------------------------------------------

var (
	reHeading = regexp.MustCompile(`^\s*#{1,6}\s+(.*?)\s*#*\s*$`)
	reFaHead  = regexp.MustCompile(`(?i)^(فارسی|persian|farsi)$`)
	reEnHead  = regexp.MustCompile(`(?i)^(english|انگلیسی)$`)
	reSkip    = regexp.MustCompile(`(?i)^(what'?s changed|changes|changelog)$`)
	reItem    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(.*)$`)
	reImg     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reBold    = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	reCode    = regexp.MustCompile("`+([^`]*?)`+")
)

// noteSection returns the lines of the release notes for lang. Veilon
// notes have one "## فارسی" and one "## English" section; notes without
// language sections are shown whole, and when only the other language
// exists it is shown rather than nothing.
func noteSection(notes, lang string) []string {
	if lang != "en" {
		lang = "fa"
	}
	type sec struct {
		lang  string
		lines []string
	}
	secs := []sec{{}}
	for _, l := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		if m := reHeading.FindStringSubmatch(l); m != nil {
			h := strings.TrimSpace(m[1])
			if reFaHead.MatchString(h) || reEnHead.MatchString(h) {
				lg := "en"
				if reFaHead.MatchString(h) {
					lg = "fa"
				}
				secs = append(secs, sec{lang: lg})
				continue
			}
		}
		secs[len(secs)-1].lines = append(secs[len(secs)-1].lines, l)
	}
	var mine, all []string
	hasLang := false
	for _, s := range secs {
		if s.lang == lang {
			mine = append(mine, s.lines...)
		}
		if s.lang != "" {
			hasLang = true
			all = append(all, s.lines...)
		}
	}
	switch {
	case mine != nil:
		return mine
	case !hasLang:
		return secs[0].lines
	default:
		return all
	}
}

// inlineHTML turns inline markdown into Telegram HTML (code spans kept).
func inlineHTML(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range reCode.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(plainMD(s[last:m[0]]))
		b.WriteString(code(strings.TrimSpace(s[m[2]:m[3]])))
		last = m[1]
	}
	b.WriteString(plainMD(s[last:]))
	return b.String()
}

func plainMD(s string) string {
	s = reImg.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reBold.ReplaceAllString(s, "$1$2")
	return esc(s)
}

// releaseNotes renders the notes for the panel's language in at most max
// characters.
func releaseNotes(notes, lang string, max int) string {
	var out []string
	n := 0
	for _, l := range noteSection(notes, lang) {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "<!--") {
			continue
		}
		var s string
		if m := reHeading.FindStringSubmatch(l); m != nil {
			h := strings.TrimSpace(m[1])
			if reSkip.MatchString(h) {
				continue
			}
			s = "<b>" + plainMD(h) + "</b>"
		} else if m := reItem.FindStringSubmatch(l); m != nil {
			s = "• " + inlineHTML(m[1])
		} else {
			s = inlineHTML(l)
		}
		if n+len([]rune(s)) > max {
			out = append(out, "…")
			break
		}
		n += len([]rune(s)) + 1
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}
