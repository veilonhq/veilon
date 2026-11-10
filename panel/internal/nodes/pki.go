// Package nodes connects the panel (the master) to remote servers running
// "veilon node" (the agents). The two talk HTTPS with mutual TLS: a private
// CA kept in the master's data directory signs one client certificate for
// the master and one server certificate per node. A node only accepts the
// master's certificate; the master only accepts the certificate it issued
// for that node (pinned by SHA-256).
package nodes

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MasterCN is the common name of the master's client certificate.
const MasterCN = "veilon-master"

// ServerName is the name in node certificates; the master dials nodes with
// it as the TLS server name, so the node's IP or domain may change freely.
func ServerName(id int64) string { return fmt.Sprintf("node-%d.veilon.internal", id) }

// PKI is the master's certificate authority for nodes.
type PKI struct {
	Dir string // <data>/nodes

	mu     sync.Mutex
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte
	client *tls.Certificate
}

func (p *PKI) path(n string) string { return filepath.Join(p.Dir, n) }

// load creates the CA and the master's client certificate on first use.
func (p *PKI) load() error {
	if p.caCert != nil && p.client != nil {
		return nil
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(p.path("ca.crt")); errors.Is(err, os.ErrNotExist) {
		if err := p.createCA(); err != nil {
			return fmt.Errorf("create node CA: %w", err)
		}
	}
	certPEM, err := os.ReadFile(p.path("ca.crt"))
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(p.path("ca.key"))
	if err != nil {
		return err
	}
	cert, key, err := parsePair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("node CA: %w", err)
	}
	p.caCert, p.caKey, p.caPEM = cert, key, certPEM
	if _, err := os.Stat(p.path("master.crt")); errors.Is(err, os.ErrNotExist) {
		c, k, err := p.issue(MasterCN, nil, x509.ExtKeyUsageClientAuth)
		if err != nil {
			return err
		}
		if err := writePair(p.path("master.crt"), p.path("master.key"), c, k); err != nil {
			return err
		}
	}
	pair, err := tls.LoadX509KeyPair(p.path("master.crt"), p.path("master.key"))
	if err != nil {
		return fmt.Errorf("master client certificate: %w", err)
	}
	p.client = &pair
	return nil
}

func (p *PKI) createCA() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Veilon node CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return writePair(p.path("ca.crt"), p.path("ca.key"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// issue signs a leaf certificate (PEM cert, PEM key).
func (p *PKI) issue(cn string, dns []string, usage x509.ExtKeyUsage) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dns,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		return nil, nil, err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), nil
}

// Enrollment is what a node needs to start: the CA (to check the master),
// its own server certificate and key, and the port to listen on.
type Enrollment struct {
	V    int    `json:"v"`
	ID   int64  `json:"id"`
	CA   string `json:"ca"`
	Cert string `json:"cert"`
	Key  string `json:"key"`
	Port int    `json:"port"`
}

// Token encodes the enrollment for a command line.
func (e *Enrollment) Token() string {
	b, _ := json.Marshal(e)
	return "vn1." + base64.RawURLEncoding.EncodeToString(b)
}

// ParseToken decodes a token made by Token.
func ParseToken(tok string) (*Enrollment, error) {
	tok = strings.TrimSpace(tok)
	raw, ok := strings.CutPrefix(tok, "vn1.")
	if !ok {
		return nil, errors.New("not a Veilon node token")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("node token: %w", err)
	}
	var e Enrollment
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("node token: %w", err)
	}
	if e.CA == "" || e.Cert == "" || e.Key == "" || e.Port <= 0 || e.Port > 65535 {
		return nil, errors.New("node token is incomplete")
	}
	if _, err := tls.X509KeyPair([]byte(e.Cert), []byte(e.Key)); err != nil {
		return nil, fmt.Errorf("node token: %w", err)
	}
	return &e, nil
}

// Enroll issues a new server certificate for node id. It returns the
// enrollment and the SHA-256 of the certificate (to pin).
func (p *PKI) Enroll(id int64, port int) (*Enrollment, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.load(); err != nil {
		return nil, "", err
	}
	c, k, err := p.issue(ServerName(id), []string{ServerName(id)}, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return nil, "", err
	}
	return &Enrollment{V: 1, ID: id, CA: string(p.caPEM), Cert: string(c), Key: string(k), Port: port}, fingerprintPEM(c), nil
}

// ClientTLS returns the TLS config to dial node id: the master's client
// certificate, the CA as the only root, and the node certificate pinned.
func (p *PKI) ClientTLS(id int64, pin string) (*tls.Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.load(); err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(p.caCert)
	pin = strings.ToLower(pin)
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      pool,
		ServerName:   ServerName(id),
		Certificates: []tls.Certificate{*p.client},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("node sent no certificate")
			}
			got := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if pin != "" && hex.EncodeToString(got[:]) != pin {
				return errors.New("node certificate does not match the one issued for it (run the node install command again)")
			}
			return nil
		},
	}, nil
}

// ServerTLS is the agent side: present the node certificate and accept only
// clients with a certificate from the same CA made for the master.
func ServerTLS(e *Enrollment) (*tls.Config, error) {
	pair, err := tls.X509KeyPair([]byte(e.Cert), []byte(e.Key))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(e.CA)) {
		return nil, errors.New("bad CA certificate")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		VerifyConnection: func(cs tls.ConnectionState) error {
			// The chain was verified with the client-auth usage, so node
			// certificates (server-auth only) cannot be used here.
			if len(cs.PeerCertificates) == 0 || cs.PeerCertificates[0].Subject.CommonName != MasterCN {
				return errors.New("only the master may connect")
			}
			return nil
		},
	}, nil
}

func fingerprintPEM(certPEM []byte) string {
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		return ""
	}
	s := sha256.Sum256(blk.Bytes)
	return hex.EncodeToString(s[:])
}

func parsePair(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, errors.New("bad PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func writePair(certPath, keyPath string, certPEM, keyPEM []byte) error {
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(certPath, certPEM, 0o644)
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	return n
}
