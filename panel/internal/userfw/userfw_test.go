package userfw

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	ok := []struct{ in, out, kind string }{
		{"example.com", "example.com", KindDomain},
		{"  Example.COM. ", "example.com", KindDomain},
		{"domain:YouTube.com", "youtube.com", KindDomain},
		{"*.googlevideo.com", "googlevideo.com", KindDomain},
		{"https://www.Example.com:8443/path?q=1", "www.example.com", KindDomain},
		{"http://user@example.org/", "example.org", KindDomain},
		{"example.com:443", "example.com", KindDomain},
		{"ir", "ir", KindDomain},
		{"xn--mgbaam7a8h", "xn--mgbaam7a8h", KindDomain},
		{"_dmarc.example.com", "_dmarc.example.com", KindDomain},
		{"full:Api.Example.com", "full:api.example.com", KindFull},
		{"regexp:\\.example\\.(com|net)$", "regexp:\\.example\\.(com|net)$", KindRegexp},
		{"1.2.3.4", "1.2.3.4", KindIP},
		{"1.2.3.4:443", "1.2.3.4", KindIP},
		{"1.2.3.4/32", "1.2.3.4", KindIP},
		{"10.1.2.3/8", "10.0.0.0/8", KindIP},
		{"2001:DB8::1", "2001:db8::1", KindIP},
		{"[2001:db8::1]", "2001:db8::1", KindIP},
		{"2001:db8:abcd::/48", "2001:db8:abcd::/48", KindIP},
		{"::ffff:1.2.3.4", "1.2.3.4", KindIP},
		{"https://[2001:db8::5]:443/", "2001:db8::5", KindIP},
		{"http://8.8.8.8/dns", "8.8.8.8", KindIP},
	}
	for _, c := range ok {
		v, k, err := Normalize(c.in)
		if err != nil || v != c.out || k != c.kind {
			t.Errorf("Normalize(%q) = %q %q %v, want %q %q", c.in, v, k, err, c.out, c.kind)
		}
		if Kind(v) != c.kind {
			t.Errorf("Kind(%q) = %q", v, Kind(v))
		}
		// canonical forms are stable
		if v2, _, err := Normalize(v); err != nil || v2 != v {
			t.Errorf("not stable: %q -> %q %v", v, v2, err)
		}
	}
	bad := []struct{ in, reason string }{
		{"", ReasonEmpty},
		{"   ", ReasonEmpty},
		{"exa mple.com", ReasonBadDomain},
		{"-bad.com", ReasonBadDomain},
		{"ex..com", ReasonBadDomain},
		{"سایت.com", ReasonBadDomain},
		{"full:", ReasonBadDomain},
		{"regexp:(", ReasonBadRegexp},
		{"regexp:", ReasonBadRegexp},
		{"1.2.3.256", ReasonBadIP},
		{"1.2.3", ReasonBadIP},
		{"1.2.3.4/33", ReasonBadIP},
		{"0.0.0.0/0", ReasonTooBroad},
		{"10.0.0.0/7", ReasonTooBroad},
		{"::/0", ReasonTooBroad},
		{"2000::/3", ReasonTooBroad},
		{"geosite:youtube", ReasonUnsupported},
		{"geoip:ir", ReasonUnsupported},
		{"keyword:ads", ReasonUnsupported},
		{"fe80::1%eth0", ReasonBadIP},
		{strings.Repeat("a", 64) + ".com", ReasonBadDomain},
		{strings.Repeat("a.", 130) + "com", ReasonTooLong},
	}
	for _, c := range bad {
		_, _, err := Normalize(c.in)
		var e *EntryError
		if !errors.As(err, &e) || e.Reason != c.reason {
			t.Errorf("Normalize(%q) err = %v, want reason %s", c.in, err, c.reason)
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
