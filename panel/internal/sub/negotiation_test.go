package sub

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/testutil"
	"github.com/veilonhq/veilon/panel/internal/users"
)

// negFixture: one user ("carol", 2 inbounds, traffic and an expiry) for the
// content negotiation tests.
func negFixture(t *testing.T, status string) *Service {
	t.Helper()
	g := testutil.DB(t)
	svc := &inbounds.Service{DB: g}
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	_ = db.SaveSettings(g, db.Settings{ServerAddress: "198.51.100.7", SubPath: "sub", SubTitle: "Night Owl", SubUpdateHours: 8, Language: "fa", Timezone: "Asia/Tehran"})
	a := inbounds.Defaults()
	a.Remark, a.Protocol, a.Port, a.Network, a.Security = "VL WS", "vless", 8080, "ws", "none"
	a.Transport.Path = "/ws"
	ia, err := svc.Create(&a)
	if err != nil {
		t.Fatal(err)
	}
	b := inbounds.Defaults()
	b.Remark, b.Protocol, b.Port, b.SSMethod = "SS Edge", "shadowsocks", 8388, "chacha20-ietf-poly1305"
	ib, err := svc.Create(&b)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Date(2026, 12, 24, 20, 30, 0, 0, time.UTC)
	lo := time.Date(2026, 11, 10, 9, 15, 0, 0, time.UTC)
	u := db.User{Username: "carol", Status: status, UUID: "0b8a4c51-8a43-4c3b-9d7e-6f2a1b3c4d5e", Password: "carol-pass", SubToken: "tokCarol",
		DataLimit: 50 << 30, Up: 3 << 30, Down: 9 << 30, ExpireAt: &exp, LastOnlineAt: &lo, CreatedAt: now, UpdatedAt: now}
	g.Create(&u)
	g.Create(&db.UserInbound{UserID: u.ID, InboundID: ia.ID})
	g.Create(&db.UserInbound{UserID: u.ID, InboundID: ib.ID})
	return &Service{DB: g, UI: testUI}
}

// testUI stands in for the web build: a subscription page and one asset.
var testUI = fstest.MapFS{
	"sub.html":          {Data: []byte(`<!doctype html><html lang="fa" dir="rtl"><head><title>sub</title></head><body><div id="root"></div></body></html>`)},
	"assets/sub-abc.js": {Data: []byte(`console.log(1)`)},
	"index.html":        {Data: []byte(`<html><head></head></html>`)},
	"favicon.svg":       {Data: []byte(`<svg/>`)},
}

type negCase struct {
	name, ua, accept, query string
}

// rawCases: requests from VPN clients (and explicit formats). Their
// responses are pinned byte for byte in testdata/negotiation.golden.
var rawCases = []negCase{
	{"v2rayNG", "v2rayNG/1.9.16", "*/*", ""},
	{"v2rayN", "v2rayN/7.4.2", "", ""},
	{"Hiddify", "HiddifyNext/2.5.7 (android) like ClashMeta v2ray sing-box", "*/*", ""},
	{"Streisand", "Streisand/1.6.29 CFNetwork/1568.100.1 Darwin/24.0.0", "*/*", ""},
	{"Shadowrocket", "Shadowrocket/2070 CFNetwork/1568.100.1 Darwin/24.0.0 iPhone15,2", "*/*", ""},
	{"sing-box", "sing-box 1.10.1", "", ""},
	{"SFA", "SFA/1.10.1 (Android 14)", "", ""},
	{"clash", "clash-verge/v1.7.7", "*/*", ""},
	{"mihomo", "mihomo/1.18.10", "", ""},
	{"ClashMeta", "ClashMetaForAndroid/2.11.1.Meta", "*/*", ""},
	{"Happ", "Happ/1.9.2", "text/html,*/*", ""},
	{"V2Box", "V2Box 2.3 (iOS)", "*/*", ""},
	{"NekoBox", "NekoBox/Android/1.3.4 (Prefer ClashMeta Format)", "*/*", ""},
	{"Karing", "karing/1.0.36.540 android", "*/*", ""},
	{"curl", "curl/8.7.1", "*/*", ""},
	{"wget", "Wget/1.21.4", "*/*", ""},
	{"empty", "", "", ""},
	{"mozilla-no-html", "Mozilla/5.0 (Linux; Android 14)", "*/*", ""},
	{"browser-format-v2ray", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15", "text/html,application/xhtml+xml", "?format=v2ray"},
	{"browser-format-clash", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0 Safari/537.36", "text/html,*/*", "?format=clash"},
	{"browser-format-singbox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0 Safari/537.36", "text/html,*/*", "?format=singbox"},
	{"browser-format-json", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0 Safari/537.36", "text/html,*/*", "?format=json"},
}

func negRequest(s *Service, c negCase) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://198.51.100.7/sub/tokCarol"+c.query, nil)
	if c.ua != "" {
		r.Header.Set("User-Agent", c.ua)
	} else {
		r.Header.Del("User-Agent")
	}
	if c.accept != "" {
		r.Header.Set("Accept", c.accept)
	}
	w := httptest.NewRecorder()
	s.Serve(w, r, "tokCarol", false)
	return w
}

func dumpResponse(name string, w *httptest.ResponseRecorder) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s\nstatus %d\n", name, w.Code)
	keys := make([]string, 0, len(w.Header()))
	for k := range w.Header() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, strings.Join(w.Header()[k], ", "))
	}
	b.WriteString("\n")
	b.Write(w.Body.Bytes())
	b.WriteString("\n")
	return b.String()
}

// Every VPN client keeps receiving exactly what it received before the
// browser page existed: same status, headers and bytes.
func TestNegotiationClientsUnchanged(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	var out strings.Builder
	for _, c := range rawCases {
		out.WriteString(dumpResponse(c.name, negRequest(s, c)))
	}
	const golden = "testdata/negotiation.golden"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		got, exp := strings.Split(out.String(), "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(got) && i < len(exp); i++ {
			if got[i] != exp[i] {
				t.Fatalf("line %d differs:\n got %q\nwant %q", i+1, got[i], exp[i])
			}
		}
		t.Fatalf("length differs: %d vs %d lines", len(got), len(exp))
	}
}

const (
	chromeUA   = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36"
	safariUA   = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1"
	firefoxUA  = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
	htmlAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

func TestIsBrowser(t *testing.T) {
	cases := []struct {
		ua, accept string
		want       bool
	}{
		{chromeUA, htmlAccept, true},
		{safariUA, htmlAccept, true},
		{firefoxUA, htmlAccept, true},
		{chromeUA, "*/*", false},
		{chromeUA, "", false},
		{"curl/8.7.1", htmlAccept, false},
		{"Mozilla/5.0 (Linux; Android 14) v2rayNG/1.9", htmlAccept, false},
		{"Mozilla/5.0 Happ/1.9 (iOS)", htmlAccept, false},
		{"Mozilla/5.0 (compatible; Shadowrocket/2070)", htmlAccept, false},
		{"Mozilla/5.0 (Windows NT 10.0) clash-verge/1.7", htmlAccept, false},
		{"Mozilla/5.0 (Android) Hiddify/2.5", htmlAccept, false},
		{"Mozilla/5.0 (Android) okhttp/4.12", htmlAccept, false},
		{"v2rayNG/1.9.16", htmlAccept, false},
		{"", htmlAccept, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/sub/x", nil)
		r.Header.Set("User-Agent", c.ua)
		r.Header.Set("Accept", c.accept)
		if got := IsBrowser(r); got != c.want {
			t.Errorf("%q / %q -> %v want %v", c.ua, c.accept, got, c.want)
		}
	}
}

func pageOf(t *testing.T, w *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	body := w.Body.String()
	const pre = "window.__VEILON_SUB__="
	i := strings.Index(body, pre)
	if i < 0 {
		t.Fatalf("no inlined data: %.300s", body)
	}
	j := strings.Index(body[i:], ";</script>")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(body[i+len(pre):i+j]), &cfg); err != nil {
		t.Fatal(err)
	}
	return body, cfg
}

func TestNegotiationBrowserGetsPage(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	for _, ua := range []string{chromeUA, safariUA, firefoxUA} {
		w := negRequest(s, negCase{"b", ua, htmlAccept, ""})
		if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: %d %s", ua, w.Code, w.Header().Get("Content-Type"))
		}
		for _, h := range []string{"subscription-userinfo", "profile-title", "profile-update-interval", "Content-Disposition"} {
			if w.Header().Get(h) != "" {
				t.Fatalf("page must not carry %s", h)
			}
		}
		if w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("page security headers: %v", w.Header())
		}
		body, cfg := pageOf(t, w)
		if !strings.Contains(body, `<base href="/sub/">`) || cfg["lang"] != "fa" || !strings.Contains(body, `<html lang="fa" dir="rtl"`) {
			t.Fatalf("injection: %.300s", body)
		}
		d := cfg["data"].(map[string]any)
		if d["username"] != "carol" || d["status"] != "active" || len(d["links"].([]any)) != 2 {
			t.Fatalf("data: %v", d)
		}
	}
	// ?format=html forces the page even without browser headers.
	if w := negRequest(s, negCase{"f", "curl/8", "*/*", "?format=html"}); w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("format=html must force the page")
	}
}

// ?format=raw in a browser returns the subscription a client gets (the
// format still follows the User-Agent).
func TestNegotiationRawOverride(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	browser := negRequest(s, negCase{"raw", chromeUA, htmlAccept, "?format=raw"})
	plain := negRequest(s, negCase{"plain", chromeUA, "*/*", ""})
	if dumpResponse("x", browser) != dumpResponse("x", plain) {
		t.Fatalf("format=raw differs:\n%s\n%s", dumpResponse("x", browser), dumpResponse("x", plain))
	}
	if browser.Header().Get("subscription-userinfo") == "" || browser.Header().Get("profile-update-interval") != "8" {
		t.Fatal("raw override lost the subscription headers")
	}
	clash := negRequest(s, negCase{"raw", "mihomo/1.18", htmlAccept, "?format=raw"})
	if clash.Header().Get("Content-Type") != "text/yaml; charset=utf-8" {
		t.Fatal("format=raw must keep User-Agent detection")
	}
}

// Without a page in the build (placeholder UI) a browser gets the
// subscription as before.
func TestNegotiationNoPageFallsBack(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	s.UI = fstest.MapFS{"index.html": {Data: []byte("x")}}
	w := negRequest(s, negCase{"b", chromeUA, htmlAccept, ""})
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("subscription-userinfo") == "" {
		t.Fatalf("fallback: %v", w.Header())
	}
}

func TestStatusJSONFields(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	w := negRequest(s, negCase{"s", chromeUA, "application/json", "?format=status"})
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	var d map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	want := []string{"username", "status", "up", "down", "used", "limit", "expire_at", "last_online_at", "online", "sub_url", "title", "update_hours", "timezone", "links"}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(want)
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("fields %v want %v", keys, want)
	}
	if d["up"].(float64) != 3<<30 || d["down"].(float64) != 9<<30 || d["used"].(float64) != 12<<30 || d["limit"].(float64) != 50<<30 {
		t.Fatalf("traffic: %v", d)
	}
	if d["expire_at"] != "2026-12-24T20:30:00Z" || d["last_online_at"] != "2026-11-10T09:15:00Z" || d["update_hours"].(float64) != 8 ||
		d["timezone"] != "Asia/Tehran" || d["title"] != "Night Owl" || d["sub_url"] != "http://198.51.100.7/sub/tokCarol" {
		t.Fatalf("meta: %v", d)
	}
	if d["online"] != false {
		t.Fatalf("a user last seen days ago is not online: %v", d["online"])
	}
	links := d["links"].([]any)
	l0 := links[0].(map[string]any)
	if l0["name"] != "VL WS" || l0["protocol"] != "vless" || l0["network"] != "ws" || !strings.HasPrefix(l0["uri"].(string), "vless://") {
		t.Fatalf("link: %v", l0)
	}
	if l1 := links[1].(map[string]any); l1["protocol"] != "shadowsocks" || !strings.HasPrefix(l1["uri"].(string), "ss://") {
		t.Fatalf("link: %v", l1)
	}
	// nothing about the panel, other users or the user's secrets beyond the links
	for _, bad := range []string{"uuid", "password", "sub_token", "panel_path", "note", "admin"} {
		if _, ok := d[bad]; ok {
			t.Fatalf("leaks %s", bad)
		}
	}
	if strings.Contains(w.Body.String(), "panel_path") {
		t.Fatal("leaks panel settings")
	}
}

// Disabled, expired and limited users still see the page, with their status.
func TestPageInactiveUsers(t *testing.T) {
	for _, st := range []string{db.StatusDisabled, db.StatusExpired, db.StatusLimited, db.StatusOnHold} {
		s := negFixture(t, st)
		_, cfg := pageOf(t, negRequest(s, negCase{"b", chromeUA, htmlAccept, ""}))
		if d := cfg["data"].(map[string]any); d["status"] != st || d["username"] != "carol" {
			t.Fatalf("%s: %v", st, d)
		}
		w := negRequest(s, negCase{"s", chromeUA, "", "?format=status"})
		if !strings.Contains(w.Body.String(), `"status":"`+st+`"`) {
			t.Fatalf("%s status json: %s", st, w.Body.String())
		}
	}
	s := negFixture(t, db.StatusActive)
	r := httptest.NewRequest("GET", "/sub/nope", nil)
	r.Header.Set("User-Agent", chromeUA)
	r.Header.Set("Accept", htmlAccept)
	w := httptest.NewRecorder()
	s.Serve(w, r, "nope", false)
	if w.Code != 404 || strings.Contains(w.Body.String(), "__VEILON_SUB__") {
		t.Fatalf("unknown token: %d", w.Code)
	}
}

// The inlined JSON cannot close the script tag, whatever a name contains.
func TestPageEscapesData(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	s.DB.Model(&db.User{}).Where("sub_token = ?", "tokCarol").Update("username", "</script><b>x")
	body, cfg := pageOf(t, negRequest(s, negCase{"b", chromeUA, htmlAccept, ""}))
	if strings.Contains(body, "</script><b>") || cfg["data"].(map[string]any)["username"] != "</script><b>x" {
		t.Fatal("payload not escaped")
	}
}

func TestPageAssets(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	get := func(rel string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeAsset(w, httptest.NewRequest("GET", "/sub/"+rel, nil), rel)
		return w
	}
	if w := get("assets/sub-abc.js"); w.Code != 200 || w.Body.String() != "console.log(1)" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d", w.Code)
	}
	if get("favicon.svg").Code != 200 {
		t.Fatal("favicon")
	}
	for _, rel := range []string{"index.html", "sub.html", "assets/../index.html", "assets/missing.js", "assets"} {
		if w := get(rel); w.Code != 404 {
			t.Fatalf("%s: %d", rel, w.Code)
		}
	}
}

// The page's "online" follows the admin panel's rule (users.IsOnline):
// traffic within users.OnlineWindow, whichever server (main or a node)
// stamped last_online_at. Both the inlined data and ?format=status carry it.
func TestPageOnline(t *testing.T) {
	s := negFixture(t, db.StatusActive)
	set := func(ago time.Duration) {
		at := time.Now().UTC().Add(-ago)
		s.DB.Model(&db.User{}).Where("sub_token = ?", "tokCarol").Update("last_online_at", at)
	}
	for _, c := range []struct {
		ago  time.Duration
		want bool
	}{{5 * time.Second, true}, {users.OnlineWindow - 5*time.Second, true}, {users.OnlineWindow + 5*time.Second, false}, {48 * time.Hour, false}} {
		set(c.ago)
		admin := users.IsOnline(func() *time.Time { x := time.Now().UTC().Add(-c.ago); return &x }(), time.Now().UTC())
		if admin != c.want {
			t.Fatalf("%s: admin rule says %v", c.ago, admin)
		}
		var d map[string]any
		w := negRequest(s, negCase{"s", chromeUA, "application/json", "?format=status"})
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		if d["online"] != c.want {
			t.Fatalf("%s ago: status online=%v want %v", c.ago, d["online"], c.want)
		}
		_, cfg := pageOf(t, negRequest(s, negCase{"b", chromeUA, htmlAccept, ""}))
		if cfg["data"].(map[string]any)["online"] != c.want {
			t.Fatalf("%s ago: page online=%v want %v", c.ago, cfg["data"].(map[string]any)["online"], c.want)
		}
	}
	// never connected
	s.DB.Model(&db.User{}).Where("sub_token = ?", "tokCarol").Update("last_online_at", nil)
	var d map[string]any
	_ = json.Unmarshal(negRequest(s, negCase{"s", chromeUA, "application/json", "?format=status"}).Body.Bytes(), &d)
	if d["online"] != false || d["last_online_at"] != nil {
		t.Fatalf("never seen: %v %v", d["online"], d["last_online_at"])
	}
}
