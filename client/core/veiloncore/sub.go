package veiloncore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseResult is the outcome of parsing a subscription body or pasted text.
type ParseResult struct {
	Profiles []*Profile `json:"profiles"`
	Errors   []string   `json:"errors,omitempty"`
}

func (r *ParseResult) add(p *Profile) {
	for _, q := range r.Profiles {
		if q.ID == p.ID {
			return
		}
	}
	r.Profiles = append(r.Profiles, p)
}

// ParseContent parses any supported subscription body: base64 or plain
// share links, a WireGuard/AmneziaWG .conf, sing-box JSON, Xray JSON
// (object or array) or Clash/Mihomo YAML.
func ParseContent(body string) *ParseResult {
	res := &ParseResult{}
	body = strings.TrimSpace(strings.TrimPrefix(body, "\ufeff"))
	if body == "" {
		return res
	}
	switch {
	case strings.HasPrefix(strings.ToLower(body), "vpn://") && !strings.ContainsAny(strings.TrimSpace(body), "\n "):
		// Amnezia export (one container list → several profiles).
		ps, err := ParseAmneziaVPN(body)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
		}
		for _, p := range ps {
			res.add(p)
		}
		return res
	case strings.HasPrefix(body, "{") || strings.HasPrefix(body, "["):
		if ps, err := parseJSONConfig([]byte(body)); err == nil {
			for _, p := range ps {
				res.add(p)
			}
			return res
		} else {
			res.Errors = append(res.Errors, err.Error())
			return res
		}
	case strings.Contains(body, "[Interface]") && strings.Contains(body, "[Peer]"):
		p, err := ParseWGConf(body, "")
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
		} else {
			res.add(p)
		}
		return res
	case strings.Contains(body, "proxies:"):
		if ps, err := parseClash([]byte(body)); err == nil && len(ps) > 0 {
			for _, p := range ps {
				res.add(p)
			}
			return res
		}
	}
	text := body
	if !strings.Contains(body, "://") {
		if raw, err := decodeB64(body); err == nil {
			text = string(raw)
			if strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") {
				return ParseContent(text)
			}
		}
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		p, err := ParseLink(line)
		if err != nil {
			if !errors.Is(err, errUnsupported) {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", shorten(line), err))
			}
			continue
		}
		res.add(p)
	}
	return res
}

func shorten(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

// ParseWGConf parses a WireGuard or AmneziaWG client configuration.
func ParseWGConf(text, name string) (*Profile, error) {
	w := &WGConfig{}
	a := &AWG{}
	hasAWG := false
	section := ""
	var endpoint string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[] "))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		atoi := func() int { n, _ := strconv.Atoi(v); return n }
		switch section {
		case "interface":
			switch k {
			case "privatekey":
				w.PrivateKey = v
			case "address":
				w.Address = append(w.Address, splitList(v)...)
			case "dns":
				w.DNS = append(w.DNS, splitList(v)...)
			case "mtu":
				w.MTU = atoi()
			case "jc":
				a.Jc, hasAWG = atoi(), true
			case "jmin":
				a.Jmin, hasAWG = atoi(), true
			case "jmax":
				a.Jmax, hasAWG = atoi(), true
			case "s1":
				a.S1, hasAWG = atoi(), true
			case "s2":
				a.S2, hasAWG = atoi(), true
			case "s3":
				a.S3, hasAWG = atoi(), true
			case "s4":
				a.S4, hasAWG = atoi(), true
			case "h1":
				a.H1, hasAWG = v, true
			case "h2":
				a.H2, hasAWG = v, true
			case "h3":
				a.H3, hasAWG = v, true
			case "h4":
				a.H4, hasAWG = v, true
			case "i1":
				a.I1, hasAWG = v, true
			case "i2":
				a.I2, hasAWG = v, true
			case "i3":
				a.I3, hasAWG = v, true
			case "i4":
				a.I4, hasAWG = v, true
			case "i5":
				a.I5, hasAWG = v, true
			case "headerprotectionkey":
				a.HeaderProtectionKey, hasAWG = v, true
			case "contentpaddingaddition":
				a.ContentPaddingAddition, hasAWG = v, true
			case "rekeyaftertime":
				a.RekeyAfterTime, hasAWG = v, true
			case "rekeytimeout":
				a.RekeyTimeout, hasAWG = v, true
			case "rejectaftertime":
				a.RejectAfterTime, hasAWG = v, true
			case "keepalivetimeout":
				a.KeepaliveTimeout, hasAWG = v, true
			case "maxhandshakeattempts":
				a.MaxHandshakeAttempts, hasAWG = v, true
			case "randomtrailers":
				a.RandomTrailers, hasAWG = confBool(v), true
			case "disablecookies":
				a.DisableCookies, hasAWG = confBool(v), true
			}
		case "peer":
			switch k {
			case "publickey":
				w.PeerPublicKey = v
			case "presharedkey":
				w.PresharedKey = v
			case "endpoint":
				endpoint = v
			case "persistentkeepalive":
				if strings.Contains(v, "-") {
					a.PersistentKeepalive = strings.Join(strings.Fields(v), "")
				} else {
					w.Keepalive = atoi()
				}
			case "allowedips":
				w.AllowedIPs = append(w.AllowedIPs, splitList(v)...)
			}
		}
	}
	if w.PrivateKey == "" || w.PeerPublicKey == "" || endpoint == "" {
		return nil, errors.New("conf: missing PrivateKey, PublicKey or Endpoint")
	}
	host, port, err := splitHostPort(endpoint)
	if err != nil {
		return nil, fmt.Errorf("conf: endpoint: %w", err)
	}
	if len(w.Address) == 0 {
		return nil, errors.New("conf: missing Address")
	}
	fixWGAddresses(w)
	p := &Profile{Type: TypeWireGuard, Name: name, Server: host, Port: port, WG: w}
	if hasAWG && !awgIsPlainWG(a) {
		p.Type = TypeAmneziaWG
		w.AWG = a
	}
	if p.Name == "" {
		p.Name = host
	}
	p.normalize()
	return p, nil
}

// confBool reads an awg-quick switch ("on", "true", "1", "yes").
func confBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "true", "1", "yes":
		return true
	}
	return false
}

// awgIsPlainWG reports AWG params that are equivalent to vanilla WireGuard.
func awgIsPlainWG(a *AWG) bool {
	h := func(s string) bool { return s == "" || s == "1" || s == "2" || s == "3" || s == "4" }
	return !a.awg3() && a.PersistentKeepalive == "" && a.Jc == 0 && a.S1 == 0 && a.S2 == 0 && a.S3 == 0 && a.S4 == 0 && h(a.H1) && h(a.H2) && h(a.H3) && h(a.H4) &&
		a.I1 == "" && a.I2 == "" && a.I3 == "" && a.I4 == "" && a.I5 == ""
}

// ---------- sing-box / Xray JSON ----------

func parseJSONConfig(b []byte) ([]*Profile, error) {
	var any0 any
	if err := json.Unmarshal(b, &any0); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	var out []*Profile
	switch v := any0.(type) {
	case []any:
		for _, c := range v {
			if m, ok := c.(map[string]any); ok {
				out = append(out, profilesFromConfigObject(m)...)
			}
		}
	case map[string]any:
		out = profilesFromConfigObject(v)
	}
	if len(out) == 0 {
		return nil, errors.New("json: no supported outbounds")
	}
	return out, nil
}

func profilesFromConfigObject(m map[string]any) []*Profile {
	var out []*Profile
	remark, _ := m["remarks"].(string)
	obs, _ := m["outbounds"].([]any)
	for _, o := range obs {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		var p *Profile
		if _, isXray := om["protocol"]; isXray {
			p = profileFromXrayOutbound(om)
			if p != nil && remark != "" {
				p.Name = remark
			}
		} else {
			p = profileFromSingboxOutbound(om)
		}
		if p != nil {
			p.normalize()
			out = append(out, p)
		}
	}
	eps, _ := m["endpoints"].([]any)
	for _, e := range eps {
		if em, ok := e.(map[string]any); ok {
			if p := profileFromSingboxOutbound(em); p != nil {
				p.normalize()
				out = append(out, p)
			}
		}
	}
	return out
}

type jm = map[string]any

func gs(m jm, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	}
	return ""
}
func gi(m jm, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
func gb(m jm, k string) bool { b, _ := m[k].(bool); return b }
func gm(m jm, k string) jm   { x, _ := m[k].(map[string]any); return x }
func gl(m jm, k string) []string {
	switch v := m[k].(type) {
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return splitList(v)
	}
	return nil
}

func profileFromSingboxOutbound(o jm) *Profile {
	t := gs(o, "type")
	p := &Profile{Name: gs(o, "tag"), Server: gs(o, "server"), Port: gi(o, "server_port")}
	switch t {
	case "vless":
		p.Type, p.UUID, p.Flow = TypeVLESS, gs(o, "uuid"), gs(o, "flow")
	case "vmess":
		p.Type, p.UUID, p.VMessSecurity, p.AlterID = TypeVMess, gs(o, "uuid"), gs(o, "security"), gi(o, "alter_id")
	case "trojan":
		p.Type, p.Password = TypeTrojan, gs(o, "password")
	case "shadowsocks":
		p.Type, p.Method, p.Password = TypeShadowsocks, gs(o, "method"), gs(o, "password")
	case "hysteria2":
		p.Type, p.Password, p.UpMbps, p.DownMbps = TypeHysteria2, gs(o, "password"), gi(o, "up_mbps"), gi(o, "down_mbps")
		if ob := gm(o, "obfs"); ob != nil {
			p.ObfsPassword = gs(ob, "password")
		}
	case "tuic":
		p.Type, p.UUID, p.Password, p.Congestion, p.UDPRelayMode = TypeTUIC, gs(o, "uuid"), gs(o, "password"), gs(o, "congestion_control"), gs(o, "udp_relay_mode")
	case "http":
		p.Type, p.Username, p.Password = TypeHTTP, gs(o, "username"), gs(o, "password")
	case "socks":
		p.Type, p.Username, p.Password = TypeSOCKS, gs(o, "username"), gs(o, "password")
	case "wireguard":
		peers, _ := o["peers"].([]any)
		w := &WGConfig{PrivateKey: gs(o, "private_key"), Address: gl(o, "address"), MTU: gi(o, "mtu")}
		if len(w.Address) == 0 {
			w.Address = gl(o, "local_address")
		}
		if len(peers) > 0 {
			pm, _ := peers[0].(map[string]any)
			p.Server, p.Port = gs(pm, "address"), gi(pm, "port")
			w.PeerPublicKey, w.PresharedKey, w.Keepalive = gs(pm, "public_key"), gs(pm, "pre_shared_key"), gi(pm, "persistent_keepalive_interval")
		} else {
			w.PeerPublicKey, w.PresharedKey = gs(o, "peer_public_key"), gs(o, "pre_shared_key")
		}
		if w.PrivateKey == "" || w.PeerPublicKey == "" || p.Server == "" {
			return nil
		}
		fixWGAddresses(w)
		p.Type, p.WG = TypeWireGuard, w
		return p
	default:
		return nil
	}
	if p.Server == "" || p.Port == 0 {
		return nil
	}
	if tr := gm(o, "transport"); tr != nil {
		p.Network = gs(tr, "type")
		p.Path = gs(tr, "path")
		switch p.Network {
		case "ws":
			if h := gm(tr, "headers"); h != nil {
				p.Host = gs(h, "Host")
			}
		case "httpupgrade":
			p.Host = gs(tr, "host")
		case "grpc":
			p.ServiceName = gs(tr, "service_name")
		case "http":
			p.Network = "h2"
			if hs := gl(tr, "host"); len(hs) > 0 {
				p.Host = hs[0]
			}
		}
	}
	if tl := gm(o, "tls"); tl != nil && gb(tl, "enabled") {
		p.Security = "tls"
		p.SNI, p.Insecure, p.ALPN = gs(tl, "server_name"), gb(tl, "insecure"), gl(tl, "alpn")
		if u := gm(tl, "utls"); u != nil {
			p.Fingerprint = gs(u, "fingerprint")
		}
		if pins := gl(tl, "certificate_public_key_sha256"); len(pins) > 0 {
			p.PinSPKI = pins[0]
		}
		if r := gm(tl, "reality"); r != nil && gb(r, "enabled") {
			p.Security, p.PublicKey, p.ShortID = "reality", gs(r, "public_key"), gs(r, "short_id")
		}
	}
	return p
}

func profileFromXrayOutbound(o jm) *Profile {
	proto := gs(o, "protocol")
	st := gm(o, "settings")
	ss := gm(o, "streamSettings")
	p := &Profile{Name: gs(o, "tag")}
	server := func(list string) jm {
		if arr, ok := st[list].([]any); ok && len(arr) > 0 {
			m, _ := arr[0].(map[string]any)
			return m
		}
		return nil
	}
	user := func(s jm) jm {
		if arr, ok := s["users"].([]any); ok && len(arr) > 0 {
			m, _ := arr[0].(map[string]any)
			return m
		}
		return nil
	}
	switch proto {
	case "vless", "vmess":
		s := server("vnext")
		if s == nil {
			// Xray v26 flat form: settings.address/port/id
			s = st
		}
		p.Server, p.Port = gs(s, "address"), gi(s, "port")
		u := user(s)
		if u == nil {
			u = s
		}
		p.UUID = gs(u, "id")
		if proto == "vless" {
			p.Type, p.Flow, p.Encryption = TypeVLESS, gs(u, "flow"), gs(u, "encryption")
		} else {
			p.Type, p.VMessSecurity = TypeVMess, gs(u, "security")
		}
	case "trojan", "shadowsocks":
		s := server("servers")
		if s == nil {
			s = st
		}
		p.Server, p.Port, p.Password = gs(s, "address"), gi(s, "port"), gs(s, "password")
		if proto == "trojan" {
			p.Type = TypeTrojan
		} else {
			p.Type, p.Method = TypeShadowsocks, gs(s, "method")
		}
	case "socks", "http":
		s := server("servers")
		if s == nil {
			s = st
		}
		p.Server, p.Port = gs(s, "address"), gi(s, "port")
		if u := user(s); u != nil {
			p.Username, p.Password = gs(u, "user"), gs(u, "pass")
		} else if gs(s, "user") != "" {
			// Flat Xray form: settings {address, port, user, pass}.
			p.Username, p.Password = gs(s, "user"), gs(s, "pass")
		}
		if proto == "socks" {
			p.Type = TypeSOCKS
		} else {
			p.Type = TypeHTTP
		}
	case "hysteria":
		p.Type, p.Server, p.Port = TypeHysteria2, gs(st, "address"), gi(st, "port")
		if hs := gm(ss, "hysteriaSettings"); hs != nil {
			p.Password = gs(hs, "auth")
		}
		if fm := gm(ss, "finalmask"); fm != nil {
			if arr, ok := fm["udp"].([]any); ok {
				for _, x := range arr {
					if xm, ok := x.(map[string]any); ok && gs(xm, "type") == "salamander" {
						p.ObfsPassword = gs(gm(xm, "settings"), "password")
					}
				}
			}
		}
	case "masque":
		p.Type, p.Server, p.Port = TypeMasque, gs(st, "address"), gi(st, "port")
		if ms := gm(ss, "masqueSettings"); ms != nil {
			p.MasquePath, p.Username, p.Password = gs(ms, "path"), gs(ms, "user"), gs(ms, "pass")
		}
	case "wireguard":
		w := &WGConfig{PrivateKey: gs(st, "secretKey"), Address: gl(st, "address"), MTU: gi(st, "mtu")}
		if arr, ok := st["peers"].([]any); ok && len(arr) > 0 {
			pm, _ := arr[0].(map[string]any)
			w.PeerPublicKey, w.PresharedKey, w.Keepalive = gs(pm, "publicKey"), gs(pm, "preSharedKey"), gi(pm, "keepAlive")
			h, port, err := splitHostPort(gs(pm, "endpoint"))
			if err != nil {
				return nil
			}
			p.Server, p.Port = h, port
		}
		if w.PrivateKey == "" || w.PeerPublicKey == "" {
			return nil
		}
		fixWGAddresses(w)
		p.Type, p.WG = TypeWireGuard, w
		return p
	default:
		return nil
	}
	if p.Server == "" || p.Port == 0 {
		return nil
	}
	if ss != nil {
		p.Network = gs(ss, "network")
		switch p.Network {
		case "ws":
			w := gm(ss, "wsSettings")
			p.Path, p.Host = gs(w, "path"), gs(w, "host")
			if p.Host == "" {
				if h := gm(w, "headers"); h != nil {
					p.Host = gs(h, "Host")
				}
			}
		case "httpupgrade":
			w := gm(ss, "httpupgradeSettings")
			p.Path, p.Host = gs(w, "path"), gs(w, "host")
		case "xhttp", "splithttp":
			w := gm(ss, "xhttpSettings")
			if w == nil {
				w = gm(ss, "splithttpSettings")
			}
			p.Network = "xhttp"
			p.Path, p.Host, p.Mode = gs(w, "path"), gs(w, "host"), gs(w, "mode")
			if ex := gm(w, "extra"); ex != nil {
				b, _ := json.Marshal(ex)
				p.XHTTPExtra = string(b)
			}
		case "grpc":
			w := gm(ss, "grpcSettings")
			p.ServiceName, p.Authority = gs(w, "serviceName"), gs(w, "authority")
			if gb(w, "multiMode") {
				p.Mode = "multi"
			} else {
				p.Mode = "gun"
			}
		case "kcp", "mkcp":
			p.Network = "kcp"
			w := gm(ss, "kcpSettings")
			k := &KCPConfig{MTU: gi(w, "mtu"), TTI: gi(w, "tti"), UplinkCapacity: gi(w, "uplinkCapacity"),
				DownlinkCapacity: gi(w, "downlinkCapacity"), CwndMultiplier: gi(w, "cwndMultiplier"), MaxSendingWindow: gi(w, "maxSendingWindow")}
			if *k != (KCPConfig{}) {
				p.KCP = k
			}
			if h := gm(w, "header"); h != nil && gs(h, "type") != "none" {
				p.HeaderType = gs(h, "type")
				if p.HeaderType == "dns" {
					p.Host = gs(h, "domain")
				}
			}
			p.Seed = gs(w, "seed")
		case "xdrive":
			if w := gm(ss, "xdriveSettings"); w != nil {
				b, _ := json.Marshal(w)
				p.XDrive = string(b)
			}
		case "hysteria", "masque":
			p.Network = ""
		case "", "raw", "tcp":
			p.Network = "tcp"
			w := gm(ss, "tcpSettings")
			if w == nil {
				w = gm(ss, "rawSettings")
			}
			if h := gm(w, "header"); h != nil && gs(h, "type") == "http" {
				p.HeaderType = "http"
				req := gm(h, "request")
				p.Path = strings.Join(gl(req, "path"), ",")
				if hd := gm(req, "headers"); hd != nil {
					p.Host = strings.Join(gl(hd, "Host"), ",")
				}
			}
		}
		if w := gm(ss, "xhttpSettings"); w != nil && p.Network == "xhttp" {
			p.XPaddingBytes = gs(w, "xPaddingBytes")
		}
		raw := func(k string) string {
			if v := gm(ss, k); len(v) > 0 {
				b, _ := json.Marshal(v)
				return string(b)
			}
			return ""
		}
		if p.Type != TypeHysteria2 {
			p.FinalMask = raw("finalmask")
		}
		p.Sockopt = raw("sockopt")
		switch gs(ss, "security") {
		case "tls":
			t := gm(ss, "tlsSettings")
			p.Security = "tls"
			p.SNI, p.ALPN, p.Fingerprint = gs(t, "serverName"), gl(t, "alpn"), gs(t, "fingerprint")
			if pin := gs(t, "pinnedPeerCertSha256"); pin != "" {
				p.PinSHA256 = strings.ToLower(strings.ReplaceAll(strings.Split(pin, ",")[0], ":", ""))
				p.Insecure = true
			}
			if gb(t, "allowInsecure") {
				p.Insecure = true
			}
			p.ECH, p.VerifyName = gs(t, "echConfigList"), gs(t, "verifyPeerCertByName")
			p.CipherSuites = gs(t, "cipherSuites")
		case "reality":
			r := gm(ss, "realitySettings")
			p.Security = "reality"
			p.SNI, p.Fingerprint, p.PublicKey, p.ShortID, p.SpiderX = gs(r, "serverName"), gs(r, "fingerprint"), gs(r, "publicKey"), gs(r, "shortId"), gs(r, "spiderX")
			if p.PublicKey == "" {
				p.PublicKey = gs(r, "password")
			}
			p.PQV = gs(r, "mldsa65Verify")
		}
	}
	if m := gm(o, "mux"); m != nil && gb(m, "enabled") && p.Network != "xdrive" {
		b, _ := json.Marshal(m)
		p.Mux = string(b)
	}
	return p
}

// ---------- Clash / Mihomo ----------

func parseClash(b []byte) ([]*Profile, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	var out []*Profile
	for _, c := range doc.Proxies {
		if p := profileFromClash(c); p != nil {
			p.normalize()
			out = append(out, p)
		}
	}
	return out, nil
}

func yi(m jm, k string) int {
	switch v := m[k].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
func ys(m jm, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case int, int64, uint64, float64:
		return fmt.Sprint(v)
	}
	return ""
}
func yb(m jm, k string) bool { b, _ := m[k].(bool); return b }

func profileFromClash(c jm) *Profile {
	p := &Profile{Name: ys(c, "name"), Server: ys(c, "server"), Port: yi(c, "port")}
	switch ys(c, "type") {
	case "vless":
		p.Type, p.UUID, p.Flow = TypeVLESS, ys(c, "uuid"), ys(c, "flow")
	case "vmess":
		p.Type, p.UUID, p.AlterID, p.VMessSecurity = TypeVMess, ys(c, "uuid"), yi(c, "alterId"), ys(c, "cipher")
	case "trojan":
		p.Type, p.Password, p.Security = TypeTrojan, ys(c, "password"), "tls"
	case "ss":
		p.Type, p.Method, p.Password = TypeShadowsocks, ys(c, "cipher"), ys(c, "password")
	case "hysteria2":
		p.Type, p.Password, p.Security = TypeHysteria2, ys(c, "password"), "tls"
		if ys(c, "obfs") == "salamander" {
			p.ObfsPassword = ys(c, "obfs-password")
		}
	case "tuic":
		p.Type, p.UUID, p.Password, p.Congestion, p.Security = TypeTUIC, ys(c, "uuid"), ys(c, "password"), ys(c, "congestion-controller"), "tls"
		p.UDPRelayMode = ys(c, "udp-relay-mode")
	case "http":
		p.Type, p.Username, p.Password = TypeHTTP, ys(c, "username"), ys(c, "password")
		if yb(c, "tls") {
			p.Security = "tls"
		}
	case "socks5":
		p.Type, p.Username, p.Password = TypeSOCKS, ys(c, "username"), ys(c, "password")
	case "wireguard":
		w := &WGConfig{PrivateKey: ys(c, "private-key"), PeerPublicKey: ys(c, "public-key"), PresharedKey: ys(c, "pre-shared-key"), MTU: yi(c, "mtu")}
		if ip := ys(c, "ip"); ip != "" {
			w.Address = append(w.Address, ip)
		}
		if ip := ys(c, "ipv6"); ip != "" {
			w.Address = append(w.Address, ip)
		}
		if w.PrivateKey == "" || w.PeerPublicKey == "" || len(w.Address) == 0 {
			return nil
		}
		fixWGAddresses(w)
		p.Type, p.WG = TypeWireGuard, w
		if ao, ok := c["amnezia-wg-option"].(map[string]any); ok {
			a := &AWG{Jc: yi(ao, "jc"), Jmin: yi(ao, "jmin"), Jmax: yi(ao, "jmax"), S1: yi(ao, "s1"), S2: yi(ao, "s2"),
				S3: yi(ao, "s3"), S4: yi(ao, "s4"), H1: ys(ao, "h1"), H2: ys(ao, "h2"), H3: ys(ao, "h3"), H4: ys(ao, "h4"),
				I1: ys(ao, "i1"), I2: ys(ao, "i2"), I3: ys(ao, "i3"), I4: ys(ao, "i4"), I5: ys(ao, "i5")}
			if !awgIsPlainWG(a) {
				p.Type, w.AWG = TypeAmneziaWG, a
			}
		}
		return p
	default:
		return nil
	}
	if p.Server == "" || p.Port == 0 {
		return nil
	}
	if yb(c, "tls") && p.Security == "" {
		p.Security = "tls"
	}
	p.SNI = ys(c, "servername")
	if p.SNI == "" {
		p.SNI = ys(c, "sni")
	}
	p.Insecure = yb(c, "skip-cert-verify")
	p.Fingerprint = ys(c, "client-fingerprint")
	if ro, ok := c["reality-opts"].(map[string]any); ok {
		p.Security, p.PublicKey, p.ShortID = "reality", ys(ro, "public-key"), ys(ro, "short-id")
	}
	if al, ok := c["alpn"].([]any); ok {
		for _, x := range al {
			p.ALPN = append(p.ALPN, fmt.Sprint(x))
		}
	}
	p.Network = ys(c, "network")
	switch p.Network {
	case "ws":
		if wo, ok := c["ws-opts"].(map[string]any); ok {
			p.Path = ys(wo, "path")
			if h, ok := wo["headers"].(map[string]any); ok {
				p.Host = ys(h, "Host")
			}
		}
	case "grpc":
		if g, ok := c["grpc-opts"].(map[string]any); ok {
			p.ServiceName = ys(g, "grpc-service-name")
		}
	}
	return p
}
