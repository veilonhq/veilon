package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// GenerateSelfSigned returns a 10-year ECDSA P-256 self-signed certificate
// and key (PEM) for domain.
func GenerateSelfSigned(domain string) (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(domain); ip != nil {
		tpl.IPAddresses = []net.IP{ip}
	} else {
		tpl.DNSNames = []string{domain}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})), nil
}

// SelfSigned creates and stores a self-signed certificate (source "self").
func (s *Service) SelfSigned(domain string) (int64, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" || strings.ContainsAny(domain, " /:") {
		return 0, &ValidationError{Msg: "a valid domain name is required for the self-signed certificate"}
	}
	cp, kp, err := GenerateSelfSigned(domain)
	if err != nil {
		return 0, err
	}
	leaf, err := ParsePair(cp, kp)
	if err != nil {
		return 0, err
	}
	nb := leaf.NotBefore.UTC()
	c := &db.Cert{Domain: domain, Kind: "domain", Issuer: "self-signed", NotBefore: &nb, NotAfter: leaf.NotAfter.UTC(), Source: "self", CertPEM: cp, KeyPEM: kp}
	if err := s.DB.Create(c).Error; err != nil {
		return 0, err
	}
	if err := s.writeFiles(c); err != nil {
		return 0, err
	}
	s.changed()
	return c.ID, nil
}

// Pins are the fingerprints clients use to pin a certificate.
type Pins struct {
	// CertSHA256 is the hex SHA-256 of the leaf certificate (DER).
	CertSHA256 string
	// SPKISHA256 is the base64 SHA-256 of the leaf's SubjectPublicKeyInfo.
	SPKISHA256 string
}

// PinsOf computes the pins of a PEM certificate chain's leaf.
func PinsOf(certPEM string) (Pins, bool) {
	blk, _ := pem.Decode([]byte(certPEM))
	if blk == nil {
		return Pins{}, false
	}
	sum := sha256.Sum256(blk.Bytes)
	p := Pins{CertSHA256: hex.EncodeToString(sum[:])}
	if c, err := x509.ParseCertificate(blk.Bytes); err == nil {
		s := sha256.Sum256(c.RawSubjectPublicKeyInfo)
		p.SPKISHA256 = base64.StdEncoding.EncodeToString(s[:])
	}
	return p, true
}
