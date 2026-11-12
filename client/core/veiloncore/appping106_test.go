package veiloncore

// Run 106: reproduce the Servers page ping exactly as the app sends it.
//
// Dart AppState.ping → Native.urlTest(profile.encode(), coreSettingsJson(),
// settings.pingUrl, 9000) → Kotlin "urlTest" → Veiloncore.urlTestError(
// profile, Store.withPaths(settings), url, timeout). The profile JSON is the
// map FetchSubscription returned, decoded and re-encoded by Dart with
// "subId" added; the settings are AppSettings.toCoreJson() defaults plus the
// paths Kotlin adds; the URL is the app default (https gstatic).
//
// Live part reads the subscription URL from $VEILON_SUB_URL_FILE and never
// logs it, addresses or keys.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// appCoreSettingsJSON mirrors AppSettings.toCoreJson() (app/lib/core/
// models.dart) with its defaults, plus Store.withPaths (Kotlin).
func appCoreSettingsJSON(t *testing.T, core string, fullHandshake bool) string {
	t.Helper()
	dir := t.TempDir()
	asset := filepath.Join(dir, "rules")
	_ = os.MkdirAll(asset, 0o755)
	if src, err := filepath.Abs("../../app/assets/rules"); err == nil {
		ents, _ := os.ReadDir(src)
		for _, e := range ents {
			b, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err == nil {
				_ = os.WriteFile(filepath.Join(asset, e.Name()), b, 0o644)
			}
		}
	}
	m := map[string]any{
		"mtu": 9000, "remoteDns": "https://1.1.1.1/dns-query", "directDns": "8.8.8.8",
		"disableQuic": false, "bypassIran": true, "bypassLan": true, "blockAds": false, "ipv6": false,
		"proxyRules": []string{}, "directRules": []string{}, "blockRules": []string{},
		"perAppMode": "off", "perAppPackages": []string{}, "appPackage": "app.veilon",
		"logLevel": "warn", "assetDir": asset, "core": core, "fullHandshakePing": fullHandshake,
		"dataDir": filepath.Join(dir, "core"), "tempDir": filepath.Join(dir, "cache"),
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// appStoredProfile is the profile as Dart stores and sends it: the map from
// FetchSubscription's JSON, with subId added, re-encoded.
func appStoredProfile(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["subId"] = "sub1"
	b, _ := json.Marshal(m)
	return string(b)
}

const appPingURL = "https://www.gstatic.com/generate_204"

func TestLiveAppPing(t *testing.T) {
	path := os.Getenv("VEILON_SUB_URL_FILE")
	if path == "" {
		t.Skip("set VEILON_SUB_URL_FILE")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dev := `{"package":"app.veilon","assetDir":"/data/x","hwid":"0123456789abcdef","os":"Android","osVersion":"14","sdk":34,"model":"Test Phone","appVersion":"1.0.4"}`
	var r struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(FetchSubscription(strings.TrimSpace(string(b)), dev, "", 20000)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Profiles) == 0 {
		t.Fatal("no profiles")
	}
	failed := 0
	for _, raw := range r.Profiles {
		pj := appStoredProfile(t, raw)
		p, _ := decodeProfile(pj)
		if p.Security != "reality" {
			continue
		}
		t.Logf("SHAPE %q serverIsIP=%v sniSet=%v sniEqServer=%v", p.Name, net.ParseIP(p.Server) != nil, p.SNI != "", p.SNI == p.Server)
		for _, c := range []struct {
			core string
			full bool
		}{{"auto", false}, {"auto", true}, {"xray", false}, {"sing-box", false}} {
			st := appCoreSettingsJSON(t, c.core, c.full)
			res := URLTestError(pj, st, appPingURL, 9000)
			rc := ResolveCoreFor(pj, st)
			t.Logf("APPPING %q core=%s full=%v resolve=%s → %s", p.Name, c.core, c.full, rc, redactIPs(res))
			if c.core == "auto" && strings.Contains(res, `"delay":-1`) {
				failed++
			}
		}
	}
	if failed > 0 {
		t.Fatalf("%d auto-core app pings failed", failed)
	}
}

// TestLiveAppPingAll is AppState.pingAll: every pingable profile at once
// (up to 6 workers), auto core, app settings.
func TestLiveAppPingAll(t *testing.T) {
	path := os.Getenv("VEILON_SUB_URL_FILE")
	if path == "" {
		t.Skip("set VEILON_SUB_URL_FILE")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	_ = json.Unmarshal([]byte(FetchSubscription(strings.TrimSpace(string(b)), `{"package":"app.veilon"}`, "", 20000)), &r)
	st := appCoreSettingsJSON(t, "auto", false)
	for round := 0; round < 2; round++ {
		type res struct{ name, out string }
		ch := make(chan res, len(r.Profiles))
		n := 0
		for _, raw := range r.Profiles {
			pj := appStoredProfile(t, raw)
			p, _ := decodeProfile(pj)
			if p.Type == TypeAmneziaWG || p.Type == TypeWireGuard {
				continue
			}
			n++
			go func() { ch <- res{p.Name, URLTestError(pj, st, appPingURL, 9000)} }()
		}
		for i := 0; i < n; i++ {
			x := <-ch
			t.Logf("PINGALL round %d %q → %s", round, x.name, redactIPs(x.out))
			if strings.Contains(x.out, `"delay":-1`) {
				t.Errorf("%q failed", x.name)
			}
		}
	}
}

// TestPingXrayConcurrent: the Servers page pings a group with AppState.
// pingAll (up to 6 URLTestError calls at once). Every Reality profile runs
// its own Xray instance in this process; each must answer.
func TestPingXrayConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	a := newPingStandIn(t, "127.0.0.1", true)
	defer a.stop()
	b := newPingStandIn(t, "127.0.0.1", true)
	defer b.stop()
	st := appCoreSettingsJSON(t, "auto", false)
	for round := 0; round < 3; round++ {
		links := []string{a.link("127.0.0.1"), b.link("127.0.0.1"), a.link("127.0.0.1"), b.link("127.0.0.1")}
		ch := make(chan string, len(links))
		for i, l := range links {
			p, err := ParseLink(l)
			if err != nil {
				t.Fatal(err)
			}
			p.Name = fmt.Sprintf("r%d-%d", round, i)
			pj, _ := json.Marshal(p)
			go func() { ch <- p.Name + " " + URLTestError(string(pj), st, "http://ping.test/generate_204", 9000) }()
		}
		for range links {
			r := <-ch
			t.Logf("CONC %s", r)
			if strings.Contains(r, `"delay":-1`) {
				t.Errorf("concurrent Xray ping failed: %s", r)
			}
		}
	}
	// And a single ping afterwards.
	d, e := pingViaAPI(t, a.link("127.0.0.1"), "auto")
	t.Logf("AFTER single ping: %d ms err=%q", d, e)
	if d <= 0 {
		t.Errorf("ping after concurrent pings failed: %s", e)
	}
}
