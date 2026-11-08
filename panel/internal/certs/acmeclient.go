package certs

// A small RFC 8555 ACME client. It exists because certificates for IP
// addresses need two things the vendored x/crypto/acme does not offer at our
// Go version: certificate profiles (newOrder "profile" field, advertised in
// the directory "meta.profiles") and fine control over identifier types.
// Only what Veilon needs is implemented: ES256 account keys, newAccount,
// newOrder (dns and ip identifiers, optional profile), http-01 challenges,
// finalize, certificate download and revocation.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Identifier is an ACME identifier ("dns" or "ip").
type Identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Identifiers builds ACME identifiers from host names and IP addresses.
// IP values are normalised to their canonical text form (RFC 8738).
func Identifiers(names ...string) []Identifier {
	out := make([]Identifier, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if ip := net.ParseIP(strings.Trim(n, "[]")); ip != nil {
			out = append(out, Identifier{Type: "ip", Value: ip.String()})
		} else {
			out = append(out, Identifier{Type: "dns", Value: strings.ToLower(n)})
		}
	}
	return out
}

// acmeDirectory is the subset of the ACME directory object we use.
type acmeDirectory struct {
	NewNonce   string `json:"newNonce"`
	NewAccount string `json:"newAccount"`
	NewOrder   string `json:"newOrder"`
	RevokeCert string `json:"revokeCert"`
	Meta       struct {
		TermsOfService string            `json:"termsOfService"`
		Profiles       map[string]string `json:"profiles"`
	} `json:"meta"`
}

// acmeProblem is an RFC 7807 problem document returned by the CA.
type acmeProblem struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func (p *acmeProblem) Error() string {
	t := strings.TrimPrefix(p.Type, "urn:ietf:params:acme:error:")
	if p.Detail == "" {
		return "acme: " + t
	}
	return fmt.Sprintf("acme: %s: %s", t, p.Detail)
}

type acmeChallenge struct {
	Type   string       `json:"type"`
	URL    string       `json:"url"`
	Token  string       `json:"token"`
	Status string       `json:"status"`
	Error  *acmeProblem `json:"error"`
}

type acmeAuthz struct {
	Identifier Identifier      `json:"identifier"`
	Status     string          `json:"status"`
	Challenges []acmeChallenge `json:"challenges"`
}

// acmeOrder is an ACME order; URL is taken from the Location header.
type acmeOrder struct {
	URL            string       `json:"-"`
	Status         string       `json:"status"`
	Identifiers    []Identifier `json:"identifiers"`
	Profile        string       `json:"profile,omitempty"`
	Authorizations []string     `json:"authorizations"`
	Finalize       string       `json:"finalize"`
	Certificate    string       `json:"certificate"`
	Error          *acmeProblem `json:"error"`
}

// newOrderRequest is the JSON body of a newOrder request.
type newOrderRequest struct {
	Identifiers []Identifier `json:"identifiers"`
	Profile     string       `json:"profile,omitempty"`
}

// ErrProfileUnsupported is returned when the CA does not advertise a profile.
var ErrProfileUnsupported = errors.New("acme: the CA does not offer the requested certificate profile")

// acmeClient talks to one ACME directory with one ES256 account key.
type acmeClient struct {
	DirectoryURL string
	Key          *ecdsa.PrivateKey
	HTTP         *http.Client
	// PollInterval overrides the default wait between status polls.
	PollInterval time.Duration

	mu     sync.Mutex
	dir    *acmeDirectory
	nonces []string
	kid    string
}

func (c *acmeClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Directory fetches (once) and returns the CA directory.
func (c *acmeClient) Directory(ctx context.Context) (*acmeDirectory, error) {
	c.mu.Lock()
	d := c.dir
	c.mu.Unlock()
	if d != nil {
		return d, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.DirectoryURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("acme directory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("acme directory: HTTP %d", resp.StatusCode)
	}
	d = &acmeDirectory{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(d); err != nil {
		return nil, fmt.Errorf("acme directory: %w", err)
	}
	if d.NewNonce == "" || d.NewAccount == "" || d.NewOrder == "" {
		return nil, errors.New("acme directory: missing endpoints")
	}
	c.mu.Lock()
	c.dir = d
	c.mu.Unlock()
	return d, nil
}

// SupportsProfile reports whether the CA advertises profile name.
func (d *acmeDirectory) SupportsProfile(name string) bool {
	_, ok := d.Meta.Profiles[name]
	return ok
}

func (c *acmeClient) nonce(ctx context.Context) (string, error) {
	c.mu.Lock()
	if n := len(c.nonces); n > 0 {
		v := c.nonces[n-1]
		c.nonces = c.nonces[:n-1]
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	d, err := c.Directory(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, d.NewNonce, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("acme nonce: %w", err)
	}
	resp.Body.Close()
	n := resp.Header.Get("Replay-Nonce")
	if n == "" {
		return "", errors.New("acme nonce: no Replay-Nonce header")
	}
	return n, nil
}

func (c *acmeClient) saveNonce(h http.Header) {
	if n := h.Get("Replay-Nonce"); n != "" {
		c.mu.Lock()
		if len(c.nonces) < 16 {
			c.nonces = append(c.nonces, n)
		}
		c.mu.Unlock()
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// jwk returns the public JWK of the account key with members in
// lexicographic order (as required for the RFC 7638 thumbprint).
func jwk(k *ecdsa.PublicKey) string {
	size := (k.Curve.Params().BitSize + 7) / 8
	x := k.X.FillBytes(make([]byte, size))
	y := k.Y.FillBytes(make([]byte, size))
	return fmt.Sprintf(`{"crv":"%s","kty":"EC","x":"%s","y":"%s"}`, k.Curve.Params().Name, b64(x), b64(y))
}

// Thumbprint is the base64url SHA-256 JWK thumbprint of the account key.
func (c *acmeClient) Thumbprint() string {
	s := sha256.Sum256([]byte(jwk(&c.Key.PublicKey)))
	return b64(s[:])
}

// KeyAuthorization returns the http-01 key authorization for token.
func (c *acmeClient) KeyAuthorization(token string) string {
	return token + "." + c.Thumbprint()
}

// signJWS builds a flattened JWS. payload nil means POST-as-GET (empty payload).
func (c *acmeClient) signJWS(url, nonce string, payload []byte, useJWK bool) ([]byte, error) {
	if c.Key.Curve.Params().BitSize != 256 {
		return nil, errors.New("acme: account key must be P-256")
	}
	prot := map[string]any{"alg": "ES256", "nonce": nonce, "url": url}
	if useJWK || c.kid == "" {
		prot["jwk"] = json.RawMessage(jwk(&c.Key.PublicKey))
	} else {
		prot["kid"] = c.kid
	}
	pb, err := json.Marshal(prot)
	if err != nil {
		return nil, err
	}
	p64 := b64(pb)
	pl64 := ""
	if payload != nil {
		pl64 = b64(payload)
	}
	h := sha256.Sum256([]byte(p64 + "." + pl64))
	r, s, err := ecdsa.Sign(rand.Reader, c.Key, h[:])
	if err != nil {
		return nil, err
	}
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return json.Marshal(map[string]string{"protected": p64, "payload": pl64, "signature": b64(sig)})
}

// post sends a signed request. body nil = POST-as-GET. It retries once on
// badNonce and returns the response (body read) or a problem error.
func (c *acmeClient) post(ctx context.Context, url string, body any, useJWK bool, accept string) (*http.Response, []byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, nil, err
		}
	}
	for attempt := 0; ; attempt++ {
		n, err := c.nonce(ctx)
		if err != nil {
			return nil, nil, err
		}
		jws, err := c.signJWS(url, n, payload, useJWK)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jws))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/jose+json")
		req.Header.Set("User-Agent", "veilon-acme/1")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		c.saveNonce(resp.Header)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode < 400 {
			return resp, data, nil
		}
		p := &acmeProblem{Status: resp.StatusCode}
		if json.Unmarshal(data, p) != nil || p.Type == "" {
			p.Detail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
		}
		if strings.HasSuffix(p.Type, ":badNonce") && attempt < 3 {
			continue
		}
		return resp, data, p
	}
}

// Register creates (or finds) the account and records its URL (kid).
func (c *acmeClient) Register(ctx context.Context, email string) error {
	d, err := c.Directory(ctx)
	if err != nil {
		return err
	}
	req := map[string]any{"termsOfServiceAgreed": true}
	if email != "" {
		req["contact"] = []string{"mailto:" + email}
	}
	resp, _, err := c.post(ctx, d.NewAccount, req, true, "")
	if err != nil {
		return fmt.Errorf("acme register: %w", err)
	}
	kid := resp.Header.Get("Location")
	if kid == "" {
		return errors.New("acme register: no account URL")
	}
	c.mu.Lock()
	c.kid = kid
	c.mu.Unlock()
	return nil
}

// buildNewOrder validates identifiers and the profile against the directory.
func buildNewOrder(d *acmeDirectory, ids []Identifier, profile string) (*newOrderRequest, error) {
	if len(ids) == 0 {
		return nil, errors.New("acme: no identifiers")
	}
	for _, id := range ids {
		if id.Type != "dns" && id.Type != "ip" {
			return nil, fmt.Errorf("acme: unsupported identifier type %q", id.Type)
		}
	}
	if profile != "" && !d.SupportsProfile(profile) {
		names := make([]string, 0, len(d.Meta.Profiles))
		for n := range d.Meta.Profiles {
			names = append(names, n)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("%w %q (the CA advertises no profiles)", ErrProfileUnsupported, profile)
		}
		return nil, fmt.Errorf("%w %q (offered: %s)", ErrProfileUnsupported, profile, strings.Join(names, ", "))
	}
	return &newOrderRequest{Identifiers: ids, Profile: profile}, nil
}

// NewOrder submits a newOrder request.
func (c *acmeClient) NewOrder(ctx context.Context, ids []Identifier, profile string) (*acmeOrder, error) {
	d, err := c.Directory(ctx)
	if err != nil {
		return nil, err
	}
	req, err := buildNewOrder(d, ids, profile)
	if err != nil {
		return nil, err
	}
	resp, data, err := c.post(ctx, d.NewOrder, req, false, "")
	if err != nil {
		return nil, fmt.Errorf("acme new order: %w", err)
	}
	o := &acmeOrder{}
	if err := json.Unmarshal(data, o); err != nil {
		return nil, fmt.Errorf("acme new order: %w", err)
	}
	o.URL = resp.Header.Get("Location")
	return o, nil
}

func (c *acmeClient) getJSON(ctx context.Context, url string, v any) (*http.Response, error) {
	resp, data, err := c.post(ctx, url, nil, false, "")
	if err != nil {
		return resp, err
	}
	return resp, json.Unmarshal(data, v)
}

// Authorization fetches an authorization object.
func (c *acmeClient) Authorization(ctx context.Context, url string) (*acmeAuthz, error) {
	z := &acmeAuthz{}
	_, err := c.getJSON(ctx, url, z)
	return z, err
}

// Accept tells the CA a challenge is ready to be validated.
func (c *acmeClient) Accept(ctx context.Context, ch *acmeChallenge) error {
	_, _, err := c.post(ctx, ch.URL, struct{}{}, false, "")
	if err != nil {
		return fmt.Errorf("acme accept challenge: %w", err)
	}
	return nil
}

func (c *acmeClient) wait(ctx context.Context, resp *http.Response) error {
	d := c.PollInterval
	if d == 0 {
		d = 2 * time.Second
		if resp != nil {
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 30 {
				d = time.Duration(s) * time.Second
			}
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// WaitAuthorization polls until the authorization is valid or failed.
func (c *acmeClient) WaitAuthorization(ctx context.Context, url string) error {
	for {
		z := &acmeAuthz{}
		resp, err := c.getJSON(ctx, url, z)
		if err != nil {
			return err
		}
		switch z.Status {
		case "valid":
			return nil
		case "invalid", "deactivated", "expired", "revoked":
			for _, ch := range z.Challenges {
				if ch.Error != nil {
					return fmt.Errorf("authorization for %s %s: %w", z.Identifier.Type, z.Identifier.Value, ch.Error)
				}
			}
			return fmt.Errorf("authorization for %s is %s", z.Identifier.Value, z.Status)
		}
		if err := c.wait(ctx, resp); err != nil {
			return err
		}
	}
}

// WaitOrder polls until the order leaves the pending/processing states.
func (c *acmeClient) WaitOrder(ctx context.Context, url string, want ...string) (*acmeOrder, error) {
	for {
		o := &acmeOrder{}
		resp, err := c.getJSON(ctx, url, o)
		if err != nil {
			return nil, err
		}
		o.URL = url
		for _, w := range want {
			if o.Status == w {
				return o, nil
			}
		}
		if o.Status == "invalid" {
			if o.Error != nil {
				return nil, fmt.Errorf("order failed: %w", o.Error)
			}
			return nil, errors.New("order failed")
		}
		if err := c.wait(ctx, resp); err != nil {
			return nil, err
		}
	}
}

// Finalize submits the CSR and returns the issued PEM chain.
func (c *acmeClient) Finalize(ctx context.Context, o *acmeOrder, csrDER []byte) (string, error) {
	resp, data, err := c.post(ctx, o.Finalize, map[string]string{"csr": b64(csrDER)}, false, "")
	if err != nil {
		return "", fmt.Errorf("acme finalize: %w", err)
	}
	cur := &acmeOrder{}
	_ = json.Unmarshal(data, cur)
	if cur.Status != "valid" || cur.Certificate == "" {
		_ = c.wait(ctx, resp)
		if cur, err = c.WaitOrder(ctx, o.URL, "valid"); err != nil {
			return "", err
		}
	}
	_, pemData, err := c.post(ctx, cur.Certificate, nil, false, "application/pem-certificate-chain")
	if err != nil {
		return "", fmt.Errorf("acme download certificate: %w", err)
	}
	if !strings.Contains(string(pemData), "BEGIN CERTIFICATE") {
		return "", errors.New("acme: CA returned no PEM certificate")
	}
	return string(pemData), nil
}

// Revoke revokes a certificate (DER) signed by this account.
func (c *acmeClient) Revoke(ctx context.Context, certDER []byte) error {
	d, err := c.Directory(ctx)
	if err != nil {
		return err
	}
	if d.RevokeCert == "" {
		return errors.New("acme: CA has no revokeCert endpoint")
	}
	_, _, err = c.post(ctx, d.RevokeCert, map[string]any{"certificate": b64(certDER)}, false, "")
	return err
}
