// Package userfw is the per-user firewall: a manual list of domains and
// IP addresses / ranges an admin blocks for one user only. It keeps no
// record of what a user visits; it only checks, normalises and renders
// the admin's own list.
//
// Entry forms (stored canonical form in brackets):
//
//	example.com, domain:example.com, *.example.com, https://example.com/x
//	                          [example.com]          the domain and every subdomain
//	full:example.com          [full:example.com]     exactly this name
//	regexp:\.example\.(com|net)$  [regexp:...]       a regular expression on the name
//	1.2.3.4, 2001:db8::1      [1.2.3.4]              one address
//	10.0.0.0/8, 2001:db8::/32 [10.0.0.0/8]           a range (masked)
//
// Xray and sing-box apply every form; AmneziaWG (kernel tunnel, nftables)
// applies addresses and ranges only.
package userfw

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Limits.
const (
	// MaxEntries is the most entries one user can have.
	MaxEntries = 200
	// MaxLen is the longest entry (a full domain name is at most 253).
	MaxLen = 253
	// MinBits4 / MinBits6 are the widest ranges accepted: a wider one
	// would cut the user off from a large part of the internet (disable
	// the user instead).
	MinBits4 = 8
	MinBits6 = 16
)

// Kinds of entry.
const (
	KindDomain = "domain" // the domain and its subdomains
	KindFull   = "full"   // exactly this domain
	KindRegexp = "regexp" // regular expression on the domain
	KindIP     = "ip"     // address or range
)

// Reasons an entry is refused (stable codes for the UI and the bot).
const (
	ReasonEmpty       = "empty"
	ReasonTooLong     = "too_long"
	ReasonBadDomain   = "bad_domain"
	ReasonBadRegexp   = "bad_regexp"
	ReasonBadIP       = "bad_ip"
	ReasonTooBroad    = "too_broad"
	ReasonUnsupported = "unsupported"
	ReasonTooMany     = "too_many"
)

// EntryError says which entry was refused and why.
type EntryError struct {
	Entry  string
	Reason string
}

func (e *EntryError) Error() string {
	switch e.Reason {
	case ReasonTooMany:
		return fmt.Sprintf("firewall: at most %d entries per user", MaxEntries)
	case ReasonTooLong:
		return fmt.Sprintf("firewall: %q is longer than %d characters", e.Entry, MaxLen)
	case ReasonBadDomain:
		return fmt.Sprintf("firewall: %q is not a valid domain", e.Entry)
	case ReasonBadRegexp:
		return fmt.Sprintf("firewall: %q is not a valid regular expression", e.Entry)
	case ReasonBadIP:
		return fmt.Sprintf("firewall: %q is not a valid IP address or range", e.Entry)
	case ReasonTooBroad:
		return fmt.Sprintf("firewall: %q is too wide (IPv4 /%d or narrower, IPv6 /%d or narrower)", e.Entry, MinBits4, MinBits6)
	case ReasonUnsupported:
		return fmt.Sprintf("firewall: %q uses an unsupported prefix (use a domain, full:, regexp:, an IP or a CIDR)", e.Entry)
	}
	return fmt.Sprintf("firewall: %q is empty", e.Entry)
}

var labelRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?$`)

// knownPrefix matches "word:" prefixes the panel does not take (geosite:,
// geoip:, keyword:, ext: ...), so they are refused instead of read as a
// host name with a port.
var knownPrefix = regexp.MustCompile(`^[a-z][a-z0-9_-]*:`)

// hostPort is a host name with a port ("example.com:443").
var hostPort = regexp.MustCompile(`^([a-z0-9_.*-]+):([0-9]{1,5})$`)

// Normalize checks one entry and returns its canonical form and kind.
func Normalize(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", &EntryError{Entry: raw, Reason: ReasonEmpty}
	}
	if len(s) > MaxLen {
		return "", "", &EntryError{Entry: s, Reason: ReasonTooLong}
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "regexp:"):
		re := strings.TrimSpace(s[len("regexp:"):])
		if re == "" {
			return "", "", &EntryError{Entry: s, Reason: ReasonBadRegexp}
		}
		if _, err := regexp.Compile(re); err != nil {
			return "", "", &EntryError{Entry: s, Reason: ReasonBadRegexp}
		}
		return "regexp:" + re, KindRegexp, nil
	case strings.HasPrefix(low, "full:"):
		d, ok := domain(low[len("full:"):])
		if !ok {
			return "", "", &EntryError{Entry: s, Reason: ReasonBadDomain}
		}
		return "full:" + d, KindFull, nil
	case strings.HasPrefix(low, "domain:"):
		d, ok := domain(low[len("domain:"):])
		if !ok {
			return "", "", &EntryError{Entry: s, Reason: ReasonBadDomain}
		}
		return d, KindDomain, nil
	}
	if strings.Contains(low, "://") {
		// A pasted link: its host name.
		low = low[strings.Index(low, "://")+3:]
		if i := strings.IndexAny(low, "/?#"); i >= 0 {
			low = low[:i]
		}
		if i := strings.LastIndex(low, "@"); i >= 0 {
			low = low[i+1:]
		}
		if strings.HasPrefix(low, "[") {
			if i := strings.Index(low, "]"); i > 0 {
				low = low[1:i]
			}
		} else if i := strings.LastIndex(low, ":"); i >= 0 && strings.Count(low, ":") == 1 {
			low = low[:i]
		}
	}
	if v, ok, err := ipEntry(low, s); ok || err != nil {
		if err != nil {
			return "", "", err
		}
		return v, KindIP, nil
	}
	if strings.Contains(low, ":") {
		// Not an IPv6 address (tried above): "host:port" or a prefix the
		// panel does not take (geosite:, geoip:, keyword: ...).
		if m := hostPort.FindStringSubmatch(low); m != nil {
			low = m[1]
			if v, ok, err := ipEntry(low, s); ok || err != nil {
				if err != nil {
					return "", "", err
				}
				return v, KindIP, nil
			}
		} else if knownPrefix.MatchString(low) {
			return "", "", &EntryError{Entry: s, Reason: ReasonUnsupported}
		} else {
			return "", "", &EntryError{Entry: s, Reason: ReasonBadDomain}
		}
	}
	d, ok := domain(low)
	if !ok {
		if looksIP(low) {
			return "", "", &EntryError{Entry: s, Reason: ReasonBadIP}
		}
		return "", "", &EntryError{Entry: s, Reason: ReasonBadDomain}
	}
	return d, KindDomain, nil
}

// ipEntry reads an address or a range. ok is false when s is not one.
func ipEntry(s, orig string) (string, bool, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			if looksIP(strings.SplitN(s, "/", 2)[0]) {
				return "", false, &EntryError{Entry: orig, Reason: ReasonBadIP}
			}
			return "", false, nil
		}
		a := p.Addr().Unmap()
		bits := p.Bits()
		if p.Addr().Is4In6() {
			bits -= 96
			if bits < 0 {
				return "", false, &EntryError{Entry: orig, Reason: ReasonBadIP}
			}
		}
		if a.Zone() != "" {
			return "", false, &EntryError{Entry: orig, Reason: ReasonBadIP}
		}
		p = netip.PrefixFrom(a, bits).Masked()
		if (a.Is4() && bits < MinBits4) || (a.Is6() && bits < MinBits6) {
			return "", false, &EntryError{Entry: orig, Reason: ReasonTooBroad}
		}
		if p.IsSingleIP() {
			return p.Addr().String(), true, nil
		}
		return p.String(), true, nil
	}
	a, err := netip.ParseAddr(strings.Trim(s, "[]"))
	if err != nil {
		return "", false, nil
	}
	if a.Zone() != "" {
		return "", false, &EntryError{Entry: orig, Reason: ReasonBadIP}
	}
	return a.Unmap().String(), true, nil
}

var ipish = regexp.MustCompile(`^[0-9.]+$|^[0-9a-f:]*:[0-9a-f:.]*$`)

func looksIP(s string) bool { return ipish.MatchString(s) }

// domain checks and lowercases a domain name ("*." and a trailing dot are
// dropped). Single labels ("ir") are allowed: they block a whole TLD.
func domain(s string) (string, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "*.")
	s = strings.TrimPrefix(s, ".")
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return "", false
	}
	allDigits := true
	for _, l := range strings.Split(s, ".") {
		if !labelRe.MatchString(l) {
			return "", false
		}
		if strings.Trim(l, "0123456789") != "" {
			allDigits = false
		}
	}
	if allDigits {
		return "", false // a broken IPv4 address, not a name
	}
	return s, true
}

// NormalizeList checks a whole list: canonical forms, duplicates removed
// (first one kept, order kept), at most MaxEntries.
func NormalizeList(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		v, _, err := Normalize(raw)
		if err != nil {
			return nil, err
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) > MaxEntries {
		return nil, &EntryError{Reason: ReasonTooMany}
	}
	return out, nil
}

// Join stores a normalised list (one entry per line).
func Join(list []string) string { return strings.Join(list, "\n") }

// Split reads a stored list. Entries that no longer pass the checks are
// left out (a bad row must never break a config).
func Split(stored string) []string {
	if strings.TrimSpace(stored) == "" {
		return []string{}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, l := range strings.Split(stored, "\n") {
		v, _, err := Normalize(l)
		if err != nil || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Kind returns the kind of a canonical entry.
func Kind(v string) string {
	switch {
	case strings.HasPrefix(v, "regexp:"):
		return KindRegexp
	case strings.HasPrefix(v, "full:"):
		return KindFull
	}
	if _, err := netip.ParsePrefix(v); err == nil {
		return KindIP
	}
	if _, err := netip.ParseAddr(v); err == nil {
		return KindIP
	}
	return KindDomain
}

// Rules is one user's list rendered for the engines.
type Rules struct {
	// XrayDomains are Xray routing "domain" values (domain:, full:,
	// regexp:).
	XrayDomains []string
	// XrayIPs are Xray routing "ip" values (addresses and CIDRs).
	XrayIPs []string
	// Suffix / Exact / Regex are sing-box domain_suffix, domain and
	// domain_regex values (a "domain" entry is in Exact as itself and in
	// Suffix with a leading dot).
	Suffix, Exact, Regex []string
	// CIDRs are every address and range as a prefix (sing-box ip_cidr,
	// nftables).
	CIDRs []string
}

// Empty reports whether nothing is blocked.
func (r Rules) Empty() bool { return len(r.XrayDomains) == 0 && len(r.CIDRs) == 0 }

// Render turns a canonical list into engine values (sorted, stable).
func Render(list []string) Rules {
	var r Rules
	for _, v := range list {
		switch Kind(v) {
		case KindRegexp:
			re := v[len("regexp:"):]
			r.XrayDomains = append(r.XrayDomains, v)
			r.Regex = append(r.Regex, re)
		case KindFull:
			r.XrayDomains = append(r.XrayDomains, v)
			r.Exact = append(r.Exact, v[len("full:"):])
		case KindDomain:
			// sing-box: the name itself plus ".name" for its subdomains
			// (never a longer name that merely ends the same way).
			r.XrayDomains = append(r.XrayDomains, "domain:"+v)
			r.Exact = append(r.Exact, v)
			r.Suffix = append(r.Suffix, "."+v)
		case KindIP:
			r.XrayIPs = append(r.XrayIPs, v)
			r.CIDRs = append(r.CIDRs, Prefix(v))
		}
	}
	for _, s := range [][]string{r.XrayDomains, r.XrayIPs, r.Suffix, r.Exact, r.Regex, r.CIDRs} {
		sort.Strings(s)
	}
	return r
}

// Prefix returns an address or range as a prefix ("1.2.3.4" ->
// "1.2.3.4/32"); "" when it is neither.
func Prefix(v string) string {
	if p, err := netip.ParsePrefix(v); err == nil {
		return p.Masked().String()
	}
	if a, err := netip.ParseAddr(v); err == nil {
		return netip.PrefixFrom(a, a.BitLen()).String()
	}
	return ""
}

// CIDRs returns only the address and range entries of a stored list, as
// prefixes (AmneziaWG applies these only).
func CIDRs(stored string) []string { return Render(Split(stored)).CIDRs }
