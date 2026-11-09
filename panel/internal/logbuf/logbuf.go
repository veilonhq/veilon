// Package logbuf keeps recent log lines of the engines and of the panel
// itself as timestamped, levelled entries for the logs page.
package logbuf

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Entry is one log line.
type Entry struct {
	// Seq orders entries across every buffer (later = larger). 0 for
	// entries parsed from plain lines.
	Seq uint64 `json:"seq"`
	// T is when the line was written (UTC); nil when unknown.
	T      *time.Time `json:"t"`
	Source string     `json:"source"`
	// Level is debug, info, warning or error.
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

var seq atomic.Uint64

// Ring is a bounded buffer of entries.
type Ring struct {
	Source string
	mu     sync.Mutex
	items  []Entry
	max    int
	// Now is the clock (tests).
	Now func() time.Time
}

// New returns a ring that keeps the last max entries.
func New(source string, max int) *Ring { return &Ring{Source: source, max: max} }

// Add records a raw output line, received now. A timestamp the engine
// wrote at the start of the line wins over the receive time and is cut
// from the message.
func (r *Ring) Add(line string) { r.AddTagged("", line) }

// AddTagged is Add with "[tag] " put in front of the message.
func (r *Ring) AddTagged(tag, line string) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	t, level, msg := Parse(line, time.Local)
	if tag != "" {
		msg = "[" + tag + "] " + msg
	}
	if t == nil {
		v := now().UTC()
		t = &v
	}
	r.push(Entry{T: t, Source: r.Source, Level: level, Msg: msg})
}

// AddEntry records an entry as is (Seq and Source are filled in).
func (r *Ring) AddEntry(e Entry) {
	if e.Source == "" {
		e.Source = r.Source
	}
	r.push(e)
}

func (r *Ring) push(e Entry) {
	e.Seq = seq.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, e)
	if len(r.items) > r.max+r.max/4 {
		r.items = append([]Entry(nil), r.items[len(r.items)-r.max:]...)
	}
}

// Last returns up to n most recent entries, oldest first (n <= 0: all).
func (r *Ring) Last(n int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.items
	if len(items) > r.max {
		items = items[len(items)-r.max:]
	}
	if n <= 0 || n > len(items) {
		n = len(items)
	}
	return append([]Entry{}, items[len(items)-n:]...)
}

// Lines returns up to n most recent entries as plain text, each starting
// with its RFC 3339 UTC time.
func (r *Ring) Lines(n int) []string {
	es := r.Last(n)
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Msg
		if e.T != nil {
			out[i] = e.T.UTC().Format(time.RFC3339Nano) + " " + e.Msg
		}
	}
	return out
}

var (
	// Xray: "2026/11/09 10:45:27.928621 [Info] ..." (local time of the process).
	xrayTS = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) `)
	// sing-box: "+0330 2026-11-09 10:45:27 INFO ..." or "2026-11-09T10:45:27.123Z ..."
	sbTS  = regexp.MustCompile(`^([+-]\d{4} \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) `)
	isoTS = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?) `)
	// slog text: time=2026-11-09T10:45:27.123+03:30 level=INFO msg=...
	slogTS  = regexp.MustCompile(`^time=(\S+) `)
	lvlTag  = regexp.MustCompile(`(?i)^\[(debug|info|warning|warn|error|fatal|panic)\]\s*`)
	lvlWord = regexp.MustCompile(`^(DEBUG|INFO|WARN|WARNING|ERROR|FATAL|PANIC|TRACE)\b\s*`)
	slogLvl = regexp.MustCompile(`\blevel=(DEBUG|INFO|WARN|ERROR)\b`)
	jsonLvl = regexp.MustCompile(`"level"\s*:\s*"(\w+)"`)
)

// Parse splits a raw line into its own timestamp (nil if it has none),
// level and message. loc is the zone of timestamps without an offset.
func Parse(line string, loc *time.Location) (*time.Time, string, string) {
	msg := strings.TrimRight(line, "\r")
	var t *time.Time
	set := func(v time.Time, err error, n int) {
		if err == nil {
			u := v.UTC()
			t = &u
			msg = msg[n:]
		}
	}
	if m := xrayTS.FindStringSubmatch(msg); m != nil {
		v, err := time.ParseInLocation("2006/01/02 15:04:05.999999999", m[1], loc)
		set(v, err, len(m[0]))
	} else if m := sbTS.FindStringSubmatch(msg); m != nil {
		v, err := time.Parse("-0700 2006-01-02 15:04:05.999999999", m[1])
		set(v, err, len(m[0]))
	} else if m := isoTS.FindStringSubmatch(msg); m != nil {
		s := strings.Replace(m[1], " ", "T", 1)
		var v time.Time
		var err error
		for _, f := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700"} {
			if v, err = time.Parse(f, s); err == nil {
				break
			}
		}
		if err != nil {
			v, err = time.ParseInLocation("2006-01-02T15:04:05.999999999", s, loc)
		}
		set(v, err, len(m[0]))
	} else if m := slogTS.FindStringSubmatch(msg); m != nil {
		v, err := time.Parse(time.RFC3339Nano, m[1])
		set(v, err, len(m[0]))
	}
	return t, levelOf(msg), msg
}

func levelOf(msg string) string {
	var w string
	if m := lvlTag.FindStringSubmatch(msg); m != nil {
		w = m[1]
	} else if m := lvlWord.FindStringSubmatch(msg); m != nil {
		w = m[1]
	} else if m := slogLvl.FindStringSubmatch(msg); m != nil {
		w = m[1]
	} else if m := jsonLvl.FindStringSubmatch(msg); m != nil {
		w = m[1]
	}
	switch strings.ToLower(w) {
	case "debug", "trace":
		return "debug"
	case "warning", "warn":
		return "warning"
	case "error", "fatal", "panic":
		return "error"
	}
	return "info"
}

// FromLines turns plain lines (with or without their own timestamps) into
// entries. A line without a timestamp (a banner or continuation line, a
// panel note) takes the time of the next line that has one: engines print
// those lines right before it. Trailing lines without one keep none.
func FromLines(source string, lines []string, loc *time.Location) []Entry {
	out := make([]Entry, len(lines))
	for i, l := range lines {
		t, lvl, msg := Parse(l, loc)
		out[i] = Entry{T: t, Source: source, Level: lvl, Msg: msg}
	}
	var next *time.Time
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].T != nil {
			next = out[i].T
		} else if next != nil {
			v := *next
			out[i].T = &v
		}
	}
	return out
}

// Handler is a slog.Handler that copies every record into a ring and
// passes it on to the next handler.
type Handler struct {
	Next  slog.Handler
	Ring  *Ring
	attrs []slog.Attr
	group string
}

// Enabled reports whether the next handler logs the level.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool { return h.Next.Enabled(ctx, l) }

// Handle records r and forwards it.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	write := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		k := a.Key
		if h.group != "" {
			k = h.group + "." + k
		}
		v := a.Value.Resolve().String()
		if strings.ContainsAny(v, " \"=") {
			v = fmt.Sprintf("%q", v)
		}
		b.WriteString(" " + k + "=" + v)
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(func(a slog.Attr) bool { write(a); return true })
	lvl := "info"
	switch {
	case r.Level >= slog.LevelError:
		lvl = "error"
	case r.Level >= slog.LevelWarn:
		lvl = "warning"
	case r.Level < slog.LevelInfo:
		lvl = "debug"
	}
	t := r.Time.UTC()
	if r.Time.IsZero() {
		t = time.Now().UTC()
	}
	h.Ring.AddEntry(Entry{T: &t, Level: lvl, Msg: b.String()})
	return h.Next.Handle(ctx, r)
}

// WithAttrs returns a handler with extra attributes.
func (h *Handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &Handler{Next: h.Next.WithAttrs(as), Ring: h.Ring, attrs: append(append([]slog.Attr{}, h.attrs...), as...), group: h.group}
}

// WithGroup returns a handler that nests later attributes in a group.
func (h *Handler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &Handler{Next: h.Next.WithGroup(name), Ring: h.Ring, attrs: h.attrs, group: g}
}

// SortByTime orders entries of several sources by time. An entry without a
// time stays right after the entry before it in its own source.
func SortByTime(es []Entry) {
	var last time.Time
	var src string
	key := make([]time.Time, len(es))
	for i, e := range es {
		if e.Source != src {
			src, last = e.Source, time.Time{}
		}
		if e.T != nil {
			last = *e.T
		}
		key[i] = last
	}
	idx := make([]int, len(es))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ka, kb := key[idx[a]], key[idx[b]]
		if !ka.Equal(kb) {
			return ka.Before(kb)
		}
		sa, sb := es[idx[a]].Seq, es[idx[b]].Seq
		return sa != 0 && sb != 0 && sa < sb
	})
	out := make([]Entry, len(es))
	for i, j := range idx {
		out[i] = es[j]
	}
	copy(es, out)
}
