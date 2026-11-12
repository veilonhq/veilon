package veiloncore

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFetchSubscriptionSelfSignedIP mimics a typical panel sub server on a
// bare IP:port with a self-signed certificate (httptest's cert is signed by
// an untrusted CA): base64 body of share links + the usual headers. The
// import must succeed (verification fallback) and keep the header metadata.
func TestFetchSubscriptionSelfSignedIP(t *testing.T) {
	links := strings.Join([]string{
		"vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?type=tcp&security=tls&sni=a.test#srv-one",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"srv-two","add":"198.51.100.11","port":"8080","id":"11111111-2222-3333-4444-555555555556","aid":"0","net":"ws","path":"/x","host":"b.test","tls":""}`)),
		"trojan://pw@198.51.100.12:443?security=tls&sni=c.test#srv-three",
	}, "\n")
	body := base64.StdEncoding.EncodeToString([]byte(links))
	var gotUA []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = append(gotUA, r.Header.Get("User-Agent"))
		if r.URL.Path != "/subpath/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Subscription-Userinfo", "upload=100; download=200; total=1073741824; expire=1893456000")
		w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("My Sub")))
		w.Header().Set("Profile-Update-Interval", "12")
		// Some panels ignore ?format=… and serves the same body again.
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	if !strings.HasPrefix(srv.URL, "https://127.0.0.1:") {
		t.Fatalf("unexpected test server URL shape")
	}
	res := fetchSubscription(srv.URL+"/subpath/token", Device{HWID: "h", AppVer: "1.0.1"}, "", 5*time.Second)
	if res.HTTPStatusCode != 200 {
		t.Fatalf("status %d errors %v", res.HTTPStatusCode, res.Errors)
	}
	if len(res.Profiles) != 3 {
		t.Fatalf("profiles %d (want 3), errors %v", len(res.Profiles), res.Errors)
	}
	if !res.UserInfo.Present || res.UserInfo.Total != 1073741824 || res.UserInfo.Download != 200 {
		t.Errorf("userinfo %+v", res.UserInfo)
	}
	if res.Title != "My Sub" || res.UpdateHours != 12 {
		t.Errorf("title %q hours %d", res.Title, res.UpdateHours)
	}
	if len(gotUA) == 0 || !strings.HasPrefix(gotUA[0], "Veilon/") {
		t.Errorf("UA %v", gotUA)
	}
}

// A network error (not a certificate problem) must not trigger the
// insecure retry.
func TestIsCertError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	_, err := httpClient("", 3*time.Second).Get(srv.URL)
	if err == nil || !isCertError(err) {
		t.Fatalf("self-signed: isCertError=false (%v)", err)
	}
	_, err = httpClient("", 2*time.Second).Get("https://127.0.0.1:1/")
	if err == nil || isCertError(err) {
		t.Fatalf("refused: isCertError=true (%v)", err)
	}
}
