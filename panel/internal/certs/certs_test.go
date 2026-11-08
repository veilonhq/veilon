package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// SelfSigned returns a PEM cert/key for domain (test helper).
func SelfSigned(t *testing.T, domain string) (string, string) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain},
		Issuer: pkix.Name{Organization: []string{"Test CA"}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))
}

func TestUploadAndDelete(t *testing.T) {
	gdb := testutil.DB(t)
	dir := t.TempDir()
	changed := 0
	s := &Service{DB: gdb, DataDir: dir, OnChange: func() { changed++ }}
	cp, kp := SelfSigned(t, "a.example.com")
	if _, err := s.Upload("", "garbage", kp); err == nil {
		t.Fatal("garbage accepted")
	}
	v, err := s.Upload("", cp, kp)
	if err != nil {
		t.Fatal(err)
	}
	if v.Domain != "a.example.com" || v.Source != "upload" || v.NotAfter.Before(time.Now()) {
		t.Fatalf("bad view %+v", v)
	}
	cf, kf := Paths(dir, v.ID)
	if _, err := os.Stat(cf); err != nil {
		t.Fatal("cert file missing")
	}
	if st, _ := os.Stat(kf); st.Mode().Perm() != 0o600 {
		t.Fatal("key file permissions too open")
	}
	if c, err := s.Certificate(v.ID); err != nil || c == nil {
		t.Fatal("tls certificate load failed")
	}
	if err := s.Delete(v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cf); !os.IsNotExist(err) {
		t.Fatal("cert files not removed")
	}
	if changed != 2 {
		t.Fatalf("OnChange called %d times", changed)
	}
}
