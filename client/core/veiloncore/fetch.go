package veiloncore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/proxy"
)

// Device is what the client reports to the panel (device limits).
type Device struct {
	HWID      string `json:"hwid"`
	OS        string `json:"os"`
	OSVersion string `json:"osVersion"`
	Model     string `json:"model"`
	AppVer    string `json:"appVersion"`
}

// UserInfo is the parsed subscription-userinfo header.
type UserInfo struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
	Present  bool  `json:"present"`
}

// SubResult is returned by FetchSubscription.
type SubResult struct {
	Profiles       []*Profile `json:"profiles"`
	Errors         []string   `json:"errors,omitempty"`
	UserInfo       UserInfo   `json:"userInfo"`
	Title          string     `json:"title,omitempty"`
	UpdateHours    int        `json:"updateHours,omitempty"`
	Refused        string     `json:"refused,omitempty"`
	SupportURL     string     `json:"supportUrl,omitempty"`
	WebPageURL     string     `json:"webPageUrl,omitempty"`
	Announce       string     `json:"announce,omitempty"`
	Description    string     `json:"description,omitempty"`
	FetchedAt      int64      `json:"fetchedAt"`
	HTTPStatusCode int        `json:"status"`
}

// ParseUserInfo parses "upload=1; download=2; total=3; expire=4".
func ParseUserInfo(h string) UserInfo {
	var u UserInfo
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "upload":
			u.Upload, u.Present = int64(n), true
		case "download":
			u.Download, u.Present = int64(n), true
		case "total":
			u.Total, u.Present = int64(n), true
		case "expire":
			u.Expire, u.Present = int64(n), true
		}
	}
	return u
}

// decodeHeaderText decodes "base64:..." header values (profile-title).
func decodeHeaderText(v string) string {
	v = strings.TrimSpace(v)
	if s, ok := strings.CutPrefix(v, "base64:"); ok {
		if b, err := decodeB64(s); err == nil {
			return string(b)
		}
	}
	return v
}

// subMetaKeys are the response headers that a subscription body may also
// carry as directive lines ("#profile-description: …" or
// "#profile-description:…", a common subscription convention).
var subMetaKeys = map[string]bool{
	"profile-title":           true,
	"profile-description":     true,
	"announce":                true,
	"support-url":             true,
	"profile-web-page-url":    true,
	"profile-update-interval": true,
	"subscription-userinfo":   true,
}

// splitBodyDirectives (1.0.12) takes the "#<key>: value" directive lines of
// subMetaKeys out of a subscription body. Directives may sit in the plain
// body (also above a base64 blob) or inside the base64-decoded body. The
// first value per key wins. When none is found the body is returned
// unchanged, so bodies without directives parse exactly as before; JSON
// bodies are never touched. Other '#' lines are left to ParseContent.
func splitBodyDirectives(body string) (string, map[string]string) {
	t := strings.TrimSpace(strings.TrimPrefix(body, "\ufeff"))
	if t == "" || strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return body, nil
	}
	dir := map[string]string{}
	rest, found := takeDirectives(t, dir)
	if r := strings.TrimSpace(rest); r != "" && !strings.Contains(r, "://") {
		if raw, err := decodeB64(r); err == nil && utf8.Valid(raw) {
			if inner, ok := takeDirectives(strings.TrimPrefix(string(raw), "\ufeff"), dir); ok {
				rest, found = inner, true
			}
		}
	}
	if !found {
		return body, nil
	}
	return rest, dir
}

// takeDirectives removes directive lines from text into dir (keeping values
// already in dir) and reports whether it found any.
func takeDirectives(text string, dir map[string]string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := lines[:0]
	found := false
	for _, ln := range lines {
		s := strings.TrimSpace(ln)
		if strings.HasPrefix(s, "#") {
			if k, v, ok := strings.Cut(s[1:], ":"); ok {
				if key := strings.ToLower(strings.TrimSpace(k)); subMetaKeys[key] {
					found = true
					if _, seen := dir[key]; !seen {
						dir[key] = strings.TrimSpace(v)
					}
					continue
				}
			}
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n"), found
}

// MaxDescriptionRunes caps the subscription description (untrusted text).
const MaxDescriptionRunes = 1000

// cleanDescription (1.0.12) turns the panel's profile-description into safe
// plain text: valid UTF-8, line breaks normalised, control characters and
// bidi embedding/override/isolate controls removed, tabs → spaces, ZWNJ →
// space, at most one empty line in a row, trimmed, capped at
// MaxDescriptionRunes (with "…").
func cleanDescription(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t' || r == '\u200c' || r == '\u00a0':
			b.WriteByte(' ')
		case unicode.IsControl(r), r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069',
			r == '\u200e', r == '\u200f', r == '\u061c', r == '\ufeff', r == '\ufffd':
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	lines := strings.Split(b.String(), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " ")
		if strings.TrimSpace(ln) == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, ln)
	}
	res := strings.TrimSpace(strings.Join(out, "\n"))
	if utf8.RuneCountInString(res) > MaxDescriptionRunes {
		res = strings.TrimSpace(string([]rune(res)[:MaxDescriptionRunes-1])) + "…"
	}
	return res
}

func userAgent(d Device) string {
	v := d.AppVer
	if v == "" {
		v = Version
	}
	return fmt.Sprintf("Veilon/%s (Android %s; %s)", v, d.OSVersion, d.Model)
}

func httpClient(proxyAddr string, timeout time.Duration) *http.Client {
	return httpClientTLS(proxyAddr, timeout, false)
}

// httpClientTLS is httpClient with optional certificate verification off.
// Used only as a fallback for subscription URLs whose certificate cannot be
// verified (self-signed panels, a domain certificate served on a bare IP
// such as https://<ip>:2096/... from common panel sub servers).
func httpClientTLS(proxyAddr string, timeout time.Duration, insecure bool) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecure},
		TLSHandshakeTimeout: timeout,
		DialContext:         (&net.Dialer{Timeout: timeout, Resolver: directResolver()}).DialContext,
	}
	var auth *proxy.Auth
	if proxyAddr == "auto" {
		proxyAddr = ""
		if lp := internalProxy(); lp != nil {
			proxyAddr = fmt.Sprintf("127.0.0.1:%d", lp.Port)
			auth = &proxy.Auth{User: lp.User, Password: lp.Pass}
		}
	}
	if proxyAddr != "" {
		if d, err := proxy.SOCKS5("tcp", proxyAddr, auth, proxy.Direct); err == nil {
			if cd, ok := d.(proxy.ContextDialer); ok {
				tr.DialContext = cd.DialContext
			}
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout * 2}
}

func setDeviceHeaders(req *http.Request, d Device) {
	req.Header.Set("User-Agent", userAgent(d))
	if d.HWID != "" {
		req.Header.Set("X-Hwid", d.HWID)
	}
	req.Header.Set("X-Device-Os", firstNonEmpty(d.OS, "Android"))
	if d.OSVersion != "" {
		req.Header.Set("X-Ver-Os", d.OSVersion)
	}
	if d.Model != "" {
		req.Header.Set("X-Device-Model", d.Model)
	}
	req.Header.Set("Accept", "*/*")
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func withFormat(raw, format string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("format", format)
	u.RawQuery = q.Encode()
	return u.String()
}

func get(ctx context.Context, c *http.Client, rawURL string, d Device) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	setDeviceHeaders(req, d)
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp, body, err
}

// FetchSubscription downloads and parses a subscription.
//
// The Veilon panel serves every protocol except AmneziaWG and MASQUE as
// share links in its default format; AmneziaWG is only in the Clash format
// and MASQUE only in the Xray JSON format, so those two are fetched as well
// (best effort; other panels just return their default body again, which is
// ignored).
func fetchSubscription(rawURL string, d Device, proxyAddr string, timeout time.Duration) *SubResult {
	if proxyAddr == "auto" && internalProxy() != nil {
		res := fetchSubscriptionOnce(rawURL, d, "auto", timeout)
		if res.HTTPStatusCode != 0 {
			return res
		}
		// The tunnel could not reach it; try the direct network.
		direct := fetchSubscriptionOnce(rawURL, d, "", timeout)
		if direct.HTTPStatusCode != 0 {
			return direct
		}
		return res
	}
	if proxyAddr == "auto" {
		proxyAddr = ""
	}
	return fetchSubscriptionOnce(rawURL, d, proxyAddr, timeout)
}

func fetchSubscriptionOnce(rawURL string, d Device, proxyAddr string, timeout time.Duration) *SubResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout*3)
	defer cancel()
	res := &SubResult{FetchedAt: time.Now().Unix()}
	c := httpClient(proxyAddr, timeout)
	resp, body, err := get(ctx, c, rawURL, d)
	if err != nil && isCertError(err) {
		// 1.0.8 (audit H1): never retry without certificate verification.
		// A literal-IP sub server (self-signed panel certificate) is
		// retried only with the certificate the user approved (SHA-256
		// pin); otherwise the app gets err.subCertUntrusted:<sha256> (IP)
		// or err.subCertInvalid (domain). See subtls.go.
		if pin := subPinFor(rawURL); pin != "" {
			c = pinnedSubClient(proxyAddr, timeout, pin)
			resp, body, err = get(ctx, c, rawURL, d)
			err = cleanSubCertError(err)
		} else {
			pushLogLevel("warn", "subscription: TLS certificate not trusted ("+certErrorKind(err)+"); not fetched")
			err = subCertFailure(ctx, rawURL, proxyAddr, timeout, err)
		}
	}
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		return res
	}
	res.HTTPStatusCode = resp.StatusCode
	if resp.StatusCode/100 != 2 {
		res.Errors = append(res.Errors, fmt.Sprintf("HTTP %d", resp.StatusCode))
		if r := resp.Header.Get("X-Veilon-Refused"); r != "" {
			res.Refused = r
		}
		return res
	}
	h := resp.Header
	// 1.0.12: the same metadata may also come as body directive lines
	// ("#profile-title: …"); a non-empty response header always wins.
	content, dir := splitBodyDirectives(string(body))
	meta := func(k string) string {
		if v := strings.TrimSpace(h.Get(k)); v != "" {
			return v
		}
		return dir[k]
	}
	res.UserInfo = ParseUserInfo(meta("subscription-userinfo"))
	res.Title = decodeHeaderText(meta("profile-title"))
	res.UpdateHours, _ = strconv.Atoi(strings.TrimSpace(meta("profile-update-interval")))
	res.Refused = h.Get("X-Veilon-Refused")
	res.SupportURL = meta("support-url")
	res.WebPageURL = meta("profile-web-page-url")
	res.Announce = decodeHeaderText(meta("announce"))
	res.Description = cleanDescription(decodeHeaderText(meta("profile-description")))
	pr := ParseContent(content)
	res.Profiles, res.Errors = pr.Profiles, append(res.Errors, pr.Errors...)
	if res.Refused != "" {
		return res
	}
	have := map[string]bool{}
	for _, p := range res.Profiles {
		have[p.ID] = true
	}
	extra := func(format string, keep func(*Profile) bool) {
		if ctx.Err() != nil {
			return
		}
		r2, b2, err := get(ctx, c, withFormat(rawURL, format), d)
		if err != nil || r2.StatusCode/100 != 2 {
			return
		}
		for _, p := range ParseContent(string(b2)).Profiles {
			if keep(p) && !have[p.ID] {
				have[p.ID] = true
				res.Profiles = append(res.Profiles, p)
			}
		}
	}
	extra("clash", func(p *Profile) bool { return p.Type == TypeAmneziaWG })
	extra("json", func(p *Profile) bool { return p.Type == TypeMasque })
	return res
}

// isCertError reports whether err is a TLS certificate verification failure
// (unknown authority, name/IP mismatch, expired…), not a network error.
func isCertError(err error) bool {
	var (
		ve *tls.CertificateVerificationError
		ua x509.UnknownAuthorityError
		he x509.HostnameError
		ci x509.CertificateInvalidError
	)
	return errors.As(err, &ve) || errors.As(err, &ua) || errors.As(err, &he) || errors.As(err, &ci)
}

func certErrorKind(err error) string {
	var (
		ua x509.UnknownAuthorityError
		he x509.HostnameError
		ci x509.CertificateInvalidError
	)
	switch {
	case errors.As(err, &ua):
		return "unknown authority / self-signed"
	case errors.As(err, &he):
		return "name or IP mismatch"
	case errors.As(err, &ci):
		return "invalid or expired"
	}
	return "verification failed"
}

// encodeJSON marshals v for the app.
func encodeJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":` + strconv.Quote(err.Error()) + `}`
	}
	return string(b)
}

var _ = base64.StdEncoding
