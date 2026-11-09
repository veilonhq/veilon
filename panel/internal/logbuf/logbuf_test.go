package logbuf

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	teh, _ := time.LoadLocation("Asia/Tehran")
	cases := []struct {
		line, want, level, msg string
	}{
		{"2026/11/09 10:45:27.928621 [Info] infra/conf/serial: Reading config", "2026-11-09T07:15:27.928621Z", "info", "[Info] infra/conf/serial: Reading config"},
		{"2026/11/09 10:45:28.641587 [Warning] core: Xray 26.10.31 started", "2026-11-09T07:15:28.641587Z", "warning", "[Warning] core: Xray 26.10.31 started"},
		{"2026/11/09 10:45:32.875357 from 127.0.0.1:36692 accepted tcp:127.0.0.1:21395 [api -> api]", "2026-11-09T07:15:32.875357Z", "info", "from 127.0.0.1:36692 accepted tcp:127.0.0.1:21395 [api -> api]"},
		{"+0330 2026-11-09 10:45:27 ERROR [123 0ms] inbound/tuic: boom", "2026-11-09T07:15:27Z", "error", "ERROR [123 0ms] inbound/tuic: boom"},
		{"2026-11-09T07:15:27.5Z WARN something", "2026-11-09T07:15:27.5Z", "warning", "WARN something"},
		{"Xray 26.10.31 (Xray, Penetrates Everything.) a4a3dff (go1.27.3 linux/amd64)", "", "info", "Xray 26.10.31 (Xray, Penetrates Everything.) a4a3dff (go1.27.3 linux/amd64)"},
		{`{"level":"error","msg":"x"}`, "", "error", `{"level":"error","msg":"x"}`},
	}
	for _, c := range cases {
		ts, lvl, msg := Parse(c.line, teh)
		got := ""
		if ts != nil {
			got = ts.Format(time.RFC3339Nano)
		}
		if got != c.want || lvl != c.level || msg != c.msg {
			t.Errorf("Parse(%q) = %q %q %q, want %q %q %q", c.line, got, lvl, msg, c.want, c.level, c.msg)
		}
	}
}

func TestRingKeepsEveryLineUpToMax(t *testing.T) {
	r := New("xray", 100)
	base := time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC)
	i := 0
	r.Now = func() time.Time { i++; return base.Add(time.Duration(i) * time.Millisecond) }
	for n := 0; n < 1000; n++ {
		r.AddTagged("vawg1", "line")
	}
	es := r.Last(0)
	if len(es) != 100 {
		t.Fatalf("kept %d", len(es))
	}
	for k := 1; k < len(es); k++ {
		if es[k].Seq != es[k-1].Seq+1 || !es[k].T.After(*es[k-1].T) {
			t.Fatal("entries not contiguous / ordered")
		}
	}
	if es[0].Msg != "[vawg1] line" || es[0].T.Location() != time.UTC {
		t.Fatalf("entry %+v", es[0])
	}
	if l := r.Lines(1)[0]; !strings.HasSuffix(l, "Z [vawg1] line") {
		t.Fatalf("line %q", l)
	}
}

func TestFromLinesAndSort(t *testing.T) {
	es := FromLines("xray", []string{
		"[veilon] xray 26.10.31 started (pid 1)",
		"Xray 26.10.31 (Xray, Penetrates Everything.)",
		"2026/11/09 07:00:02.000001 [Info] reading config",
		"[veilon] tail",
	}, time.UTC)
	if es[0].T == nil || !es[0].T.Equal(*es[2].T) || es[3].T != nil {
		t.Fatalf("times not filled from the next stamped line: %+v", es)
	}
	p := time.Date(2026, 11, 9, 7, 0, 1, 0, time.UTC)
	all := append(es, Entry{Seq: 9, T: &p, Source: "panel", Msg: "p"})
	SortByTime(all)
	if all[0].Source != "panel" || all[4].Msg != "[veilon] tail" {
		t.Fatalf("order: %+v", all)
	}
}

func TestHandler(t *testing.T) {
	r := New("panel", 10)
	var sb strings.Builder
	l := slog.New(&Handler{Next: slog.NewTextHandler(&sb, nil), Ring: r}).With("engine", "xray")
	l.Warn("engine exited", "err", "exit status 1")
	l.Log(context.Background(), slog.LevelDebug, "hidden")
	es := r.Last(0)
	if len(es) != 1 || es[0].Level != "warning" || es[0].Msg != `engine exited engine=xray err="exit status 1"` || es[0].T == nil {
		t.Fatalf("%+v", es)
	}
	if !strings.Contains(sb.String(), "engine exited") {
		t.Fatal("not forwarded")
	}
}
