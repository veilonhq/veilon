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

func TestTrafficAlignsToPanelTimezone(t *testing.T) {
	loc := time.FixedZone("Tehran", 3*3600+1800)
	now := time.Date(2026, 11, 9, 20, 47, 0, 0, loc)
	if got := alignStart(now, time.Hour, loc); !got.Equal(time.Date(2026, 11, 9, 20, 0, 0, 0, loc)) {
		t.Fatalf("hour start %v", got)
	}
	if got := alignStart(now, 24*time.Hour, loc); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, loc)) {
		t.Fatalf("day start %v", got)
	}
	if got := alignStart(now, 6*time.Hour, loc); !got.Equal(time.Date(2026, 11, 9, 18, 0, 0, 0, loc)) {
		t.Fatalf("6h start %v", got)
	}
	if _, ok := BucketSpec("2h"); ok {
		t.Fatal("bad bucket accepted")
	}
}

func TestParseNetDevSkipsVirtual(t *testing.T) {
	in := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 500 5 0 0 0 0 0 0 500 5 0 0 0 0 0 0
  ens5: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0
 vawg2: 300 3 0 0 0 0 0 0 400 4 0 0 0 0 0 0
`
	rx, tx := parseNetDev(bufio.NewScanner(strings.NewReader(in)), func(n string) bool { return n == "vawg2" })
	if rx != 1000 || tx != 2000 {
		t.Fatalf("rx=%d tx=%d", rx, tx)
	}
}

func dayKeys(es []time.Time, loc *time.Location) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.In(loc).Format("01-02 15:04")
	}
	return out
}

// Day buckets are calendar days in the panel zone, also across daylight
// saving changes (23- and 25-hour days), and the last bucket holds now.
func TestEdgesDSTCalendarDays(t *testing.T) {
	ber, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 2026-03-29 02:00 CET -> 03:00 CEST; 2026-10-25 03:00 CEST -> 02:00 CET
	now := time.Date(2026, 3, 30, 10, 0, 0, 0, ber)
	es := Edges(now, 3*24*time.Hour, 24*time.Hour, ber)
	got := dayKeys(es, ber)
	want := []string{"03-28 00:00", "03-29 00:00", "03-30 00:00", "03-31 00:00"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spring edges %v", got)
	}
	if d := es[2].Sub(es[1]); d != 23*time.Hour {
		t.Fatalf("spring-forward day is %v", d)
	}
	now = time.Date(2026, 10, 25, 23, 59, 0, 0, ber)
	es = Edges(now, 2*24*time.Hour, 24*time.Hour, ber)
	if es[2].Sub(es[1]) != 25*time.Hour || es[1].In(ber).Hour() != 0 {
		t.Fatalf("fall-back day: %v", dayKeys(es, ber))
	}
	// 6-hour buckets keep local wall times on the change day
	es = Edges(time.Date(2026, 3, 29, 20, 0, 0, 0, ber), 24*time.Hour, 6*time.Hour, ber)
	if strings.Join(dayKeys(es, ber), ",") != "03-29 00:00,03-29 06:00,03-29 12:00,03-29 18:00,03-30 00:00" {
		t.Fatalf("6h edges %v", dayKeys(es, ber))
	}
}

// Midnight boundaries in a non-UTC zone: a point one minute before local
// midnight lands in the old day, one at midnight in the new day.
func TestTrafficMidnightBoundaryTehran(t *testing.T) {
	teh, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Skip("no tzdata")
	}
	mid := time.Date(2026, 11, 10, 0, 0, 0, 0, teh)
	rows := []db.TrafficPoint{
		{T: mid.Add(-time.Minute).UTC(), Up: 1},
		{T: mid.UTC(), Up: 10},
		{T: mid.Add(23*time.Hour + 59*time.Minute).UTC(), Up: 100},
	}
	es := Edges(mid.Add(12*time.Hour), 7*24*time.Hour, 24*time.Hour, teh)
	if len(es) != 8 || !es[len(es)-2].Equal(mid) {
		t.Fatalf("edges %v", dayKeys(es, teh))
	}
	pts := BucketizeEdges(rows, es)
	if pts[5].Up != 1 || pts[6].Up != 110 {
		t.Fatalf("points %+v", pts)
	}
	// hour buckets at 00:00 Tehran = 20:30 UTC
	es = Edges(mid.Add(30*time.Minute), 24*time.Hour, time.Hour, teh)
	if got := es[len(es)-2].UTC().Format("15:04"); got != "20:30" {
		t.Fatalf("hour bucket start %s UTC", got)
	}
}
