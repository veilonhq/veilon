package veiloncore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFetchSubscriptionSelfSignedIP mimics a typical panel sub server on a
// bare IP:port with a self-signed certificate (httptest's cert is signed by
// an untrusted CA): base64 body of share links + the usual headers.
//
// 1.0.8 (audit H1): without an approved pin nothing is fetched: the core
// reads the certificate in a bare TLS handshake (no HTTP request reaches the
// server, so no path/token/device headers) and returns
// err.subCertUntrusted:<sha256>. With that fingerprint approved
// (SetSubPins) the import succeeds and keeps the header metadata; a
// different pin is refused with the real fingerprint.
func TestFetchSubscriptionSelfSignedIP(t *testing.T) {
	defer func() { _ = setSubPins("") }()
	links := strings.Join([]string{
		"vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?type=tcp&security=tls&sni=a.test#srv-one",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"srv-two","add":"198.51.100.11","port":"8080","id":"11111111-2222-3333-4444-555555555556","aid":"0","net":"ws","path":"/x","host":"b.test","tls":""}`)),
		"trojan://pw@198.51.100.12:443?security=tls&sni=c.test#srv-three",
	}, "\n")
	body := base64.StdEncoding.EncodeToString([]byte(links))
	var gotUA, gotHWID []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = append(gotUA, r.Header.Get("User-Agent"))
		gotHWID = append(gotHWID, r.Header.Get("X-Hwid"))
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
	sum := sha256.Sum256(srv.Certificate().Raw)
	want := hex.EncodeToString(sum[:])
	url := srv.URL + "/subpath/token"
	dev := Device{HWID: "h", AppVer: "1.0.1"}

	// 1. No pin: refused, fingerprint reported, nothing sent.
	_ = setSubPins("")
	res := fetchSubscription(url, dev, "", 5*time.Second)
	if res.HTTPStatusCode != 0 || len(res.Profiles) != 0 {
		t.Fatalf("unpinned: status %d profiles %d", res.HTTPStatusCode, len(res.Profiles))
	}
	if len(res.Errors) != 1 || res.Errors[0] != "err.subCertUntrusted:"+want {
		t.Fatalf("unpinned errors %v (want fingerprint %s)", res.Errors, want)
	}
	if len(gotUA) != 0 {
		t.Fatalf("unpinned: %d HTTP requests reached the server (want 0)", len(gotUA))
	}

	// 2. A different pin: refused with the real fingerprint, nothing sent.
	key := strings.TrimPrefix(srv.URL, "https://")
	if err := setSubPins(`{"` + key + `":"` + strings.Repeat("ab", 32) + `"}`); err != nil {
		t.Fatal(err)
	}
	res = fetchSubscription(url, dev, "", 5*time.Second)
	if res.HTTPStatusCode != 0 || len(res.Errors) != 1 || res.Errors[0] != "err.subCertUntrusted:"+want {
		t.Fatalf("wrong pin: status %d errors %v", res.HTTPStatusCode, res.Errors)
	}
	if len(gotUA) != 0 {
		t.Fatalf("wrong pin: %d HTTP requests reached the server (want 0)", len(gotUA))
	}

	// 3. Approved pin (colon-separated upper case is accepted too).
	var colon []string
	for i := 0; i < len(want); i += 2 {
		colon = append(colon, strings.ToUpper(want[i:i+2]))
	}
	if err := setSubPins(`{"` + key + `":"` + strings.Join(colon, ":") + `"}`); err != nil {
		t.Fatal(err)
	}
	res = fetchSubscription(url, dev, "", 5*time.Second)
	if res.HTTPStatusCode != 200 {
		t.Fatalf("pinned: status %d errors %v", res.HTTPStatusCode, res.Errors)
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
	if len(gotUA) == 0 || !strings.HasPrefix(gotUA[0], "Veilon/") || gotHWID[0] != "h" {
		t.Errorf("UA %v hwid %v", gotUA, gotHWID)
	}
}

// TestFetchSubscriptionUntrustedDomain: a host name with a certificate that
// does not verify is never fetched (no fallback, no pin, no request).
func TestFetchSubscriptionUntrustedDomain(t *testing.T) {
	defer func() { _ = setSubPins("") }()
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	// Even a pin for the IP must not apply to a name.
	sum := sha256.Sum256(srv.Certificate().Raw)
	_ = setSubPins(`{"127.0.0.1:` + port + `":"` + hex.EncodeToString(sum[:]) + `"}`)
	res := fetchSubscription("https://localhost:"+port+"/sub/token", Device{HWID: "h"}, "", 5*time.Second)
	if res.HTTPStatusCode != 0 || len(res.Errors) != 1 || !strings.HasPrefix(res.Errors[0], "err.subCertInvalid") {
		t.Fatalf("domain: status %d errors %v", res.HTTPStatusCode, res.Errors)
	}
	if strings.Contains(res.Errors[0], "token") {
		t.Errorf("error text contains the URL: %q", res.Errors[0])
	}
	if hits != 0 {
		t.Fatalf("domain: %d HTTP requests reached the server (want 0)", hits)
	}
}

func TestSetSubPins(t *testing.T) {
	defer func() { _ = setSubPins("") }()
	fp := strings.Repeat("0a", 32)
	if err := setSubPins(`{"203.0.113.5:2096":"` + fp + `","example.com:443":"` + fp + `","203.0.113.6":"` + fp + `","203.0.113.7:1":"zz"}`); err != nil {
		t.Fatal(err)
	}
	if got := subPinFor("https://203.0.113.5:2096/s/x"); got != fp {
		t.Errorf("ip pin %q", got)
	}
	for _, u := range []string{"https://example.com/s", "http://203.0.113.5:2096/s", "https://203.0.113.5/s", "https://203.0.113.7:1/"} {
		if got := subPinFor(u); got != "" {
			t.Errorf("%s: unexpected pin %q", u, got)
		}
	}
	if err := setSubPins("{bad"); err == nil {
		t.Error("bad JSON accepted")
	}
	if SetSubPins("") != nil || subPinFor("https://203.0.113.5:2096/s/x") != "" {
		t.Error("clear failed")
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
