package main

import (
	"testing"
	"time"
)

// The CLI prints times in the panel's time zone, never the server's.
func TestCLITimesUsePanelZone(t *testing.T) {
	defer setCLILoc("UTC")
	at := time.Date(2026, 11, 9, 21, 0, 0, 0, time.UTC) // 00:30 on the 10th in Tehran
	setCLILoc("Asia/Tehran")
	if got := fmtTime(at); got != "2026-11-10 00:30 +0330" {
		t.Fatalf("tehran: %q", got)
	}
	setCLILoc("America/New_York") // EST in November
	if got := fmtTime(at); got != "2026-11-09 16:00 EST" {
		t.Fatalf("new york: %q", got)
	}
	setCLILoc("Not/AZone")
	if got := fmtTime(at); got != "2026-11-09 21:00 UTC" {
		t.Fatalf("unknown zone: %q", got)
	}
}
