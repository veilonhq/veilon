package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func dirWithProfiles(p ...string) *acmeDirectory {
	d := &acmeDirectory{}
	d.Meta.Profiles = map[string]string{}
	for _, n := range p {
		d.Meta.Profiles[n] = "test"
	}
	return d
}

func TestBuildNewOrderIPShortLived(t *testing.T) {
	d := dirWithProfiles("classic", "shortlived", "tlsserver")
	req, err := buildNewOrder(d, Identifiers("203.0.113.7", "2001:0db8:0000::0001"), ProfileShortLived)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(req)
	want := `{"identifiers":[{"type":"ip","value":"203.0.113.7"},{"type":"ip","value":"2001:db8::1"}],"profile":"shortlived"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestBuildNewOrderDomainDefaultProfile(t *testing.T) {
	req, err := buildNewOrder(dirWithProfiles(), Identifiers("Panel.Example.COM"), "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(req)
	if want := `{"identifiers":[{"type":"dns","value":"panel.example.com"}]}`; string(b) != want {
		t.Fatalf("got %s want %s", b, want)
	}
}

func TestBuildNewOrderProfileUnsupported(t *testing.T) {
	_, err := buildNewOrder(dirWithProfiles("classic"), Identifiers("203.0.113.7"), ProfileShortLived)
	if !errors.Is(err, ErrProfileUnsupported) {
		t.Fatalf("want ErrProfileUnsupported, got %v", err)
	}
	_, err = buildNewOrder(&acmeDirectory{}, Identifiers("203.0.113.7"), ProfileShortLived)
	if !errors.Is(err, ErrProfileUnsupported) {
		t.Fatalf("want ErrProfileUnsupported for CA without profiles, got %v", err)
	}
	if _, err := buildNewOrder(dirWithProfiles(), nil, ""); err == nil {
		t.Fatal("empty identifiers accepted")
	}
}

func TestBuildCSR(t *testing.T) {
	csr := buildCSR(Identifiers("203.0.113.7", "2001:db8::1"))
	if csr.Subject.CommonName != "" || len(csr.IPAddresses) != 2 || len(csr.DNSNames) != 0 {
		t.Fatalf("ip csr: %+v", csr)
	}
	csr = buildCSR(Identifiers("a.example.com"))
	if csr.Subject.CommonName != "a.example.com" || len(csr.DNSNames) != 1 || len(csr.IPAddresses) != 0 {
		t.Fatalf("dns csr: %+v", csr)
	}
}

// fakeCA records the decoded newOrder payload.
func TestNewOrderSendsProfileAndIP(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	var protected map[string]any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Replay-Nonce", "n"+time.Now().Format("150405.000000000"))
		switch r.URL.Path {
		case "/dir":
			_, _ = io.WriteString(w, `{"newNonce":"`+srv.URL+`/nonce","newAccount":"`+srv.URL+`/acct","newOrder":"`+srv.URL+`/order","meta":{"profiles":{"classic":"x","shortlived":"y"}}}`)
		case "/nonce":
		case "/acct":
			w.Header().Set("Location", srv.URL+"/acct/1")
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"status":"valid"}`)
		case "/order":
			var jws struct{ Protected, Payload string }
			_ = json.NewDecoder(r.Body).Decode(&jws)
			pb, _ := base64.RawURLEncoding.DecodeString(jws.Payload)
			hb, _ := base64.RawURLEncoding.DecodeString(jws.Protected)
			mu.Lock()
			_ = json.Unmarshal(pb, &got)
			_ = json.Unmarshal(hb, &protected)
			mu.Unlock()
			w.Header().Set("Location", srv.URL+"/order/1")
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"status":"pending","profile":"shortlived","identifiers":[{"type":"ip","value":"203.0.113.7"}],"authorizations":["`+srv.URL+`/authz/1"],"finalize":"`+srv.URL+`/fin"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c := &acmeClient{DirectoryURL: srv.URL + "/dir", Key: k}
	ctx := context.Background()
	if err := c.Register(ctx, "a@example.com"); err != nil {
		t.Fatal(err)
	}
	o, err := c.NewOrder(ctx, Identifiers("203.0.113.7"), ProfileShortLived)
	if err != nil {
		t.Fatal(err)
	}
	if o.URL != srv.URL+"/order/1" || o.Profile != "shortlived" || len(o.Authorizations) != 1 {
		t.Fatalf("order: %+v", o)
	}
	mu.Lock()
	defer mu.Unlock()
	if got["profile"] != "shortlived" {
		t.Fatalf("payload profile: %v", got)
	}
	ids := got["identifiers"].([]any)
	if id := ids[0].(map[string]any); id["type"] != "ip" || id["value"] != "203.0.113.7" {
		t.Fatalf("payload identifiers: %v", ids)
	}
	if protected["kid"] != srv.URL+"/acct/1" || protected["jwk"] != nil || protected["alg"] != "ES256" {
		t.Fatalf("protected header: %v", protected)
	}
}

func TestRenewAtShortLived(t *testing.T) {
	nb := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := &db.Cert{Source: "acme", AutoRenew: true, Profile: ProfileShortLived, NotBefore: &nb, NotAfter: nb.Add(160 * time.Hour)}
	if got, want := RenewAt(c), nb.Add(72*time.Hour); !got.Equal(want) {
		t.Fatalf("RenewAt = %v want %v", got, want)
	}
	if renewDue(c, nb.Add(71*time.Hour)) {
		t.Fatal("due too early")
	}
	if !renewDue(c, nb.Add(73*time.Hour)) {
		t.Fatal("not due at 73h")
	}
	// After a failure: retry hourly, not on every check.
	last := nb.Add(73 * time.Hour)
	c.LastError, c.LastAttemptAt = "boom", &last
	if renewDue(c, last.Add(30*time.Minute)) {
		t.Fatal("retried within backoff")
	}
	if !renewDue(c, last.Add(61*time.Minute)) {
		t.Fatal("no retry after backoff")
	}
	// No NotBefore recorded: estimated from the profile.
	c2 := &db.Cert{Source: "acme", AutoRenew: true, Profile: ProfileShortLived, NotAfter: nb.Add(160 * time.Hour)}
	if got := RenewAt(c2); !got.Equal(nb.Add(72 * time.Hour)) {
		t.Fatalf("estimated RenewAt = %v", got)
	}
	// Very short lifetimes renew at half life.
	c3 := &db.Cert{Source: "acme", AutoRenew: true, NotBefore: &nb, NotAfter: nb.Add(24 * time.Hour)}
	if got := RenewAt(c3); !got.Equal(nb.Add(12 * time.Hour)) {
		t.Fatalf("1-day RenewAt = %v", got)
	}
}

func TestRenewAtDefault90d(t *testing.T) {
	nb := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := &db.Cert{Source: "acme", AutoRenew: true, NotBefore: &nb, NotAfter: nb.Add(90 * 24 * time.Hour)}
	if got, want := RenewAt(c), nb.Add(60*24*time.Hour); !got.Equal(want) {
		t.Fatalf("RenewAt = %v want %v", got, want)
	}
	last := nb.Add(61 * 24 * time.Hour)
	c.LastError, c.LastAttemptAt = "x", &last
	if renewDue(c, last.Add(2*time.Hour)) || !renewDue(c, last.Add(7*time.Hour)) {
		t.Fatal("default backoff should be 6h")
	}
	c.AutoRenew = false
	if renewDue(c, last.Add(30*24*time.Hour)) {
		t.Fatal("auto_renew off but due")
	}
	up := &db.Cert{Source: "upload", AutoRenew: true, NotBefore: &nb, NotAfter: nb.Add(time.Hour)}
	if renewDue(up, nb.Add(2*time.Hour)) {
		t.Fatal("uploaded cert renewed")
	}
}

func TestNormalize(t *testing.T) {
	s := &Service{DB: testutil.DB(t)}
	ctx := context.Background()
	r := ACMERequest{Type: "ip", IP: "203.0.113.7", IPv6: "2001:db8:1::5"}
	// 203.0.113.0/24 is TEST-NET-3 but counts as public for IsPublicIP;
	// 2001:db8::/32 (documentation) is rejected.
	if _, _, err := s.Normalize(ctx, &r); err == nil {
		t.Fatal("documentation IPv6 accepted")
	}
	r = ACMERequest{Type: "ip", IP: "203.0.113.7", IPv6: "2a01:4f8::1", HTTPPort: 8080}
	ids, prof, err := s.Normalize(ctx, &r)
	if err != nil {
		t.Fatal(err)
	}
	if prof != ProfileShortLived || len(ids) != 2 || ids[1].Value != "2a01:4f8::1" || r.HTTPPort != 8080 {
		t.Fatalf("ids=%v prof=%q req=%+v", ids, prof, r)
	}
	for _, bad := range []string{"10.0.0.1", "127.0.0.1", "100.64.1.1", "192.168.1.1", "not-an-ip"} {
		r := ACMERequest{Type: "ip", IP: bad}
		if _, _, err := s.Normalize(ctx, &r); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
	r = ACMERequest{Type: "ip", IP: "203.0.113.7", IPv6: "203.0.113.8"}
	if _, _, err := s.Normalize(ctx, &r); err == nil {
		t.Fatal("IPv4 in ipv6 field accepted")
	}
	r = ACMERequest{Domain: "Panel.Example.com."}
	ids, prof, err = s.Normalize(ctx, &r)
	if err != nil || prof != "" || ids[0].Type != "dns" || ids[0].Value != "panel.example.com" || r.Type != "domain" || r.HTTPPort != 80 {
		t.Fatalf("domain: ids=%v prof=%q err=%v", ids, prof, err)
	}
	for _, bad := range []string{"", "localhost", "*.example.com", "a..b", "-a.com", "1.2.3.4"} {
		r := ACMERequest{Type: "domain", Domain: bad}
		if _, _, err := s.Normalize(ctx, &r); err == nil {
			t.Fatalf("domain %q accepted", bad)
		}
	}
	r = ACMERequest{Type: "ip", IP: "203.0.113.7", HTTPPort: 70000}
	if _, _, err := s.Normalize(ctx, &r); err == nil {
		t.Fatal("bad port accepted")
	}
	s.AllowNonPublicIPs = true
	r = ACMERequest{Type: "ip", IP: "127.0.0.1"}
	if _, _, err := s.Normalize(ctx, &r); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeIPs(t *testing.T) {
	res := summarizeIPs(func(y func(ip, src string)) {
		y("198.51.100.9", "interface")
		y("203.0.113.7", "api.ipify.org")
		y("203.0.113.7", "icanhazip.com")
		y("2a01:4f8::1", "api.ipify.org")
	})
	if res.IPv4 != "203.0.113.7" || res.IPv6 != "2a01:4f8::1" || len(res.Candidates) != 3 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(strings.Join(res.Candidates[0].Sources, ","), "icanhazip") {
		t.Fatalf("best candidate first: %+v", res.Candidates)
	}
}

func TestServeHTTP01(t *testing.T) {
	s := &Service{}
	s.challenges.Store("/.well-known/acme-challenge/tok", "tok.thumb")
	rec := httptest.NewRecorder()
	if !s.ServeHTTP01(rec, httptest.NewRequest("GET", "/.well-known/acme-challenge/tok", nil)) || rec.Body.String() != "tok.thumb" {
		t.Fatalf("challenge not served: %q", rec.Body.String())
	}
	if s.ServeHTTP01(httptest.NewRecorder(), httptest.NewRequest("GET", "/.well-known/acme-challenge/other", nil)) {
		t.Fatal("unknown token served")
	}
}
