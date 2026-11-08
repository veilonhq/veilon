package stats

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func TestBucketize(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []db.TrafficPoint{
		{T: start.Add(1 * time.Minute), Up: 1, Down: 2},
		{T: start.Add(14 * time.Minute), Up: 1, Down: 2},
		{T: start.Add(15 * time.Minute), Up: 5, Down: 5},
		{T: start.Add(-time.Minute), Up: 100},
	}
	pts := Bucketize(rows, start, start.Add(time.Hour), 15*time.Minute)
	if len(pts) != 4 || pts[0].Up != 2 || pts[0].Down != 4 || pts[1].Up != 5 || pts[3].Up != 0 {
		t.Fatalf("%+v", pts)
	}
	if _, _, ok := RangeSpec("1y"); ok {
		t.Fatal("bad range accepted")
	}
}

func TestSnapshot(t *testing.T) {
	s := &Sampler{}
	s.sample()
	h := s.Snapshot()
	if h.MemTotal == 0 || h.DiskTotal == 0 {
		t.Skip("no /proc on this platform")
	}
}

func TestParseMemAndCPU(t *testing.T) {
	m := parseMemInfo(bufio.NewScanner(strings.NewReader("MemTotal:  2000 kB\nMemFree: 100 kB\nMemAvailable: 500 kB\nSwapTotal: 1000 kB\nSwapFree: 750 kB\n")))
	if m.total != 2000*1024 || m.used != 1500*1024 || m.swapTotal != 1000*1024 || m.swapUsed != 250*1024 {
		t.Fatalf("%+v", m)
	}
	n, model := parseCPUInfo("processor\t: 0\nmodel name\t: Intel(R)  Xeon(R) CPU\nprocessor\t: 1\nmodel name\t: Intel(R)  Xeon(R) CPU\n")
	if n != 2 || model != "Intel(R) Xeon(R) CPU" {
		t.Fatalf("%d %q", n, model)
	}
	if c, _ := cpuInfo(); c < 1 {
		t.Fatal("no cores")
	}
}
