package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// ProfileShortLived is the ACME profile Let's Encrypt requires for IP
// address certificates (about 160 hours of validity).
const ProfileShortLived = "shortlived"

// Renewal policy.
const (
	// ShortLivedRenewAge: short-lived certificates are renewed once they are
	// this old (about every 3 days), leaving ~3.6 days of margin for retries.
	ShortLivedRenewAge = 72 * time.Hour
	// shortLivedMax is the longest lifetime treated as "short-lived".
	shortLivedMax = 10 * 24 * time.Hour
	// RetryShortLived / RetryDefault: wait between failed renewal attempts.
	RetryShortLived = time.Hour
	RetryDefault    = 6 * time.Hour
)

// ACMERequest asks for a new ACME certificate.
type ACMERequest struct {
	// Type is "domain" (default) or "ip".
	Type   string `json:"type"`
	Domain string `json:"domain"`
	// IP is the IPv4 (or IPv6) address to certify. Empty = auto-detect.
	// Callers with a human in the loop should show DetectPublicIP's result
	// and send the confirmed address here.
	IP string `json:"ip"`
	// IPv6 optionally adds an IPv6 address to the same certificate
	// ("auto" = use the detected one).
	IPv6  string `json:"ipv6"`
	Email string `json:"email"`
	// HTTPPort is the local port of the HTTP-01 listener (default 80). The
	// CA always connects to port 80, so another value only makes sense when
	// port 80 is forwarded to it.
	HTTPPort int `json:"http_port"`
}

// Normalize validates the request and resolves IPs; it returns the ACME
// identifiers and profile to order.
func (s *Service) Normalize(ctx context.Context, r *ACMERequest) ([]Identifier, string, error) {
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	r.Email = strings.TrimSpace(r.Email)
	if r.Type == "" {
		r.Type = "domain"
		if r.Domain == "" && (r.IP != "" || r.IPv6 != "") {
			r.Type = "ip"
		}
	}
	if r.HTTPPort == 0 {
		r.HTTPPort = 80
	}
	if r.HTTPPort < 1 || r.HTTPPort > 65535 {
		return nil, "", &ValidationError{Msg: "http_port must be 1-65535"}
	}
	if r.Email != "" && (!strings.Contains(r.Email, "@") || strings.ContainsAny(r.Email, " ,;<>")) {
		return nil, "", &ValidationError{Msg: "email is not valid"}
	}
	switch r.Type {
	case "domain":
		d := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(r.Domain)), ".")
		if !validDomain(d) {
			return nil, "", &ValidationError{Msg: "a valid domain name is required"}
		}
		r.Domain = d
		return Identifiers(d), "", nil
	case "ip":
		var det *PublicIPResult
		detect := func() PublicIPResult {
			if det == nil {
				d := DetectPublicIP(ctx)
				det = &d
			}
			return *det
		}
		ip4 := strings.TrimSpace(r.IP)
		if ip4 == "" || ip4 == "auto" {
			d := detect()
			ip4 = d.IPv4
			if ip4 == "" && d.IPv6 != "" && r.IPv6 == "" {
				ip4 = d.IPv6
			}
			if ip4 == "" {
				return nil, "", &ValidationError{Msg: "could not detect the public IP; enter it manually"}
			}
		}
		ip6 := strings.TrimSpace(r.IPv6)
		if ip6 == "auto" {
			ip6 = detect().IPv6
			if ip6 == "" {
				return nil, "", &ValidationError{Msg: "no public IPv6 address detected; enter it manually or leave ipv6 empty"}
			}
		}
		var names []string
		for _, v := range []string{ip4, ip6} {
			if v == "" {
				continue
			}
			ip := net.ParseIP(strings.Trim(v, "[]"))
			if ip == nil {
				return nil, "", &ValidationError{Msg: fmt.Sprintf("%q is not an IP address", v)}
			}
			if !s.AllowNonPublicIPs && !IsPublicIP(ip) {
				return nil, "", &ValidationError{Msg: fmt.Sprintf("%s is not a public IP address; a public CA cannot validate it", ip)}
			}
			names = append(names, ip.String())
		}
		if ip6 != "" {
			if p := net.ParseIP(ip6); p == nil || p.To4() != nil {
				return nil, "", &ValidationError{Msg: "ipv6 must be an IPv6 address"}
			}
		}
		if len(names) == 2 && names[0] == names[1] {
			names = names[:1]
		}
		r.IP, r.IPv6 = names[0], ""
		if len(names) > 1 {
			r.IPv6 = names[1]
		}
		return Identifiers(names...), ProfileShortLived, nil
	}
	return nil, "", &ValidationError{Msg: `type must be "domain" or "ip"`}
}

func validDomain(d string) bool {
	if d == "" || len(d) > 253 || net.ParseIP(d) != nil || !strings.Contains(d, ".") {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || (ch == '*' && label == "*")) {
				return false
			}
		}
	}
	return !strings.HasPrefix(d, "*.") // wildcards need dns-01, which we do not do
}

// RenewAt returns when an ACME certificate is due for renewal: short-lived
// certificates (lifetime up to 10 days) at 3 days of age (or half their
// life if shorter), others when a third of their lifetime remains.
func RenewAt(c *db.Cert) time.Time {
	na := c.NotAfter.UTC()
	nb, life := lifetime(c)
	if life <= 0 {
		return na
	}
	if life <= shortLivedMax || c.Profile == ProfileShortLived {
		age := ShortLivedRenewAge
		if half := life / 2; half < age {
			age = half
		}
		return nb.Add(age)
	}
	return na.Add(-life / 3)
}

// lifetime returns the certificate's start and validity period (estimated
// from the profile when NotBefore is unknown).
func lifetime(c *db.Cert) (time.Time, time.Duration) {
	na := c.NotAfter.UTC()
	var nb time.Time
	switch {
	case c.NotBefore != nil && !c.NotBefore.IsZero():
		nb = c.NotBefore.UTC()
	case c.Profile == ProfileShortLived:
		nb = na.Add(-160 * time.Hour)
	default:
		nb = na.Add(-90 * 24 * time.Hour)
	}
	return nb, na.Sub(nb)
}

// renewDue reports whether c should be renewed now (honouring retry backoff).
func renewDue(c *db.Cert, now time.Time) bool {
	if c.Source != "acme" || !c.AutoRenew || now.Before(RenewAt(c)) {
		return false
	}
	if c.LastError != "" && c.LastAttemptAt != nil {
		wait := RetryDefault
		if _, life := lifetime(c); life <= shortLivedMax || c.Profile == ProfileShortLived {
			wait = RetryShortLived
		}
		if now.Sub(*c.LastAttemptAt) < wait {
			return false
		}
	}
	return true
}

func (s *Service) accountKey() (*ecdsa.PrivateKey, error) {
	if p := db.GetSetting(s.DB, db.KeyACMEAccount); p != "" {
		if blk, _ := pem.Decode([]byte(p)); blk != nil {
			if k, err := x509.ParseECPrivateKey(blk.Bytes); err == nil && k.Curve == elliptic.P256() {
				return k, nil
			}
		}
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, _ := x509.MarshalECPrivateKey(k)
	if err := db.SetSetting(s.DB, db.KeyACMEAccount, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))); err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Service) client() (*acmeClient, error) {
	key, err := s.accountKey()
	if err != nil {
		return nil, err
	}
	dir := s.ACMEDirectory
	if dir == "" {
		dir = "https://acme-v02.api.letsencrypt.org/directory"
	}
	return &acmeClient{DirectoryURL: dir, Key: key, HTTP: s.ACMEHTTPClient}, nil
}

// ServeHTTP01 answers ACME http-01 requests for pending orders. It returns
// false when the request is not a known challenge.
func (s *Service) ServeHTTP01(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		return false
	}
	v, ok := s.challenges.Load(r.URL.Path)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(v.(string)))
	return true
}

// startChallengeServer listens on the HTTP-01 port (all addresses, IPv4 and
// IPv6). When the port is taken by the panel itself, the panel answers.
func (s *Service) startChallengeServer(port int) (stop func(), err error) {
	addr := s.HTTP01Addr
	if addr == "" {
		addr = ":" + strconv.Itoa(port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) && s.PanelServesHTTP01 != nil && s.PanelServesHTTP01(port) {
			return func() {}, nil
		}
		if errors.Is(err, syscall.EADDRINUSE) {
			return nil, fmt.Errorf("HTTP-01 validation needs port %d, but it is in use (an inbound or another web server?). Free it, or forward port 80 to another port and set http_port", port)
		}
		return nil, fmt.Errorf("HTTP-01 listener on %s: %w", addr, err)
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ServeHTTP01(w, r) {
			http.NotFound(w, r)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	return func() { _ = srv.Close() }, nil
}

// obtain runs a full ACME order and returns the PEM chain and key.
func (s *Service) obtain(ctx context.Context, ids []Identifier, profile, email string, httpPort int) (certPEM, keyPEM string, err error) {
	s.acmeM.Lock()
	defer s.acmeM.Unlock()
	cl, err := s.client()
	if err != nil {
		return "", "", err
	}
	if err := cl.Register(ctx, email); err != nil {
		return "", "", err
	}
	order, err := cl.NewOrder(ctx, ids, profile)
	if err != nil {
		return "", "", err
	}
	if order.Status != "ready" && order.Status != "valid" {
		stop, err := s.startChallengeServer(httpPort)
		if err != nil {
			return "", "", err
		}
		defer stop()
		for _, zurl := range order.Authorizations {
			z, err := cl.Authorization(ctx, zurl)
			if err != nil {
				return "", "", fmt.Errorf("acme authorization: %w", err)
			}
			if z.Status == "valid" {
				continue
			}
			var ch *acmeChallenge
			for i := range z.Challenges {
				if z.Challenges[i].Type == "http-01" {
					ch = &z.Challenges[i]
				}
			}
			if ch == nil {
				return "", "", fmt.Errorf("acme: no http-01 challenge offered for %s", z.Identifier.Value)
			}
			path := "/.well-known/acme-challenge/" + ch.Token
			s.challenges.Store(path, cl.KeyAuthorization(ch.Token))
			defer s.challenges.Delete(path)
			if err := cl.Accept(ctx, ch); err != nil {
				return "", "", err
			}
			if err := cl.WaitAuthorization(ctx, zurl); err != nil {
				return "", "", err
			}
		}
		if order, err = cl.WaitOrder(ctx, order.URL, "ready", "valid"); err != nil {
			return "", "", err
		}
	}
	ckey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, buildCSR(ids), ckey)
	if err != nil {
		return "", "", err
	}
	chain, err := cl.Finalize(ctx, order, csr)
	if err != nil {
		return "", "", err
	}
	kder, _ := x509.MarshalECPrivateKey(ckey)
	return chain, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})), nil
}

// buildCSR puts dns identifiers in DNSNames (first as CN) and ip identifiers
// in IPAddresses (no CN: CAs drop it for IP-only certificates).
func buildCSR(ids []Identifier) *x509.CertificateRequest {
	tpl := &x509.CertificateRequest{}
	for _, id := range ids {
		switch id.Type {
		case "dns":
			if tpl.Subject.CommonName == "" {
				tpl.Subject = pkix.Name{CommonName: id.Value}
			}
			tpl.DNSNames = append(tpl.DNSNames, id.Value)
		case "ip":
			tpl.IPAddresses = append(tpl.IPAddresses, net.ParseIP(id.Value))
		}
	}
	return tpl
}

func idValues(ids []Identifier) string {
	v := make([]string, len(ids))
	for i, id := range ids {
		v[i] = id.Value
	}
	return strings.Join(v, ",")
}

// IssueACME obtains and stores a new ACME certificate (auto-renewing).
func (s *Service) IssueACME(ctx context.Context, req ACMERequest) (*View, error) {
	ids, profile, err := s.Normalize(ctx, &req)
	if err != nil {
		return nil, err
	}
	cp, kp, err := s.obtain(ctx, ids, profile, req.Email, req.HTTPPort)
	if err != nil {
		return nil, err
	}
	leaf, err := ParsePair(cp, kp)
	if err != nil {
		return nil, err
	}
	nb := leaf.NotBefore.UTC()
	c := &db.Cert{Domain: idValues(ids), Kind: req.Type, Profile: profile, HTTPPort: req.HTTPPort,
		Issuer: issuerName(leaf), NotBefore: &nb, NotAfter: leaf.NotAfter.UTC(), AutoRenew: true,
		Source: "acme", Email: req.Email, CertPEM: cp, KeyPEM: kp}
	if err := s.DB.Create(c).Error; err != nil {
		return nil, err
	}
	if err := s.writeFiles(c); err != nil {
		return nil, err
	}
	s.changed()
	slog.Info("certificate issued", "id", c.ID, "identifiers", c.Domain, "profile", profile, "not_after", c.NotAfter)
	v := ToView(c)
	return &v, nil
}

// Renew renews one ACME certificate now, in place (same id), and hot-reloads
// the panel listener and inbounds that use it.
func (s *Service) Renew(ctx context.Context, id int64) (*View, error) {
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		return nil, ErrNotFound
	}
	if c.Source != "acme" {
		return nil, &ValidationError{Msg: "only ACME certificates can be renewed"}
	}
	if err := s.renewOne(ctx, &c); err != nil {
		return nil, err
	}
	s.changed()
	v := ToView(&c)
	return &v, nil
}

func (s *Service) renewOne(ctx context.Context, c *db.Cert) error {
	ids := Identifiers(splitIDs(c.Domain)...)
	port := c.HTTPPort
	if port == 0 {
		port = 80
	}
	now := time.Now().UTC()
	c.LastAttemptAt = &now
	cp, kp, err := s.obtain(ctx, ids, c.Profile, c.Email, port)
	if err == nil {
		var leaf *x509.Certificate
		if leaf, err = ParsePair(cp, kp); err == nil {
			nb := leaf.NotBefore.UTC()
			c.CertPEM, c.KeyPEM, c.NotBefore, c.NotAfter, c.Issuer = cp, kp, &nb, leaf.NotAfter.UTC(), issuerName(leaf)
			c.LastError = ""
		}
	}
	if err != nil {
		c.LastError = err.Error()
		s.DB.Model(&db.Cert{}).Where("id = ?", c.ID).Updates(map[string]any{"last_error": c.LastError, "last_attempt_at": now})
		return err
	}
	if err := s.DB.Save(c).Error; err != nil {
		return err
	}
	if err := s.writeFiles(c); err != nil {
		return err
	}
	slog.Info("certificate renewed", "id", c.ID, "identifiers", c.Domain, "not_after", c.NotAfter)
	return nil
}

// RenewDue renews every auto-renewing ACME certificate whose renewal time
// has come (see RenewAt), with retry backoff after failures. The job runner
// calls it hourly.
func (s *Service) RenewDue(ctx context.Context) {
	var rows []db.Cert
	s.DB.Where("source = ? AND auto_renew = ?", "acme", true).Find(&rows)
	now := time.Now().UTC()
	renewed := false
	for i := range rows {
		c := &rows[i]
		if !renewDue(c, now) {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := s.renewOne(rctx, c)
		cancel()
		if err != nil {
			slog.Warn("certificate renewal failed", "id", c.ID, "identifiers", c.Domain, "not_after", c.NotAfter, "err", err)
			continue
		}
		renewed = true
	}
	if renewed {
		s.changed()
	}
}

// SetAutoRenew turns automatic renewal of an ACME certificate on or off.
func (s *Service) SetAutoRenew(id int64, on bool) (*View, error) {
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		return nil, ErrNotFound
	}
	if c.Source != "acme" && on {
		return nil, &ValidationError{Msg: "only ACME certificates can auto-renew"}
	}
	if err := s.DB.Model(&db.Cert{}).Where("id = ?", id).Update("auto_renew", on).Error; err != nil {
		return nil, err
	}
	c.AutoRenew = on
	v := ToView(&c)
	return &v, nil
}

// Revoke revokes an unused ACME certificate at the CA and deletes it.
func (s *Service) Revoke(ctx context.Context, id int64) error {
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		return ErrNotFound
	}
	if c.Source != "acme" {
		return &ValidationError{Msg: "only ACME certificates can be revoked; delete it instead"}
	}
	if err := s.checkUnused(id); err != nil {
		return err
	}
	blk, _ := pem.Decode([]byte(c.CertPEM))
	if blk == nil {
		return errors.New("stored certificate is not valid PEM")
	}
	cl, err := s.client()
	if err != nil {
		return err
	}
	if err := cl.Register(ctx, c.Email); err != nil {
		return err
	}
	if err := cl.Revoke(ctx, blk.Bytes); err != nil {
		var p *acmeProblem
		if !errors.As(err, &p) || !strings.HasSuffix(p.Type, ":alreadyRevoked") {
			return fmt.Errorf("acme revoke: %w", err)
		}
	}
	return s.Delete(id)
}
