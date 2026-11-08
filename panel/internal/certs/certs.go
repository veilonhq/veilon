// Package certs manages TLS certificates: uploads, ACME (HTTP-01) issuance
// for domains and IP addresses, renewal, and serving certificates to the
// panel listener.
package certs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// View is the API representation of a certificate.
type View struct {
	ID int64 `json:"id"`
	// Domain is the primary name (first identifier) for display.
	Domain string `json:"domain"`
	// Identifiers lists every name or IP the certificate covers.
	Identifiers []string   `json:"identifiers"`
	Type        string     `json:"type"` // domain | ip
	Issuer      string     `json:"issuer"`
	NotBefore   *time.Time `json:"not_before"`
	NotAfter    time.Time  `json:"not_after"`
	AutoRenew   bool       `json:"auto_renew"`
	Source      string     `json:"source"` // acme | upload | self
	Profile     string     `json:"profile"`
	HTTPPort    int        `json:"http_port,omitempty"`
	// RenewAt is when the next automatic renewal is due (ACME + auto_renew only).
	RenewAt       *time.Time `json:"renew_at"`
	LastError     string     `json:"last_error"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	InUse         bool       `json:"in_use"`
}

// ToView converts a model.
func ToView(c *db.Cert) View {
	v := View{ID: c.ID, Identifiers: splitIDs(c.Domain), Type: c.Kind, Issuer: c.Issuer, NotBefore: c.NotBefore,
		NotAfter: c.NotAfter, AutoRenew: c.AutoRenew, Source: c.Source, Profile: c.Profile,
		LastError: c.LastError, LastAttemptAt: c.LastAttemptAt}
	if len(v.Identifiers) > 0 {
		v.Domain = v.Identifiers[0]
	}
	if v.Type == "" {
		v.Type = "domain"
		if len(v.Identifiers) > 0 && net.ParseIP(v.Identifiers[0]) != nil {
			v.Type = "ip"
		}
	}
	if c.Source == "acme" {
		v.HTTPPort = c.HTTPPort
		if v.HTTPPort == 0 {
			v.HTTPPort = 80
		}
		if c.AutoRenew {
			t := RenewAt(c)
			v.RenewAt = &t
		}
	}
	return v
}

func splitIDs(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidationError is returned for bad input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ErrNotFound is returned when a certificate does not exist.
var ErrNotFound = errors.New("certificate not found")

// ErrInUse is returned when deleting a certificate that is referenced.
var ErrInUse = errors.New("certificate is in use")

// Service manages certificates.
type Service struct {
	DB            *gorm.DB
	DataDir       string
	ACMEDirectory string
	// HTTP01Addr overrides the challenge listener address (tests only;
	// normally ":<http_port>" of the certificate, default :80).
	HTTP01Addr string
	// ACMEHTTPClient overrides the HTTP client used to talk to the CA (tests).
	ACMEHTTPClient *http.Client
	// AllowNonPublicIPs permits private/loopback IP identifiers (tests).
	AllowNonPublicIPs bool
	// PanelServesHTTP01 reports whether the running panel listener on the
	// given port serves /.well-known/acme-challenge/ (when the panel itself
	// occupies the challenge port).
	PanelServesHTTP01 func(port int) bool
	OnChange          func()

	mu         sync.RWMutex
	cache      map[int64]*tls.Certificate
	acmeM      sync.Mutex
	challenges sync.Map // path -> key authorization
}

func (s *Service) changed() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	if s.OnChange != nil {
		s.OnChange()
	}
}

// List returns all certificates.
func (s *Service) List() ([]View, error) {
	var rows []db.Cert
	if err := s.DB.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	used := s.usedIDs()
	out := make([]View, 0, len(rows))
	for i := range rows {
		v := ToView(&rows[i])
		v.InUse = used[v.ID]
		out = append(out, v)
	}
	return out, nil
}

// Get returns one certificate.
func (s *Service) Get(id int64) (*View, error) {
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		return nil, ErrNotFound
	}
	v := ToView(&c)
	v.InUse = s.usedIDs()[id]
	return &v, nil
}

// checkUnused returns ErrInUse when the panel or an inbound uses the cert.
func (s *Service) checkUnused(id int64) error {
	st := db.LoadSettings(s.DB)
	if st.PanelCertID != nil && *st.PanelCertID == id {
		return fmt.Errorf("%w: used by the panel", ErrInUse)
	}
	var ibs []db.Inbound
	s.DB.Where("security = ?", "tls").Find(&ibs)
	for _, ib := range ibs {
		var t struct {
			CertID int64 `json:"cert_id"`
		}
		if json.Unmarshal([]byte(ib.TLS), &t) == nil && t.CertID == id {
			return fmt.Errorf("%w: used by inbound %q", ErrInUse, ib.Remark)
		}
	}
	return nil
}

// usedIDs returns certificate ids referenced by the panel or TLS inbounds.
func (s *Service) usedIDs() map[int64]bool {
	used := map[int64]bool{}
	st := db.LoadSettings(s.DB)
	if st.PanelCertID != nil {
		used[*st.PanelCertID] = true
	}
	var ibs []db.Inbound
	s.DB.Where("security = ?", "tls").Find(&ibs)
	for _, ib := range ibs {
		var t struct {
			CertID int64 `json:"cert_id"`
		}
		if json.Unmarshal([]byte(ib.TLS), &t) == nil && t.CertID > 0 {
			used[t.CertID] = true
		}
	}
	return used
}

// ParsePair validates a PEM certificate chain and key; returns leaf info.
func ParsePair(certPEM, keyPEM string) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, &ValidationError{Msg: "invalid certificate or key: " + err.Error()}
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, &ValidationError{Msg: "invalid certificate: " + err.Error()}
	}
	return leaf, nil
}

func issuerName(c *x509.Certificate) string {
	if len(c.Issuer.Organization) > 0 {
		return c.Issuer.Organization[0]
	}
	return c.Issuer.CommonName
}

// Upload stores a user-provided certificate.
func (s *Service) Upload(domain, certPEM, keyPEM string) (*View, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	leaf, err := ParsePair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if domain == "" {
		if len(leaf.DNSNames) > 0 {
			domain = leaf.DNSNames[0]
		} else if len(leaf.IPAddresses) > 0 {
			domain = leaf.IPAddresses[0].String()
		} else {
			domain = leaf.Subject.CommonName
		}
	}
	kind := "domain"
	if net.ParseIP(domain) != nil {
		kind = "ip"
	}
	nb := leaf.NotBefore.UTC()
	c := &db.Cert{Domain: domain, Kind: kind, Issuer: issuerName(leaf), NotBefore: &nb, NotAfter: leaf.NotAfter.UTC(), Source: "upload",
		CertPEM: certPEM, KeyPEM: keyPEM}
	if err := s.DB.Create(c).Error; err != nil {
		return nil, err
	}
	if err := s.writeFiles(c); err != nil {
		return nil, err
	}
	s.changed()
	v := ToView(c)
	return &v, nil
}

// Delete removes a certificate that is not used by the panel or inbounds.
func (s *Service) Delete(id int64) error {
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		return ErrNotFound
	}
	if err := s.checkUnused(id); err != nil {
		return err
	}
	if err := s.DB.Delete(&db.Cert{}, id).Error; err != nil {
		return err
	}
	cf, _ := Paths(s.DataDir, id)
	_ = os.RemoveAll(filepath.Dir(cf))
	s.changed()
	return nil
}

func (s *Service) writeFiles(c *db.Cert) error {
	cf, kf := Paths(s.DataDir, c.ID)
	if err := os.MkdirAll(filepath.Dir(cf), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(cf, []byte(c.CertPEM), 0o644); err != nil {
		return err
	}
	return os.WriteFile(kf, []byte(c.KeyPEM), 0o600)
}

// SyncFiles writes all certificates from the DB to disk (used at startup,
// so multi-node or restored databases get their files back).
func (s *Service) SyncFiles() error {
	var rows []db.Cert
	if err := s.DB.Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		if err := s.writeFiles(&rows[i]); err != nil {
			return err
		}
	}
	return nil
}

// Certificate returns a parsed certificate by id (cached).
func (s *Service) Certificate(id int64) (*tls.Certificate, error) {
	s.mu.RLock()
	if c, ok := s.cache[id]; ok {
		s.mu.RUnlock()
		return c, nil
	}
	s.mu.RUnlock()
	var row db.Cert
	if err := s.DB.First(&row, id).Error; err != nil {
		return nil, ErrNotFound
	}
	pair, err := tls.X509KeyPair([]byte(row.CertPEM), []byte(row.KeyPEM))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[int64]*tls.Certificate{}
	}
	s.cache[id] = &pair
	s.mu.Unlock()
	return &pair, nil
}
