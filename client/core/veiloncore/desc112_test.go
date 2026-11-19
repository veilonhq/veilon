package veiloncore

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 1.0.12: subscription description (profile-description header) and body
// directive fallbacks ("#key: value" lines).

func desc112Links() string {
	return strings.Join([]string{
		"vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?type=tcp&security=tls&sni=a.test#srv-one",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"srv-two","add":"198.51.100.11","port":"8080","id":"11111111-2222-3333-4444-555555555556","aid":"0","net":"ws","path":"/x","host":"b.test","tls":""}`)),
		"trojan://pw@198.51.100.12:443?security=tls&sni=c.test#srv-three",
	}, "\n")
}

func b64s(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// desc112Server serves body with headers on every path (also ?format=…,
// like panels that ignore the format).
func desc112Server(t *testing.T, headers map[string]string, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/sub/token"
}

func fetch112(t *testing.T, headers map[string]string, body string) *SubResult {
	t.Helper()
	url := desc112Server(t, headers, body)
	out := FetchSubscription(url, `{"hwid":"h","appVersion":"1.0.12"}`, "", 5000)
	var res SubResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if res.HTTPStatusCode != 200 {
		t.Fatalf("status %d errors %v", res.HTTPStatusCode, res.Errors)
	}
	return &res
}

func wantLinks(t *testing.T, res *SubResult) {
	t.Helper()
	if len(res.Profiles) != 3 || len(res.Errors) != 0 {
		t.Fatalf("profiles %d errors %v (want 3, none)", len(res.Profiles), res.Errors)
	}
	names := []string{res.Profiles[0].Name, res.Profiles[1].Name, res.Profiles[2].Name}
	if strings.Join(names, ",") != "srv-one,srv-two,srv-three" {
		t.Fatalf("names %v", names)
	}
}

func TestDescriptionHeaderPlain(t *testing.T) {
	res := fetch112(t, map[string]string{"Profile-Description": "  Fast servers, renewed monthly  "}, b64s(desc112Links()))
	if res.Description != "Fast servers, renewed monthly" {
		t.Fatalf("description %q", res.Description)
	}
	wantLinks(t, res)
}

func TestDescriptionHeaderBase64(t *testing.T) {
	d := "سرورهای پرسرعت\nتمدید ماهانه\r\nپشتیبانی ۲۴ ساعته"
	res := fetch112(t, map[string]string{"profile-description": "base64:" + b64s(d)}, b64s(desc112Links()))
	if res.Description != "سرورهای پرسرعت\nتمدید ماهانه\nپشتیبانی ۲۴ ساعته" {
		t.Fatalf("description %q", res.Description)
	}
	wantLinks(t, res)
}

func TestDescriptionBodyDirective(t *testing.T) {
	for _, line := range []string{"#profile-description: From the body", "#profile-description:From the body", "  #Profile-Description :  From the body  "} {
		res := fetch112(t, nil, line+"\n"+desc112Links())
		if res.Description != "From the body" {
			t.Fatalf("%q: description %q", line, res.Description)
		}
		wantLinks(t, res)
	}
	// base64: value in a body directive (multi-line text).
	res := fetch112(t, nil, "#profile-description: base64:"+b64s("line one\nline two")+"\n"+desc112Links())
	if res.Description != "line one\nline two" {
		t.Fatalf("base64 directive: %q", res.Description)
	}
}

func TestHeaderWinsOverBodyDirective(t *testing.T) {
	body := strings.Join([]string{
		"#profile-title: Body title",
		"#profile-description: Body description",
		"#announce: Body announce",
		"#support-url: https://body.test/support",
		"#profile-web-page-url: https://body.test/page",
		"#profile-update-interval: 6",
		"#subscription-userinfo: upload=1; download=2; total=3; expire=4",
		desc112Links(),
	}, "\n")
	res := fetch112(t, map[string]string{
		"profile-title":           "base64:" + b64s("Header title"),
		"profile-description":     "Header description",
		"announce":                "Header announce",
		"support-url":             "https://header.test/support",
		"profile-web-page-url":    "https://header.test/page",
		"profile-update-interval": "12",
		"subscription-userinfo":   "upload=10; download=20; total=30; expire=40",
	}, body)
	if res.Title != "Header title" || res.Description != "Header description" || res.Announce != "Header announce" ||
		res.SupportURL != "https://header.test/support" || res.WebPageURL != "https://header.test/page" || res.UpdateHours != 12 ||
		res.UserInfo != (UserInfo{Upload: 10, Download: 20, Total: 30, Expire: 40, Present: true}) {
		t.Fatalf("header must win: %+v", res)
	}
	wantLinks(t, res)

	// Headers absent → every key comes from the body.
	res = fetch112(t, nil, body)
	if res.Title != "Body title" || res.Description != "Body description" || res.Announce != "Body announce" ||
		res.SupportURL != "https://body.test/support" || res.WebPageURL != "https://body.test/page" || res.UpdateHours != 6 ||
		res.UserInfo != (UserInfo{Upload: 1, Download: 2, Total: 3, Expire: 4, Present: true}) {
		t.Fatalf("body fallback: %+v", res)
	}
	wantLinks(t, res)

	// Only some headers: the rest falls back per key; an empty header does not win.
	res = fetch112(t, map[string]string{"profile-title": "Header title", "profile-description": "  "}, body)
	if res.Title != "Header title" || res.Description != "Body description" || res.UpdateHours != 6 {
		t.Fatalf("per-key fallback: %+v", res)
	}
}

func TestDirectiveLinesKeepLinksParsing(t *testing.T) {
	dirs := "#profile-title: base64:" + b64s("عنوان") + "\n#profile-description: توضیحات\n#support-url: https://t.me/x\n"
	cases := map[string]string{
		// directives inside the base64-encoded body
		"base64 body, directives inside": b64s(dirs + desc112Links()),
		// plain directives (with "://") above a base64 blob: before 1.0.12
		// the "://" stopped the base64 decode and no link parsed
		"plain directives above base64 blob": dirs + b64s(desc112Links()),
		"plain body":                         dirs + desc112Links(),
		"plain body CRLF":                    strings.ReplaceAll(dirs+desc112Links(), "\n", "\r\n"),
		"base64 blob wrapped at 76":          dirs + wrap76(b64s(desc112Links())),
	}
	for name, body := range cases {
		res := fetch112(t, nil, body)
		if res.Description != "توضیحات" || res.Title != "عنوان" || res.SupportURL != "https://t.me/x" {
			t.Fatalf("%s: meta %+v", name, res)
		}
		wantLinks(t, res)
	}
}

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

func TestNoDescription(t *testing.T) {
	url := desc112Server(t, map[string]string{"profile-title": "T"}, b64s("#just a comment\n"+desc112Links()))
	out := FetchSubscription(url, `{}`, "", 5000)
	if strings.Contains(out, `"description"`) {
		t.Fatalf("description key present without a description: %s", out)
	}
	var res SubResult
	_ = json.Unmarshal([]byte(out), &res)
	if res.Description != "" {
		t.Fatalf("description %q", res.Description)
	}
	wantLinks(t, &res)
}

func TestSplitBodyDirectivesUnchangedWithout(t *testing.T) {
	for _, body := range []string{
		b64s(desc112Links()),
		desc112Links(),
		"# comment: not a directive\n" + desc112Links(),
		`{"outbounds":[{"type":"direct"}],"note":"#profile-title: x"}`,
		"[{\"protocol\":\"vless\"}]",
		"proxies:\n  # profile-x: comment\n  - name: a\n",
		"",
	} {
		got, dir := splitBodyDirectives(body)
		if got != body || dir != nil {
			t.Fatalf("body changed without directives: %q → %q (%v)", body, got, dir)
		}
	}
	// JSON is never touched even if a line looks like a directive.
	j := "{\n#profile-title: x\n}"
	if got, dir := splitBodyDirectives(j); got != j || dir != nil {
		t.Fatalf("JSON touched")
	}
	// First value per key wins; unknown keys stay in the body.
	got, dir := splitBodyDirectives("#profile-title: a\n#profile-title: b\n#other: c\nvless://x")
	if dir["profile-title"] != "a" || got != "#other: c\nvless://x" {
		t.Fatalf("got %q dir %v", got, dir)
	}
}

func TestCleanDescription(t *testing.T) {
	cases := map[string]string{
		"a\x00b\x07c\x1b[31md\x7f":               "abc[31md",
		"tab\there":                              "tab here",
		"\u202eevil\u202c \u2067x\u2069 \u200fy": "evil x y",
		"نیم\u200cفاصله":                         "نیم فاصله",
		"one\n\n\n\ntwo   \n  ":                  "one\n\ntwo",
		"\n\n  lead":                             "lead",
		"bad \xff utf8":                          "bad  utf8",
		"<b>html</b> <a href='x'>l</a>":          "<b>html</b> <a href='x'>l</a>", // shown as plain text by the app
	}
	for in, want := range cases {
		if got := cleanDescription(in); got != want {
			t.Fatalf("cleanDescription(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ب", 1500)
	got := cleanDescription(long)
	if utf8.RuneCountInString(got) != MaxDescriptionRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("cap: %d runes", utf8.RuneCountInString(got))
	}
	if cleanDescription(strings.Repeat("x", MaxDescriptionRunes)) != strings.Repeat("x", MaxDescriptionRunes) {
		t.Fatalf("exactly the cap must not be cut")
	}
	// The cap also applies end-to-end.
	res := fetch112(t, map[string]string{"profile-description": "base64:" + b64s(long)}, desc112Links())
	if utf8.RuneCountInString(res.Description) != MaxDescriptionRunes {
		t.Fatalf("e2e cap: %d", utf8.RuneCountInString(res.Description))
	}
	_ = time.Second
}

// TestMockSubServer112 runs only with VEILON_MOCK_SUB_URL set: an external
// local mock subscription server (logs/mocksub112.py: description header on
// /h, body directive only on /b, both on /both).
func TestMockSubServer112(t *testing.T) {
	base := os.Getenv("VEILON_MOCK_SUB_URL")
	if base == "" {
		t.Skip("VEILON_MOCK_SUB_URL not set")
	}
	for path, want := range map[string]string{"/h": "از هدر\nfrom the header", "/b": "from the body", "/both": "از هدر\nfrom the header"} {
		var res SubResult
		if err := json.Unmarshal([]byte(FetchSubscription(base+path, `{"hwid":"mock"}`, "", 5000)), &res); err != nil {
			t.Fatal(err)
		}
		if res.HTTPStatusCode != 200 || res.Description != want || len(res.Profiles) != 3 || len(res.Errors) != 0 {
			t.Fatalf("%s: status %d description %q profiles %d errors %v", path, res.HTTPStatusCode, res.Description, len(res.Profiles), res.Errors)
		}
		t.Logf("%s: description %q, %d profiles, title %q", path, res.Description, len(res.Profiles), res.Title)
	}
}
