package userfw

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		OK  [][3]string `json:"ok"`
		Bad [][2]string `json:"bad"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.OK) < 20 || len(cases.Bad) < 20 {
		t.Fatal("cases missing")
	}
	for _, c := range cases.OK {
		in, kind, out := c[0], c[1], c[2]
		v, k, err := Normalize(in)
		if err != nil || v != out || k != kind {
			t.Errorf("Normalize(%q) = %q %q %v, want %q %q", in, v, k, err, out, kind)
		}
		if Kind(v) != kind {
			t.Errorf("Kind(%q) = %q", v, Kind(v))
		}
		// canonical forms are stable
		if v2, _, err := Normalize(v); err != nil || v2 != v {
			t.Errorf("not stable: %q -> %q %v", v, v2, err)
		}
	}
	bad := cases.Bad
	bad = append(bad, [2]string{strings.Repeat("a.", 130) + "com", ReasonTooLong})
	for _, c := range bad {
		_, _, err := Normalize(c[0])
		var e *EntryError
		if !errors.As(err, &e) || e.Reason != c[1] {
			t.Errorf("Normalize(%q) err = %v, want reason %s", c[0], err, c[1])
		}
	}
}

func TestNormalizeList(t *testing.T) {
	got, err := NormalizeList([]string{"Example.com", "", "domain:example.com", "1.2.3.4/32", "1.2.3.4", "full:a.b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.com", "1.2.3.4", "full:a.b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	uniq := make([]string, MaxEntries+1)
	for i := range uniq {
		uniq[i] = "10.0." + itoa(i/250) + "." + itoa(i%250)
	}
	var e *EntryError
	if _, err := NormalizeList(uniq); !errors.As(err, &e) || e.Reason != ReasonTooMany {
		t.Fatalf("too many accepted: %v", err)
	}
	if _, err := NormalizeList(uniq[:MaxEntries]); err != nil {
		t.Fatalf("max refused: %v", err)
	}
	if _, err := NormalizeList([]string{"ok.com", "bad..com"}); !errors.As(err, &e) || e.Entry != "bad..com" {
		t.Fatalf("bad entry not named: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}

func TestSplitAndRender(t *testing.T) {
	stored := Join([]string{"example.com", "full:api.x.org", "regexp:^ads\\.", "1.2.3.4", "2001:db8::/32", "10.0.0.0/8"})
	// a damaged stored line is skipped, never fatal
	stored += "\n\nbad..entry\nexample.com"
	l := Split(stored)
	if len(l) != 6 {
		t.Fatalf("split %v", l)
	}
	r := Render(l)
	want := Rules{
		XrayDomains: []string{"domain:example.com", "full:api.x.org", "regexp:^ads\\."},
		XrayIPs:     []string{"1.2.3.4", "10.0.0.0/8", "2001:db8::/32"},
		Suffix:      []string{".example.com"},
		Exact:       []string{"api.x.org", "example.com"},
		Regex:       []string{"^ads\\."},
		CIDRs:       []string{"1.2.3.4/32", "10.0.0.0/8", "2001:db8::/32"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("render\n%+v\nwant\n%+v", r, want)
	}
	if Render(nil).Empty() != true || r.Empty() {
		t.Fatal("Empty")
	}
	if got := CIDRs(stored); !reflect.DeepEqual(got, want.CIDRs) {
		t.Fatalf("CIDRs %v", got)
	}
	if len(Split("")) != 0 || Split("") == nil {
		t.Fatal("empty split")
	}
}
