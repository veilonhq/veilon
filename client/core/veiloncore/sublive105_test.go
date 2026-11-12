package veiloncore

// Live check of a real subscription (Run 105): fetch through the app's
// FetchSubscription, then ping every profile through URLTestError with each
// core. The URL is read from $VEILON_SUB_URL_FILE and never logged; names
// are logged, never addresses / keys (read logs through redact2.sh).

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestLiveSubscription(t *testing.T) {
	path := os.Getenv("VEILON_SUB_URL_FILE")
	if path == "" {
		t.Skip("set VEILON_SUB_URL_FILE")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r SubResult
	if err := json.Unmarshal([]byte(FetchSubscription(strings.TrimSpace(string(b)), `{"package":"app.veilon"}`, "", 20000)), &r); err != nil {
		t.Fatal(err)
	}
	t.Logf("SUB status %d, %d profiles, %d errors, title set %v, updateHours %d, userinfo total>0 %v expire>0 %v used>0 %v",
		r.HTTPStatusCode, len(r.Profiles), len(r.Errors), r.Title != "", r.UpdateHours,
		r.UserInfo.Total > 0, r.UserInfo.Expire > 0, r.UserInfo.Upload+r.UserInfo.Download > 0)
	if len(r.Profiles) == 0 {
		t.Fatalf("no profiles imported")
	}
	for _, p := range r.Profiles {
		core, _ := ResolveCore(p, DefaultSettings())
		t.Logf("PROFILE %q type %s security %s net %s flow %q fp %q sni set %v pbk len %d sid len %d spx %q pqv %v auto-core %s",
			p.Name, p.Type, p.Security, p.Network, p.Flow, p.Fingerprint, p.SNI != "", len(p.PublicKey), len(p.ShortID), p.SpiderX, p.PQV != "", core)
		pj, _ := json.Marshal(p)
		for _, c := range []string{"auto", "sing-box", "xray"} {
			res := URLTestError(string(pj), appSettingsJSON(t, c), "http://www.google.com/generate_204", 12000)
			t.Logf("PING %q core %s: %s", p.Name, c, redactIPs(res))
		}
		if p.Security == "reality" && os.Getenv("VEILON_SUB_FP") == "1" {
			// sing-box REALITY with other uTLS fingerprints (diagnosis).
			for _, fp := range []string{"firefox", "safari", "edge", "ios", "chrome_psk", "360", "qq", "android"} {
				q := *p
				q.Fingerprint = fp
				qj, _ := json.Marshal(&q)
				res := URLTestError(string(qj), appSettingsJSON(t, "sing-box"), "http://www.google.com/generate_204", 12000)
				t.Logf("PING %q sing-box fp %s: %s", p.Name, fp, redactIPs(res))
			}
		}
	}
}

var ipRe = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)

// redactIPs masks anything that looks like an IPv4 address.
func redactIPs(s string) string { return ipRe.ReplaceAllString(s, "<ip>") }
