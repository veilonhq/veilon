package veiloncore

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Subscription TLS (1.0.8, audit H1).
//
// Until 1.0.7 a subscription whose certificate did not verify was silently
// fetched again with verification off, for any host. An on-path box with its
// own certificate then received the sub token and the device headers and
// could inject its own servers. Now:
//
//   - a host name (domain) never gets a fallback: the fetch fails with
//     err.subCertInvalid;
//   - a literal IP host (panel sub servers on https://<ip>:<port>/ with a
//     self-signed certificate) is fetched only when the user has approved
//     that server's certificate in the app: the leaf certificate's SHA-256
//     is pinned per ip:port (SetSubPins). Without an approved pin the core
//     only performs a bare TLS handshake to read the certificate (no HTTP
//     request, so no sub path, token or device headers are sent) and returns
//     err.subCertUntrusted:<sha256> for the app to show.

// SubCertError is returned when a literal-IP subscription server presents a
// certificate that neither verifies nor matches an approved pin.
type SubCertError struct {
	Fingerprint string // hex SHA-256 of the leaf certificate (DER)
}

func (e *SubCertError) Error() string { return "err.subCertUntrusted:" + e.Fingerprint }

var (
	subPinsMu sync.RWMutex
	subPins   = map[string]string{} // "ip:port" -> lowercase hex sha256
)

// setSubPins replaces the approved certificate pins: a JSON object
// {"ip:port": "<hex sha256>"}. Keys that are not a literal IP with a port are
// ignored.
func setSubPins(pinsJSON string) error {
	m := map[string]string{}
	if strings.TrimSpace(pinsJSON) != "" {
		if err := json.Unmarshal([]byte(pinsJSON), &m); err != nil {
			return err
		}
	}
	clean := map[string]string{}
	for k, v := range m {
		h, p, err := net.SplitHostPort(k)
		ip := net.ParseIP(h)
		fp := normFingerprint(v)
		if err != nil || ip == nil || p == "" || len(fp) != 64 {
			continue
		}
		clean[net.JoinHostPort(ip.String(), p)] = fp
	}
	subPinsMu.Lock()
	subPins = clean
	subPinsMu.Unlock()
	return nil
}

func normFingerprint(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ":", "")
	if _, err := hex.DecodeString(s); err != nil {
		return ""
	}
	return s
}

// subHostKey returns "ip:port" for an https URL whose host is a literal IP.
func subHostKey(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(ip.String(), port), true
}

func subPinFor(rawURL string) string {
	key, ok := subHostKey(rawURL)
	if !ok {
		return ""
	}
	subPinsMu.RLock()
	defer subPinsMu.RUnlock()
	return subPins[key]
}

func leafFingerprint(raw [][]byte) string {
	if len(raw) == 0 {
		return ""
	}
	sum := sha256.Sum256(raw[0])
	return hex.EncodeToString(sum[:])
}

// pinnedSubClient is the normal subscription client except that the chain
// is accepted only when the leaf certificate matches pin exactly. A
// different certificate fails with *SubCertError carrying the new
// fingerprint (the app asks the user again).
func pinnedSubClient(proxyAddr string, timeout time.Duration, pin string) *http.Client {
	c := httpClientTLS(proxyAddr, timeout, true)
	if tr, ok := c.Transport.(*http.Transport); ok {
		tr.TLSClientConfig.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			fp := leafFingerprint(raw)
			if fp == "" || fp != pin {
				return &SubCertError{Fingerprint: fp}
			}
			return nil
		}
	}
	return c
}

// subCertFailure turns the certificate error of a verified fetch into the
// error the app gets. No HTTP request is ever sent without verification.
func subCertFailure(ctx context.Context, rawURL, proxyAddr string, timeout time.Duration, cause error) error {
	key, isIP := subHostKey(rawURL)
	if !isIP {
		return fmt.Errorf("err.subCertInvalid: %s", certErrorKind(cause))
	}
	fp, err := probeLeafFingerprint(ctx, key, proxyAddr, timeout)
	if err != nil || fp == "" {
		return fmt.Errorf("err.subCertInvalid: %s", certErrorKind(cause))
	}
	return &SubCertError{Fingerprint: fp}
}

// cleanSubCertError strips the request URL from a pin failure (the app shows
// the error text; the sub URL must not appear in it).
func cleanSubCertError(err error) error {
	var sce *SubCertError
	if errors.As(err, &sce) {
		return sce
	}
	return err
}

// probeLeafFingerprint opens one TLS connection to key (over the same proxy
// path as the fetch), reads the leaf certificate and closes it without
// sending any application data.
func probeLeafFingerprint(ctx context.Context, key, proxyAddr string, timeout time.Duration) (string, error) {
	c := httpClientTLS(proxyAddr, timeout, true)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		return "", errors.New("no dialer")
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := tr.DialContext(dctx, "tcp", key)
	if err != nil {
		return "", err
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(timeout))
	var fp string
	tc := tls.Client(raw, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2", "http/1.1"},
		VerifyPeerCertificate: func(rc [][]byte, _ [][]*x509.Certificate) error {
			fp = leafFingerprint(rc)
			return nil
		},
	})
	if err := tc.HandshakeContext(dctx); err != nil {
		return "", err
	}
	_ = tc.Close()
	return fp, nil
}
