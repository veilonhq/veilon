package guard

import (
	"testing"
	"time"
)

// A new user and IP pair is handed out by DrainNew once; its later hits
// wait for the minute Drain, and nothing is counted twice.
func TestTrackerDrainNew(t *testing.T) {
	tr := NewTracker()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr.Observe(7, "1.1.1.1", t0)
	tr.Observe(7, "1.1.1.1", t0.Add(time.Second))
	p := tr.DrainNew()
	if len(p) != 1 || p[0].IP != "1.1.1.1" || p[0].Hits != 2 || !p[0].New {
		t.Fatalf("first DrainNew %+v", p)
	}
	if p := tr.DrainNew(); len(p) != 0 {
		t.Fatalf("known IP drained again: %+v", p)
	}
	tr.Observe(7, "1.1.1.1", t0.Add(2*time.Second))
	if p := tr.DrainNew(); len(p) != 0 {
		t.Fatalf("hit of a known IP went to DrainNew: %+v", p)
	}
	tr.Observe(7, "2.2.2.2", t0.Add(3*time.Second))
	if p := tr.DrainNew(); len(p) != 1 || p[0].IP != "2.2.2.2" {
		t.Fatalf("second new IP %+v", p)
	}
	d := tr.Drain(t0.Add(4*time.Second), 10*time.Minute)
	if len(d) != 1 || d[0].IP != "1.1.1.1" || d[0].Hits != 1 || d[0].New {
		t.Fatalf("Drain after DrainNew %+v", d)
	}
}

// Fresh hands out each new IP once with its hits; the stats batch (Drain)
// then carries only later hits and skips IPs that had none.
func TestCollectorFresh(t *testing.T) {
	c := &IPCollector{}
	line := func(ip string) string {
		return "2026/11/10 10:00:00 from " + ip + ":5 accepted tcp:x.com:443 [in-1 >> direct] email: 3.bob"
	}
	c.ObserveLine(line("1.1.1.1"))
	c.ObserveLine(line("1.1.1.1"))
	f := c.Fresh()
	if len(f) != 1 || f[0].Hits != 2 || f[0].UserID != 3 {
		t.Fatalf("Fresh %+v", f)
	}
	if f := c.Fresh(); len(f) != 0 {
		t.Fatalf("Fresh twice %+v", f)
	}
	c.ObserveLine(line("2.2.2.2"))
	if d := c.Drain(); len(d) != 1 || d[0].IP != "2.2.2.2" || d[0].Hits != 1 {
		t.Fatalf("Drain must skip IPs whose hits went with Fresh: %+v", d)
	}
	c.ObserveLine(line("1.1.1.1"))
	c.ObserveLine(line("1.1.1.1"))
	if f := c.Fresh(); len(f) != 1 || f[0].Hits != 2 {
		t.Fatalf("Fresh after a batch %+v", f)
	}
	c.ObserveLine(line("1.1.1.1"))
	if d := c.Drain(); len(d) != 1 || d[0].Hits != 1 {
		t.Fatalf("Drain later hits %+v", d)
	}
}
