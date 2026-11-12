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

// ToLink renders p back to a share link that ParseLink reads into an
// equivalent profile. AddressOverride is NOT baked in
// (they are local settings); ID / SubID are not part of links.
//
// Supported: vless, vmess (v2rayN base64 JSON), trojan, shadowsocks
// (SIP002), hysteria2, tuic, wireguard / amneziawg (wireguard:// with AWG
// query keys), http(s), socks5 and mtproto (tg://proxy). MASQUE has no share-link
// format and returns an error.
func ToLink(p *Profile) (string, error) {
	if p.Network == "xdrive" {
		// As in common panels: a link would print the storage secrets into every QR.
		return "", errors.New("XDRIVE has no share-link format (its storage secrets would be exposed); share the JSON subscription instead")
	}
	switch p.Type {
	case TypeVLESS, TypeTrojan:
		return v2StdLink(p), nil
	case TypeVMess:
		return vmessLink(p), nil
	case TypeShadowsocks:
		return ssLink(p), nil
	case TypeHysteria2:
		return hy2Link(p), nil
	case TypeTUIC:
		return tuicLink(p), nil
	case TypeWireGuard, TypeAmneziaWG:
		return wgLink(p)
	case TypeHTTP:
		return httpLink(p), nil
	case TypeSOCKS:
		return socksLink(p), nil
	case TypeMTProto:
		q := url.Values{}
		q.Set("server", p.Server)
		q.Set("port", strconv.Itoa(p.Port))
		q.Set("secret", p.Secret)
		return "tg://proxy?" + q.Encode(), nil
	}
	return "", fmt.Errorf("%s profiles have no share-link format", p.Type)
}

func hostPort(h string, port int) string { return net.JoinHostPort(h, strconv.Itoa(port)) }

func frag(name string) string {
	if name == "" {
		return ""
	}
	return "#" + url.PathEscape(name)
}

func setNZ(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// streamQuery is the inverse of applyStreamQuery.
func streamQuery(p *Profile) url.Values {
	q := url.Values{}
	nw := p.Network
	if nw == "" {
		nw = "tcp"
	}
	if nw == "h2" {
		nw = "http"
	}
	q.Set("type", nw)
	setNZ(q, "path", p.Path)
	setNZ(q, "host", p.Host)
	if p.Network == "grpc" {
		setNZ(q, "serviceName", p.ServiceName)
	}
	setNZ(q, "mode", p.Mode)
	setNZ(q, "headerType", p.HeaderType)
	if p.Network == "xhttp" || p.Network == "splithttp" {
		setNZ(q, "extra", p.XHTTPExtra)
		setNZ(q, "x_padding_bytes", p.XPaddingBytes)
	}
	if p.Network == "grpc" {
		setNZ(q, "authority", p.Authority)
	}
	if p.Network == "kcp" {
		setNZ(q, "seed", p.Seed)
		if p.KCP != nil {
			if p.KCP.MTU > 0 {
				q.Set("mtu", strconv.Itoa(p.KCP.MTU))
			}
			if p.KCP.TTI > 0 {
				q.Set("tti", strconv.Itoa(p.KCP.TTI))
			}
		}
	}
	setNZ(q, "fm", p.FinalMask)
	setNZ(q, "ech", p.ECH)
	setNZ(q, "vcn", p.VerifyName)
	setNZ(q, "cs", p.CipherSuites)
	setNZ(q, "pqv", p.PQV)
	sec := p.Security
	if sec == "" {
		sec = "none"
	}
	q.Set("security", sec)
	setNZ(q, "sni", p.SNI)
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	setNZ(q, "fp", p.Fingerprint)
	if p.Insecure {
		q.Set("allowInsecure", "1")
	}
	setNZ(q, "pcs", p.PinSHA256)
	setNZ(q, "pbk", p.PublicKey)
	setNZ(q, "sid", p.ShortID)
	setNZ(q, "spx", p.SpiderX)
	return q
}

func v2StdLink(p *Profile) string {
	q := streamQuery(p)
	cred := p.Password
	if p.Type == TypeVLESS {
		cred = p.UUID
		setNZ(q, "flow", p.Flow)
		enc := p.Encryption
		if enc == "" || strings.EqualFold(enc, "none") {
			enc = "none"
		}
		q.Set("encryption", enc)
	}
	u := url.URL{Scheme: p.Type, User: url.User(cred), Host: hostPort(p.Server, p.Port), RawQuery: q.Encode()}
	return u.String() + frag(p.Name)
}

func vmessLink(p *Profile) string {
	m := map[string]string{"v": "2", "ps": p.Name, "add": p.Server, "port": strconv.Itoa(p.Port), "id": p.UUID,
		"aid": strconv.Itoa(p.AlterID)}
	if p.VMessSecurity != "" {
		m["scy"] = p.VMessSecurity
	}
	nw := p.Network
	if nw == "" {
		nw = "tcp"
	}
	if nw == "h2" {
		nw = "http"
	}
	m["net"] = nw
	if p.FinalMask != "" {
		m["fm"] = p.FinalMask
	}
	switch p.Network {
	case "grpc":
		m["path"], m["type"] = p.ServiceName, firstNonEmpty(p.Mode, "gun")
		if p.Authority != "" {
			m["authority"] = p.Authority
		}
	case "kcp":
		m["path"] = p.Seed
		if p.HeaderType != "" {
			// An absent "type" means none; writing "none" for an empty
			// header would not round-trip (profiles made in the editor).
			m["type"] = p.HeaderType
		}
		if p.KCP != nil && p.KCP.MTU > 0 {
			m["mtu"] = strconv.Itoa(p.KCP.MTU)
		}
		if p.KCP != nil && p.KCP.TTI > 0 {
			m["tti"] = strconv.Itoa(p.KCP.TTI)
		}
	case "xhttp", "splithttp":
		m["path"], m["host"], m["type"] = p.Path, p.Host, firstNonEmpty(p.Mode, "none")
		if p.XPaddingBytes != "" {
			m["x_padding_bytes"] = p.XPaddingBytes
		}
	default:
		m["path"], m["host"] = p.Path, p.Host
		if p.HeaderType != "" {
			m["type"] = p.HeaderType
		}
	}
	switch p.Security {
	case "tls":
		m["tls"], m["sni"], m["alpn"], m["fp"] = "tls", p.SNI, strings.Join(p.ALPN, ","), p.Fingerprint
		if p.Insecure {
			m["allowInsecure"] = "1"
		}
		if p.PinSHA256 != "" {
			m["pcs"] = p.PinSHA256
		}
		for k, v := range map[string]string{"ech": p.ECH, "vcn": p.VerifyName, "cs": p.CipherSuites} {
			if v != "" {
				m[k] = v
			}
		}
	case "reality":
		m["tls"], m["sni"], m["fp"], m["pbk"], m["sid"], m["spx"] = "reality", p.SNI, p.Fingerprint, p.PublicKey, p.ShortID, p.SpiderX
	default:
		m["tls"] = ""
	}
	b, _ := json.Marshal(m)
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

func ssLink(p *Profile) string {
	ui := base64.RawURLEncoding.EncodeToString([]byte(p.Method + ":" + p.Password))
	s := "ss://" + ui + "@" + hostPort(p.Server, p.Port)
	q := url.Values{}
	if (p.Network != "" && p.Network != "tcp") || (p.Security != "" && p.Security != "none") || p.FinalMask != "" || p.HeaderType != "" {
		q = streamQuery(p)
	}
	if p.Plugin != "" {
		pl := p.Plugin
		if p.PluginOpts != "" {
			pl += ";" + p.PluginOpts
		}
		q.Set("plugin", pl)
	}
	if len(q) > 0 {
		s += "/?" + q.Encode()
	}
	return s + frag(p.Name)
}

func hy2Link(p *Profile) string {
	q := url.Values{}
	setNZ(q, "sni", p.SNI)
	if p.Insecure {
		q.Set("insecure", "1")
	}
	setNZ(q, "pinSHA256", p.PinSHA256)
	if p.ObfsPassword != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", p.ObfsPassword)
	}
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	if p.UpMbps > 0 {
		q.Set("upmbps", strconv.Itoa(p.UpMbps))
	}
	if p.DownMbps > 0 {
		q.Set("downmbps", strconv.Itoa(p.DownMbps))
	}
	host := hostPort(p.Server, p.Port)
	if p.Ports != "" {
		host = net.JoinHostPort(p.Server, p.Ports)
	}
	u := url.URL{Scheme: "hysteria2", Host: host, RawQuery: q.Encode()}
	if p.Password != "" {
		u.User = url.User(p.Password)
	}
	return u.String() + frag(p.Name)
}

func tuicLink(p *Profile) string {
	q := url.Values{}
	setNZ(q, "congestion_control", p.Congestion)
	setNZ(q, "udp_relay_mode", p.UDPRelayMode)
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	setNZ(q, "sni", p.SNI)
	if p.Insecure {
		q.Set("allow_insecure", "1")
	}
	setNZ(q, "pinSHA256", p.PinSHA256)
	u := url.URL{Scheme: "tuic", User: url.UserPassword(p.UUID, p.Password), Host: hostPort(p.Server, p.Port), RawQuery: q.Encode()}
	return u.String() + frag(p.Name)
}

func wgLink(p *Profile) (string, error) {
	w := p.WG
	if w == nil {
		return "", errors.New("wireguard: missing settings")
	}
	q := url.Values{}
	q.Set("publickey", w.PeerPublicKey)
	setNZ(q, "presharedkey", w.PresharedKey)
	if len(w.Address) > 0 {
		q.Set("address", strings.Join(w.Address, ","))
	}
	if w.MTU > 0 {
		q.Set("mtu", strconv.Itoa(w.MTU))
	}
	if w.Keepalive > 0 {
		q.Set("keepalive", strconv.Itoa(w.Keepalive))
	}
	if len(w.Reserved) > 0 {
		rs := make([]string, len(w.Reserved))
		for i, r := range w.Reserved {
			rs[i] = strconv.Itoa(r)
		}
		q.Set("reserved", strings.Join(rs, ","))
	}
	if p.Type == TypeAmneziaWG {
		a := w.AWG
		if a == nil {
			a = &AWG{}
		}
		seti := func(k string, v int) {
			if v != 0 {
				q.Set(k, strconv.Itoa(v))
			}
		}
		seti("jc", a.Jc)
		seti("jmin", a.Jmin)
		seti("jmax", a.Jmax)
		seti("s1", a.S1)
		seti("s2", a.S2)
		seti("s3", a.S3)
		seti("s4", a.S4)
		for k, v := range map[string]string{"h1": a.H1, "h2": a.H2, "h3": a.H3, "h4": a.H4,
			"i1": a.I1, "i2": a.I2, "i3": a.I3, "i4": a.I4, "i5": a.I5} {
			setNZ(q, k, v)
		}
		if !(q.Has("jc") || q.Has("jmin") || q.Has("jmax") || q.Has("s1") || q.Has("s2") || q.Has("s3") || q.Has("s4") ||
			q.Has("h1") || q.Has("h2") || q.Has("h3") || q.Has("h4") || q.Has("i1")) {
			// Keep the AmneziaWG type on re-import even when every value is zero.
			q.Set("jc", "0")
		}
	}
	u := url.URL{Scheme: "wireguard", User: url.User(w.PrivateKey), Host: hostPort(p.Server, p.Port), RawQuery: q.Encode()}
	return u.String() + frag(p.Name), nil
}

func httpLink(p *Profile) string {
	u := url.URL{Scheme: "http", Host: hostPort(p.Server, p.Port)}
	if p.Security == "tls" {
		u.Scheme = "https"
		q := url.Values{}
		setNZ(q, "sni", p.SNI)
		if p.Insecure {
			q.Set("insecure", "1")
		}
		u.RawQuery = q.Encode()
	}
	if p.Username != "" || p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	name := p.Name
	if name == "" {
		name = p.Server
	}
	return u.String() + frag(name)
}

// socksLink renders socks5://user:pass@host:port[?security=tls&sni=…]#name.
func socksLink(p *Profile) string {
	u := url.URL{Scheme: "socks5", Host: hostPort(p.Server, p.Port)}
	if p.Username != "" || p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	if p.Security == "tls" {
		q := url.Values{}
		q.Set("security", "tls")
		setNZ(q, "sni", p.SNI)
		if p.Insecure {
			q.Set("allowInsecure", "1")
		}
		u.RawQuery = q.Encode()
	}
	return u.String() + frag(p.Name)
}
