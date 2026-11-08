package guard

import (
	"strings"
	"testing"
	"time"
)

func newTestGuard(calls *[]string) *Guard {
	g := New(nil, "")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return now }
	g.Dev = "eth0"
	g.Run = func(stdin, name string, args ...string) (string, error) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	return g
}

// Without any IP or speed limit the guard must never touch nft or tc.
func TestGuardNoLimitsIsNoop(t *testing.T) {
	var calls []string
	g := newTestGuard(&calls)
	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		g.ObserveEmail("7.alice", ip)
	}
	for i := 0; i < 3; i++ {
		g.Enforce()
	}
	g.clearTC()
	if len(calls) != 0 {
		t.Fatalf("expected no commands, got %v", calls)
	}
	// A limit that was set and then cleared is a no-op too.
	g.Limit(7, 0, 0, 0)
	g.Enforce()
	if len(calls) != 0 {
		t.Fatalf("expected no commands after clearing, got %v", calls)
	}
}

func TestGuardSafeRecoversPanic(t *testing.T) {
	var calls []string
	g := newTestGuard(&calls)
	g.safe("test", func() { panic("boom") }) // must not crash the test
}

func TestExcess(t *testing.T) {
	if got := Excess([]string{"a", "b", "c"}, 2); len(got) != 1 || got[0] != "c" {
		t.Fatalf("got %v", got)
	}
	if Excess([]string{"a", "b"}, 0) != nil {
		t.Fatal("limit 0 must mean unlimited")
	}
}
