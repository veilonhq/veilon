package xray

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// KeyTemplate stores the admin's Xray config template (outbounds, routing,
// balancers, DNS, basic options). The "_" prefix keeps it out of the public
// settings API; it has its own endpoints.
const KeyTemplate = "_xray_template"

// Reserved outbound tags. "direct" and "blocked" are used by the panel's own
// rules and must always exist; "api" belongs to the stats API.
const (
	TagDirect  = "direct"
	TagBlocked = "blocked"
	TagAPI     = "api"
)

// Template is the admin-editable part of the Xray config. Outbounds, rules,
// balancers and DNS are kept as native Xray objects, so the advanced editor
// and the forms edit the same document. Keys starting with "_" inside
// outbounds, rules and balancers are panel metadata ("_disabled", "_name")
// and are stripped before the config is rendered.
type Template struct {
	Log struct {
		Level  string `json:"loglevel,omitempty"`
		DNSLog bool   `json:"dnsLog,omitempty"`
	} `json:"log"`
	DNS       map[string]any   `json:"dns,omitempty"`
	Outbounds []map[string]any `json:"outbounds"`
	Routing   struct {
		DomainStrategy string           `json:"domainStrategy,omitempty"`
		Rules          []map[string]any `json:"rules"`
		Balancers      []map[string]any `json:"balancers"`
	} `json:"routing"`
	Observatory      map[string]any `json:"observatory,omitempty"`
	BurstObservatory map[string]any `json:"burstObservatory,omitempty"`
	// BlockPrivate drops traffic to private/LAN addresses (geoip:private).
	// nil means on (the panel's long-standing default).
	BlockPrivate *bool `json:"block_private,omitempty"`
}

// DefaultTemplate is what a panel without a saved template renders: the
// config Veilon always produced (direct first, then blocked).
func DefaultTemplate() *Template {
	t := &Template{}
	t.Log.Level = "warning"
	t.Routing.DomainStrategy = "AsIs"
	t.Outbounds = []map[string]any{
		{"tag": TagDirect, "protocol": "freedom", "settings": map[string]any{}},
		{"tag": TagBlocked, "protocol": "blackhole", "settings": map[string]any{}},
	}
	t.Routing.Rules = []map[string]any{}
	t.Routing.Balancers = []map[string]any{}
	return t
}

// BlocksPrivate reports whether private destinations are blocked.
func (t *Template) BlocksPrivate() bool { return t.BlockPrivate == nil || *t.BlockPrivate }

var (
	logLevels        = map[string]bool{"debug": true, "info": true, "warning": true, "error": true, "none": true}
	domainStrategies = map[string]bool{"AsIs": true, "IPIfNonMatch": true, "IPOnDemand": true}
	outProtocols     = map[string]bool{"freedom": true, "blackhole": true, "socks": true, "http": true, "vless": true,
		"vmess": true, "trojan": true, "shadowsocks": true, "wireguard": true, "dns": true, "loopback": true}
	balancerStrategies = map[string]bool{"random": true, "roundRobin": true, "leastPing": true, "leastLoad": true}
	tagRx              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@:-]{0,63}$`)
	// rule fields that select traffic; a rule needs at least one of them,
	// or it would match everything by accident.
	ruleMatchers = []string{"domain", "ip", "port", "sourcePort", "network", "source", "user", "inboundTag", "protocol", "attrs", "localPort", "localIP", "vlessRoute", "process"}
)

// TemplateError is a validation problem in a template.
type TemplateError struct{ Msg string }

func (e *TemplateError) Error() string { return e.Msg }

func terr(f string, a ...any) error { return &TemplateError{Msg: fmt.Sprintf(f, a...)} }

// ParseTemplate decodes and validates a template document, filling in the
// reserved outbounds when they are missing.
func ParseTemplate(b []byte) (*Template, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return DefaultTemplate(), nil
	}
	t := &Template{}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(t); err != nil {
		return nil, terr("invalid JSON: %v", err)
	}
	if err := t.Normalize(); err != nil {
		return nil, err
	}
	return t, nil
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return strings.TrimSpace(s)
}

func strList(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	case string:
		return []string{x}
	}
	return nil
}

func disabled(m map[string]any) bool { b, _ := m["_disabled"].(bool); return b }

// Normalize validates the template and adds missing reserved outbounds.
func (t *Template) Normalize() error {
	if t.Log.Level == "" {
		t.Log.Level = "warning"
	}
	if !logLevels[t.Log.Level] {
		return terr("log level %q is not one of debug, info, warning, error, none", t.Log.Level)
	}
	if t.Routing.DomainStrategy == "" {
		t.Routing.DomainStrategy = "AsIs"
	}
	if !domainStrategies[t.Routing.DomainStrategy] {
		return terr("domain strategy %q is not one of AsIs, IPIfNonMatch, IPOnDemand", t.Routing.DomainStrategy)
	}
	if t.Routing.Rules == nil {
		t.Routing.Rules = []map[string]any{}
	}
	if t.Routing.Balancers == nil {
		t.Routing.Balancers = []map[string]any{}
	}
	tags := map[string]bool{}
	for i, o := range t.Outbounds {
		if o == nil {
			return terr("outbound %d is empty", i+1)
		}
		tag, proto := str(o, "tag"), str(o, "protocol")
		if tag == "" {
			return terr("outbound %d has no tag", i+1)
		}
		if !tagRx.MatchString(tag) {
			return terr("outbound tag %q may use only letters, digits and . _ @ : -", tag)
		}
		if tag == TagAPI || strings.HasPrefix(tag, "inbound-") || tag == BridgeTag {
			return terr("outbound tag %q is reserved", tag)
		}
		if tags[tag] {
			return terr("outbound tag %q is used twice", tag)
		}
		if !outProtocols[proto] {
			return terr("outbound %q: protocol %q is not supported", tag, proto)
		}
		if tag == TagDirect && proto != "freedom" {
			return terr(`outbound "direct" must use the freedom protocol`)
		}
		if tag == TagBlocked && proto != "blackhole" {
			return terr(`outbound "blocked" must use the blackhole protocol`)
		}
		tags[tag] = true
	}
	if !tags[TagDirect] {
		t.Outbounds = append(t.Outbounds, map[string]any{"tag": TagDirect, "protocol": "freedom", "settings": map[string]any{}})
		tags[TagDirect] = true
	}
	if !tags[TagBlocked] {
		t.Outbounds = append(t.Outbounds, map[string]any{"tag": TagBlocked, "protocol": "blackhole", "settings": map[string]any{}})
		tags[TagBlocked] = true
	}
	bal := map[string]bool{}
	for i, b := range t.Routing.Balancers {
		if b == nil {
			return terr("balancer %d is empty", i+1)
		}
		tag := str(b, "tag")
		if tag == "" || !tagRx.MatchString(tag) {
			return terr("balancer %d needs a tag (letters, digits and . _ @ : -)", i+1)
		}
		if bal[tag] || tags[tag] {
			return terr("balancer tag %q is already used", tag)
		}
		sel := strList(b["selector"])
		if len(sel) == 0 {
			return terr("balancer %q needs at least one outbound in its selector", tag)
		}
		matched := false
		for _, p := range sel {
			for ot := range tags {
				if strings.HasPrefix(ot, p) {
					matched = true
				}
			}
		}
		if !matched {
			return terr("balancer %q: no outbound tag starts with %s", tag, strings.Join(sel, ", "))
		}
		if st, ok := b["strategy"].(map[string]any); ok {
			if ty := str(st, "type"); ty != "" && !balancerStrategies[ty] {
				return terr("balancer %q: strategy %q is not one of random, roundRobin, leastPing, leastLoad", tag, ty)
			}
		}
		if fb := str(b, "fallbackTag"); fb != "" && !tags[fb] {
			return terr("balancer %q: fallback outbound %q does not exist", tag, fb)
		}
		bal[tag] = true
	}
	for i, r := range t.Routing.Rules {
		if r == nil {
			return terr("rule %d is empty", i+1)
		}
		out, bt := str(r, "outboundTag"), str(r, "balancerTag")
		switch {
		case out == "" && bt == "":
			return terr("rule %d needs an outbound or a balancer", i+1)
		case out != "" && bt != "":
			return terr("rule %d has both an outbound and a balancer", i+1)
		case out != "" && !tags[out]:
			return terr("rule %d: outbound %q does not exist", i+1, out)
		case bt != "" && !bal[bt]:
			return terr("rule %d: balancer %q does not exist", i+1, bt)
		}
		has := false
		for _, k := range ruleMatchers {
			if v, ok := r[k]; ok && v != nil && v != "" {
				if l, isList := v.([]any); isList && len(l) == 0 {
					continue
				}
				has = true
			}
		}
		if !has {
			return terr("rule %d has no condition (domain, ip, port, network, protocol, inbound or user)", i+1)
		}
	}
	return nil
}

// cleanCopy returns m without panel metadata keys ("_..."), deep-copied
// through JSON so rendering never mutates the template.
func cleanCopy(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	for k := range out {
		if strings.HasPrefix(k, "_") {
			delete(out, k)
		}
	}
	return out
}

// renderTemplate merges the template into a config that already holds the
// panel's inbounds and rules. users maps usernames to Xray emails, so rules
// may name users by username.
func renderTemplate(cfg obj, t *Template, panelRules []any, users map[string]string, aliases map[string]string) {
	lg := obj{"loglevel": t.Log.Level}
	if t.Log.DNSLog {
		lg["dnsLog"] = true
	}
	cfg["log"] = lg
	outs := make([]any, 0, len(t.Outbounds))
	for _, o := range t.Outbounds {
		if disabled(o) && str(o, "tag") != TagDirect && str(o, "tag") != TagBlocked {
			continue
		}
		c := cleanCopy(o)
		if _, ok := c["settings"]; !ok {
			c["settings"] = obj{}
		}
		outs = append(outs, c)
	}
	cfg["outbounds"] = outs
	live := map[string]bool{}
	for _, o := range outs {
		live[str(o.(map[string]any), "tag")] = true
	}
	rules := append([]any{}, panelRules...)
	balUsed := map[string]bool{}
	var bals []any
	var observe []string
	needObs := false
	for _, b := range t.Routing.Balancers {
		if disabled(b) {
			continue
		}
		c := cleanCopy(b)
		bals = append(bals, c)
		balUsed[str(c, "tag")] = true
		if st, ok := c["strategy"].(map[string]any); ok {
			if ty := str(st, "type"); ty == "leastPing" || ty == "leastLoad" {
				needObs = true
				observe = append(observe, strList(c["selector"])...)
			}
		}
	}
	for _, r := range t.Routing.Rules {
		if disabled(r) {
			continue
		}
		c := cleanCopy(r)
		if out := str(c, "outboundTag"); out != "" && !live[out] {
			continue // its outbound is switched off
		}
		if bt := str(c, "balancerTag"); bt != "" && !balUsed[bt] {
			continue
		}
		if us, ok := c["user"]; ok {
			list := strList(us)
			mapped := make([]string, 0, len(list))
			for _, u := range list {
				if e, ok := users[u]; ok {
					mapped = append(mapped, e)
				} else if u != "" {
					mapped = append(mapped, u)
				}
			}
			if len(mapped) == 0 {
				continue // an empty user list would match everyone
			}
			c["user"] = withAliases(mapped, aliases)
		}
		c["type"] = "field"
		rules = append(rules, c)
	}
	routing := obj{"domainStrategy": t.Routing.DomainStrategy, "rules": rules}
	if len(bals) > 0 {
		routing["balancers"] = bals
	}
	cfg["routing"] = routing
	if len(t.DNS) > 0 {
		cfg["dns"] = cleanCopy(t.DNS)
	}
	switch {
	case len(t.Observatory) > 0:
		cfg["observatory"] = cleanCopy(t.Observatory)
	case needObs:
		sort.Strings(observe)
		uniq := observe[:0]
		for i, s := range observe {
			if i == 0 || s != observe[i-1] {
				uniq = append(uniq, s)
			}
		}
		cfg["observatory"] = obj{"subjectSelector": uniq, "probeUrl": "https://www.gstatic.com/generate_204",
			"probeInterval": "1m", "enableConcurrency": true}
	}
	if len(t.BurstObservatory) > 0 {
		cfg["burstObservatory"] = cleanCopy(t.BurstObservatory)
	}
}

// MarshalTemplate renders a template as indented JSON for the editor.
func MarshalTemplate(t *Template) []byte {
	b, _ := json.MarshalIndent(t, "", "  ")
	return b
}
