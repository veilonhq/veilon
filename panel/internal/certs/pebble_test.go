package certs

// Integration test against Pebble, the ACME test CA (it supports profiles
// and IP identifiers). Runs only when VEILON_PEBBLE is set to a directory
// containing the pebble binary ("pebble") and its test TLS files
// (cert.pem, key.pem):
//
//	VEILON_PEBBLE=/path/to/pebble-dir go test ./panel/internal/certs -run Pebble -v

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestPebbleIPCertificate(t *testing.T) {
	dir := os.Getenv("VEILON_PEBBLE")
	if dir == "" {
		t.Skip("VEILON_PEBBLE not set")
	}
	apiPort, mgmtPort, httpPort := freePort(t), freePort(t), freePort(t)
	cfg := map[string]any{"pebble": map[string]any{
		"listenAddress":                  fmt.Sprintf("127.0.0.1:%d", apiPort),
		"managementListenAddress":        fmt.Sprintf("127.0.0.1:%d", mgmtPort),
		"certificate":                    filepath.Join(dir, "cert.pem"),
		"privateKey":                     filepath.Join(dir, "key.pem"),
		"httpPort":                       httpPort,
		"tlsPort":                        freePort(t),
		"externalAccountBindingRequired": false,
		"retryAfter":                     map[string]int{"authz": 1, "order": 1},
		"keyAlgorithm":                   "ecdsa",
		"profiles": map[string]any{
			"default":    map[string]any{"description": "default", "validityPeriod": 7776000},
			"shortlived": map[string]any{"description": "short", "validityPeriod": 576000}, // 160 h
		},
	}}
	cfgFile := filepath.Join(t.TempDir(), "pebble.json")
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgFile, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, "pebble"), "-config", cfgFile)
	cmd.Env = append(os.Environ(), "PEBBLE_VA_NOSLEEP=1")
	logf, _ := os.Create(filepath.Join(t.TempDir(), "pebble.log"))
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // test CA
	dirURL := fmt.Sprintf("https://127.0.0.1:%d/dir", apiPort)
	for i := 0; ; i++ {
		if resp, err := hc.Get(dirURL); err == nil {
			resp.Body.Close()
			break
		}
		if i > 50 {
			t.Fatal("pebble did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}

	gdb := testutil.DB(t)
	changes := 0
	s := &Service{DB: gdb, DataDir: t.TempDir(), ACMEDirectory: dirURL, ACMEHTTPClient: hc, AllowNonPublicIPs: true,
		HTTP01Addr: fmt.Sprintf(":%d", httpPort), OnChange: func() { changes++ }}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// IP certificate: shortlived profile, ip identifier, http-01.
	v, err := s.IssueACME(ctx, ACMERequest{Type: "ip", IP: "127.0.0.1", Email: "ops@example.com", HTTPPort: httpPort})
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != "ip" || v.Profile != ProfileShortLived || v.Source != "acme" || !v.AutoRenew || v.RenewAt == nil {
		t.Fatalf("view: %+v", v)
	}
	leaf := leafOf(t, s, v.ID)
	if len(leaf.IPAddresses) != 1 || !leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) || len(leaf.DNSNames) != 0 {
		t.Fatalf("SANs: ip=%v dns=%v", leaf.IPAddresses, leaf.DNSNames)
	}
	life := leaf.NotAfter.Sub(leaf.NotBefore)
	if life < 159*time.Hour || life > 161*time.Hour {
		t.Fatalf("shortlived lifetime %v", life)
	}
	if got := v.RenewAt.Sub(leaf.NotBefore.UTC()); got != ShortLivedRenewAge {
		t.Fatalf("renew at +%v", got)
	}
	tlsCert, err := s.Certificate(v.ID)
	if err != nil || tlsCert == nil {
		t.Fatal(err)
	}

	// Forced renewal keeps the id, replaces the cert, and invalidates the cache.
	before := changes
	serial := leaf.SerialNumber
	if _, err := s.Renew(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if changes == before {
		t.Fatal("renew did not signal a reload")
	}
	if leafOf(t, s, v.ID).SerialNumber.Cmp(serial) == 0 {
		t.Fatal("renew returned the same certificate")
	}
	tls2, _ := s.Certificate(v.ID)
	if tls2 == tlsCert {
		t.Fatal("TLS cache not refreshed after renew")
	}

	// RenewDue: pretend the certificate is 4 days old.
	old := time.Now().UTC().Add(-96 * time.Hour)
	gdb.Model(&db.Cert{}).Where("id = ?", v.ID).Updates(map[string]any{"not_before": old, "not_after": old.Add(160 * time.Hour)})
	serial = leafOf(t, s, v.ID).SerialNumber
	s.RenewDue(ctx)
	if leafOf(t, s, v.ID).SerialNumber.Cmp(serial) == 0 {
		t.Fatal("RenewDue did not renew a due short-lived certificate")
	}
	s.RenewDue(ctx) // not due now: no change
	var row db.Cert
	gdb.First(&row, v.ID)
	if row.LastError != "" || row.LastAttemptAt == nil {
		t.Fatalf("attempt bookkeeping: %+v", row)
	}

	// IPv4 + IPv6 in one certificate.
	v6, err := s.IssueACME(ctx, ACMERequest{Type: "ip", IP: "127.0.0.1", IPv6: "::1", HTTPPort: httpPort})
	if err != nil {
		t.Fatal(err)
	}
	if l := leafOf(t, s, v6.ID); len(l.IPAddresses) != 2 {
		t.Fatalf("dual-stack SANs: %v", l.IPAddresses)
	}

	// Default profile for a non-IP request path (IP identifier, no profile).
	cp, kp, err := s.obtain(ctx, Identifiers("127.0.0.1"), "", "", httpPort)
	if err != nil {
		t.Fatal(err)
	}
	dl, _ := ParsePair(cp, kp)
	if d := dl.NotAfter.Sub(dl.NotBefore); d < 89*24*time.Hour {
		t.Fatalf("default profile lifetime %v", d)
	}

	// Revoke deletes the certificate; in-use certificates are refused.
	_ = db.SetSetting(gdb, db.KeyPanelCertID, fmt.Sprint(v6.ID))
	if err := s.Revoke(ctx, v6.ID); err == nil {
		t.Fatal("revoked a certificate used by the panel")
	}
	_ = db.SetSetting(gdb, db.KeyPanelCertID, "")
	if err := s.Revoke(ctx, v6.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(v6.ID); err == nil {
		t.Fatal("revoked certificate still listed")
	}
}

func leafOf(t *testing.T, s *Service, id int64) *x509.Certificate {
	t.Helper()
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		t.Fatal(err)
	}
	l, err := ParsePair(c.CertPEM, c.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
