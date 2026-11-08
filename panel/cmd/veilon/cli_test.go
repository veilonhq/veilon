package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestChecksumFor(t *testing.T) {
	sums := "aaa  veilon-linux-amd64\nbbb *veilon-linux-arm64\nccc  install.sh\n"
	if got := checksumFor(sums, "veilon-linux-arm64"); got != "bbb" {
		t.Fatalf("arm64: %q", got)
	}
	if got := checksumFor(sums, "veilon-linux-amd64"); got != "aaa" {
		t.Fatalf("amd64: %q", got)
	}
	if got := checksumFor(sums, "veilon-linux-386"); got != "" {
		t.Fatalf("missing: %q", got)
	}
}

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	yes := fs.Bool("y", false, "")
	pos, err := parseInterspersed(fs, []string{"7", "-y"})
	if err != nil || !*yes || !reflect.DeepEqual(pos, []string{"7"}) {
		t.Fatalf("pos=%v yes=%v err=%v", pos, *yes, err)
	}
}

func TestCertID(t *testing.T) {
	if id, err := certID([]string{"#12"}); err != nil || id != 12 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	for _, bad := range [][]string{{}, {"x"}, {"0"}, {"1", "2"}} {
		if _, err := certID(bad); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}
