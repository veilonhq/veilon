package veiloncore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

var errUnsupported = errors.New("unsupported link")

// decodeB64 decodes standard / URL-safe base64 with or without padding.
func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\r", "", "\n", "", " ", "").Replace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func fragmentName(u *url.URL) string {
	if u.Fragment != "" {
		return u.Fragment
	}
	if u.RawFragment != "" {
		if s, err := url.PathUnescape(u.RawFragment); err == nil {
			return s
		}
	}
	return ""
}

func splitHostPort(hostport string) (string, int, error) {
	h, ps, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", 0, err
	}
	p, err := strconv.Atoi(ps)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, fmt.Errorf("invalid port %q", ps)
	}
	return strings.Trim(h, "[]"), p, nil
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// ParseLink parses one share link.
func ParseLink(link string) (*Profile, error) {
	link = strings.TrimSpace(link)
	i := strings.Index(link, "://")
	if i <= 0 {
		return nil, errUnsupported
	}
	scheme := strings.ToLower(link[:i])
	var (
		p   *Profile
		err error
	)
	switch scheme {
	case "vless":
		p, err = parseV2Std(link, TypeVLESS)
	case "trojan", "trojan-go":
		p, err = parseV2Std(link, TypeTrojan)
	case "vmess":
		p, err = parseVMess(link)
	case "ss":
		p, err = parseSS(link)
	case "hysteria2", "hy2":
		p, err = parseHysteria2(link)
	case "tuic":
		p, err = parseTUIC(link)
	case "wireguard", "wg":
		p, err = parseWGLink(link)
	case "http", "https":
		p, err = parseHTTPLink(link)
	case "socks", "socks5", "socks5h":
		p, err = parseSOCKSLink(link)
	case "naive+https", "naive+quic", "naive":
		// NaiveProxy profiles were removed in 1.0.4.
		return nil, ErrTypeRemoved
	case "tg":
		p, err = parseTG(link)
	case "slipnet", "slipgate", "ssh":
		// SlipNet / SlipGate protocols (SSH, StunTLS, DNS tunnels, ...)
		// were removed in 1.0.4.
		return nil, ErrSlipnetRemoved
	case "vpn":
		var ps []*Profile
		if ps, err = ParseAmneziaVPN(link); err == nil {
			p = ps[0]
		}
	default:
		return nil, errUnsupported
	}
	if err != nil {
		return nil, err
	}
	p.Link = link
	p.normalize()
	p.setWarnings()
	return p, nil
}

// parseV2Std parses vless:// and trojan:// (and vmess:// in the std form).
func parseV2Std(link, typ string) (*Profile, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	if u.User == nil {
		return nil, errors.New("missing credentials")
	}
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	p := &Profile{Type: typ, Server: host, Port: port, Name: fragmentName(u)}
	cred := u.User.Username()
	if pw, ok := u.User.Password(); ok {
		cred += ":" + pw
	}
	switch typ {
	case TypeVLESS, TypeVMess:
		p.UUID = cred
	default:
		p.Password = cred
	}
	q := u.Query()
	applyStreamQuery(p, q)
	if typ == TypeVLESS {
		p.Flow = q.Get("flow")
		p.Encryption = q.Get("encryption")
		if strings.EqualFold(p.Encryption, "none") {
			p.Encryption = ""
		}
	}
	if typ == TypeTrojan && p.Security == "none" && q.Get("security") == "" {
		p.Security = "tls" // trojan defaults to TLS
	}
	return p, nil
}

// compactJSON returns s re-encoded compactly when it is a JSON object,
// otherwise "".
func compactJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil || len(v) == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func applyStreamQuery(p *Profile, q url.Values) {
	p.Network = strings.ToLower(q.Get("type"))
	switch p.Network {
	case "", "raw":
		p.Network = "tcp"
	case "http": // "type=http" = h2 transport
		p.Network = "h2"
	case "mkcp":
		p.Network = "kcp"
	case "splithttp":
		p.Network = "xhttp"
	}
	p.Path = q.Get("path")
	p.Host = q.Get("host")
	p.ServiceName = q.Get("serviceName")
	p.Mode = q.Get("mode")
	p.HeaderType = q.Get("headerType")
	if p.Network == "grpc" && p.ServiceName == "" {
		p.ServiceName = p.Path
	}
	if p.Network == "xhttp" {
		p.XHTTPExtra = q.Get("extra")
		p.XPaddingBytes = q.Get("x_padding_bytes")
	}
	if p.Network == "grpc" {
		p.Authority = q.Get("authority")
	}
	if p.Network == "kcp" {
		p.Seed = q.Get("seed")
		mtu, _ := strconv.Atoi(q.Get("mtu"))
		tti, _ := strconv.Atoi(q.Get("tti"))
		if mtu > 0 || tti > 0 {
			p.KCP = &KCPConfig{MTU: mtu, TTI: tti}
		}
	}
	p.FinalMask = compactJSON(q.Get("fm"))
	p.ECH, p.VerifyName, p.CipherSuites, p.PQV = q.Get("ech"), q.Get("vcn"), q.Get("cs"), q.Get("pqv")
	p.Security = strings.ToLower(q.Get("security"))
	if p.Security == "" {
		p.Security = "none"
	}
	if p.Security == "xtls" {
		p.Security = "tls"
	}
	p.SNI = q.Get("sni")
	if p.SNI == "" {
		p.SNI = q.Get("peer")
	}
	p.ALPN = splitList(q.Get("alpn"))
	p.Fingerprint = q.Get("fp")
	p.Insecure = truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure"))
	p.PinSHA256 = strings.ToLower(strings.ReplaceAll(q.Get("pcs"), ":", ""))
	p.PublicKey = q.Get("pbk")
	p.ShortID = q.Get("sid")
	p.SpiderX = q.Get("spx")
}

func parseVMess(link string) (*Profile, error) {
	body := strings.TrimPrefix(link[len("vmess://"):], "")
	if strings.Contains(body, "@") {
		return parseV2Std(link, TypeVMess)
	}
	frag := ""
	if i := strings.Index(body, "#"); i >= 0 {
		frag, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	raw, err := decodeB64(body)
	if err != nil {
		return nil, fmt.Errorf("vmess: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("vmess: %w", err)
	}
	str := func(k string) string {
		switch v := m[k].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatInt(int64(v), 10)
		case bool:
			if v {
				return "1"
			}
		}
		return ""
	}
	port, err := strconv.Atoi(str("port"))
	if err != nil || port <= 0 {
		return nil, errors.New("vmess: invalid port")
	}
	p := &Profile{Type: TypeVMess, Name: str("ps"), Server: str("add"), Port: port, UUID: str("id"),
		VMessSecurity: str("scy"), Network: strings.ToLower(str("net"))}
	if frag != "" && p.Name == "" {
		p.Name = frag
	}
	p.AlterID, _ = strconv.Atoi(str("aid"))
	if p.Network == "" {
		p.Network = "tcp"
	}
	if p.Network == "http" {
		p.Network = "h2"
	}
	switch p.Network {
	case "raw":
		p.Network = "tcp"
	case "mkcp":
		p.Network = "kcp"
	case "splithttp":
		p.Network = "xhttp"
	}
	p.FinalMask = compactJSON(str("fm"))
	switch p.Network {
	case "grpc":
		p.ServiceName = str("path")
		p.Authority = str("authority")
		p.Mode = str("type")
		if p.Mode == "none" || p.Mode == "" {
			p.Mode = "gun"
		}
	case "kcp":
		// v2rayN: "type" = header type, "path" = seed.
		if t := str("type"); t != "" && t != "none" {
			p.HeaderType = t
		}
		p.Seed = firstNonEmpty(str("seed"), str("path"))
		mtu, _ := strconv.Atoi(str("mtu"))
		tti, _ := strconv.Atoi(str("tti"))
		if mtu > 0 || tti > 0 {
			p.KCP = &KCPConfig{MTU: mtu, TTI: tti}
		}
	case "xhttp":
		p.Path, p.Host = str("path"), str("host")
		p.XPaddingBytes = str("x_padding_bytes")
		if t := str("type"); t != "none" {
			p.Mode = t
		}
	default:
		p.Path, p.Host = str("path"), str("host")
		p.HeaderType = str("type")
	}
	if strings.EqualFold(str("tls"), "tls") {
		p.Security = "tls"
		p.SNI = str("sni")
		p.ALPN = splitList(str("alpn"))
		p.Fingerprint = str("fp")
		p.Insecure = truthy(str("allowInsecure")) || truthy(str("insecure"))
		p.PinSHA256 = strings.ToLower(strings.ReplaceAll(str("pcs"), ":", ""))
		p.ECH, p.VerifyName, p.CipherSuites = str("ech"), str("vcn"), str("cs")
	} else if strings.EqualFold(str("tls"), "reality") {
		p.Security = "reality"
		p.SNI, p.Fingerprint, p.PublicKey, p.ShortID, p.SpiderX = str("sni"), str("fp"), str("pbk"), str("sid"), str("spx")
	}
	return p, nil
}

func parseSS(link string) (*Profile, error) {
	body := link[len("ss://"):]
	name := ""
	if i := strings.Index(body, "#"); i >= 0 {
		name, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	query := ""
	if i := strings.Index(body, "?"); i >= 0 {
		query = body[i+1:]
		body = body[:i]
	}
	body = strings.TrimSuffix(body, "/")
	var method, password, hostport string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		ui := body[:at]
		hostport = body[at+1:]
		if strings.Contains(ui, ":") {
			// SIP022 plain userinfo: percent-encoded method:password.
			m, pw, _ := strings.Cut(ui, ":")
			method, _ = url.PathUnescape(m)
			password, _ = url.QueryUnescape(pw)
			// Password itself may contain ':' (2022 server:user key pair).
		} else {
			raw, err := decodeB64(func() string { s, _ := url.PathUnescape(ui); return s }())
			if err != nil {
				return nil, fmt.Errorf("ss: %w", err)
			}
			m, pw, ok := strings.Cut(string(raw), ":")
			if !ok {
				return nil, errors.New("ss: bad userinfo")
			}
			method, password = m, pw
		}
	} else {
		raw, err := decodeB64(body)
		if err != nil {
			return nil, fmt.Errorf("ss: %w", err)
		}
		s := string(raw)
		at := strings.LastIndex(s, "@")
		if at < 0 {
			return nil, errors.New("ss: bad legacy link")
		}
		m, pw, ok := strings.Cut(s[:at], ":")
		if !ok {
			return nil, errors.New("ss: bad legacy userinfo")
		}
		method, password, hostport = m, pw, s[at+1:]
	}
	host, port, err := splitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	p := &Profile{Type: TypeShadowsocks, Name: name, Server: host, Port: port, Method: strings.ToLower(method), Password: password, Network: "tcp"}
	if query != "" {
		q, _ := url.ParseQuery(query)
		if pl := q.Get("plugin"); pl != "" {
			name, opts, _ := strings.Cut(pl, ";")
			p.Plugin, p.PluginOpts = name, opts
		}
		// Panel / Xray SS links can carry a stream (type=ws, security=tls,
		// fm=…) like vless/trojan.
		if q.Get("type") != "" || q.Get("security") != "" || q.Get("fm") != "" {
			applyStreamQuery(p, q)
		}
	}
	return p, nil
}

func parseHysteria2(link string) (*Profile, error) {
	// net/url rejects port ranges ("host:20000-30000", "host:443,1000-2000")
	// in the authority; parse with a placeholder port and restore it.
	hopPorts := ""
	if i := strings.Index(link, "://"); i > 0 {
		rest := link[i+3:]
		end := strings.IndexAny(rest, "/?#")
		if end < 0 {
			end = len(rest)
		}
		auth := rest[:end]
		hp := auth
		if at := strings.LastIndex(auth, "@"); at >= 0 {
			hp = auth[at+1:]
		}
		if c := strings.LastIndex(hp, ":"); c >= 0 && !strings.HasSuffix(hp, "]") && strings.ContainsAny(hp[c+1:], "-,") {
			hopPorts = hp[c+1:]
			newAuth := auth[:len(auth)-len(hp)] + hp[:c] + ":1"
			link = link[:i+3] + newAuth + rest[end:]
		}
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	hostport := u.Host
	if hopPorts != "" {
		hostport = u.Hostname()
		if strings.Contains(hostport, ":") {
			hostport = "[" + hostport + "]"
		}
		hostport += ":" + hopPorts
	}
	p := &Profile{Type: TypeHysteria2, Name: fragmentName(u)}
	q := u.Query()
	// Port hopping: host:20000-30000 or host:443,1000-2000.
	if h, ps, err := net.SplitHostPort(hostport); err == nil && strings.ContainsAny(ps, "-,") {
		p.Server = strings.Trim(h, "[]")
		p.Ports = ps
		first := strings.FieldsFunc(ps, func(r rune) bool { return r == '-' || r == ',' })[0]
		p.Port, _ = strconv.Atoi(first)
	} else {
		host, port, err := splitHostPort(hostport)
		if err != nil {
			if !strings.Contains(hostport, ":") {
				host, port, err = strings.Trim(hostport, "[]"), 443, nil
			} else {
				return nil, err
			}
		}
		p.Server, p.Port = host, port
	}
	if mp := q.Get("mport"); mp != "" {
		p.Ports = mp
	}
	if u.User != nil {
		p.Password = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			p.Password += ":" + pw
		}
	}
	if a := q.Get("auth"); a != "" && p.Password == "" {
		p.Password = a
	}
	p.Security = "tls"
	p.SNI = q.Get("sni")
	p.Insecure = truthy(q.Get("insecure"))
	p.PinSHA256 = strings.ToLower(strings.ReplaceAll(q.Get("pinSHA256"), ":", ""))
	if q.Get("obfs") == "salamander" {
		p.ObfsPassword = q.Get("obfs-password")
	}
	p.ALPN = splitList(q.Get("alpn"))
	p.UpMbps, _ = strconv.Atoi(q.Get("upmbps"))
	p.DownMbps, _ = strconv.Atoi(q.Get("downmbps"))
	return p, nil
}

func parseTUIC(link string) (*Profile, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	p := &Profile{Type: TypeTUIC, Name: fragmentName(u), Server: host, Port: port, Security: "tls"}
	if u.User != nil {
		p.UUID = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	q := u.Query()
	p.Congestion = q.Get("congestion_control")
	if p.Congestion == "" {
		p.Congestion = q.Get("congestion")
	}
	p.UDPRelayMode = q.Get("udp_relay_mode")
	p.ALPN = splitList(q.Get("alpn"))
	p.SNI = q.Get("sni")
	p.Insecure = truthy(q.Get("allow_insecure")) || truthy(q.Get("insecure")) || truthy(q.Get("allowInsecure"))
	p.PinSHA256 = strings.ToLower(strings.ReplaceAll(q.Get("pinSHA256"), ":", ""))
	return p, nil
}

func parseWGLink(link string) (*Profile, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	w := &WGConfig{PeerPublicKey: q.Get("publickey"), PresharedKey: q.Get("presharedkey")}
	if u.User != nil {
		w.PrivateKey, _ = url.PathUnescape(u.User.Username())
	}
	if w.PrivateKey == "" {
		w.PrivateKey = q.Get("privatekey")
	}
	if w.PeerPublicKey == "" {
		w.PeerPublicKey = q.Get("peer_public_key")
	}
	w.Address = splitList(q.Get("address"))
	if len(w.Address) == 0 {
		w.Address = splitList(q.Get("ip"))
	}
	w.MTU, _ = strconv.Atoi(q.Get("mtu"))
	w.Keepalive, _ = strconv.Atoi(q.Get("keepalive"))
	if r := q.Get("reserved"); r != "" {
		for _, x := range splitList(r) {
			if n, err := strconv.Atoi(x); err == nil {
				w.Reserved = append(w.Reserved, n)
			}
		}
	}
	if w.PrivateKey == "" || w.PeerPublicKey == "" {
		return nil, errors.New("wireguard: missing keys")
	}
	p := &Profile{Type: TypeWireGuard, Name: fragmentName(u), Server: host, Port: port, WG: w}
	if a := parseAWGQuery(q); a != nil {
		p.Type = TypeAmneziaWG
		w.AWG = a
	}
	fixWGAddresses(w)
	return p, nil
}

func parseAWGQuery(q url.Values) *AWG {
	a := &AWG{}
	any := false
	geti := func(k string, dst *int) {
		if v := q.Get(k); v != "" {
			*dst, _ = strconv.Atoi(v)
			any = true
		}
	}
	gets := func(k string, dst *string) {
		if v := q.Get(k); v != "" {
			*dst = v
			any = true
		}
	}
	geti("jc", &a.Jc)
	geti("jmin", &a.Jmin)
	geti("jmax", &a.Jmax)
	geti("s1", &a.S1)
	geti("s2", &a.S2)
	geti("s3", &a.S3)
	geti("s4", &a.S4)
	gets("h1", &a.H1)
	gets("h2", &a.H2)
	gets("h3", &a.H3)
	gets("h4", &a.H4)
	gets("i1", &a.I1)
	gets("i2", &a.I2)
	gets("i3", &a.I3)
	gets("i4", &a.I4)
	gets("i5", &a.I5)
	if !any {
		return nil
	}
	return a
}

// fixWGAddresses adds a host prefix length to bare addresses.
func fixWGAddresses(w *WGConfig) {
	for i, a := range w.Address {
		if !strings.Contains(a, "/") {
			if strings.Contains(a, ":") {
				w.Address[i] = a + "/128"
			} else {
				w.Address[i] = a + "/32"
			}
		}
	}
}

func parseHTTPLink(link string) (*Profile, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		if u.Scheme == "https" {
			host, port, err = u.Hostname(), 443, nil
		} else {
			host, port, err = u.Hostname(), 80, nil
		}
		if host == "" {
			return nil, errors.New("http: missing host")
		}
	}
	p := &Profile{Type: TypeHTTP, Name: fragmentName(u), Server: host, Port: port}
	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	if u.Scheme == "https" {
		p.Security = "tls"
		q := u.Query()
		p.SNI = q.Get("sni")
		p.Insecure = truthy(q.Get("insecure")) || truthy(q.Get("allowInsecure"))
	}
	if p.Username == "" && p.Password == "" && fragmentName(u) == "" && u.Path != "" && u.Path != "/" {
		// A plain web URL (e.g. a subscription), not a proxy link.
		return nil, errUnsupported
	}
	return p, nil
}

func parseTG(link string) (*Profile, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(q.Get("port"))
	if q.Get("server") == "" || port == 0 || q.Get("secret") == "" {
		return nil, errors.New("mtproto: missing server/port/secret")
	}
	return &Profile{Type: TypeMTProto, Name: "MTProto " + q.Get("server"), Server: q.Get("server"), Port: port, Secret: q.Get("secret")}, nil
}

// parseSOCKSLink reads socks://, socks5:// and socks5h:// links:
// user:pass@host:port, the v2rayN form socks://base64(user:pass)@host:port,
// and optional ?security=tls (or tls=1) with sni / allowInsecure.
func parseSOCKSLink(link string) (*Profile, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	p := &Profile{Type: TypeSOCKS, Name: fragmentName(u), Server: host, Port: port}
	if u.User != nil {
		user := u.User.Username()
		pw, hasPw := u.User.Password()
		if !hasPw {
			if raw, err := decodeB64(func() string { s, _ := url.PathUnescape(user); return s }()); err == nil && strings.Contains(string(raw), ":") {
				user, pw, _ = strings.Cut(string(raw), ":")
			}
		}
		p.Username, p.Password = user, pw
	}
	q := u.Query()
	if strings.EqualFold(q.Get("security"), "tls") || truthy(q.Get("tls")) {
		p.Security = "tls"
		p.SNI = firstNonEmpty(q.Get("sni"), q.Get("peer"))
		p.Insecure = truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure"))
	}
	return p, nil
}
