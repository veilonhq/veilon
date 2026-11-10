package xray

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParseShareLink converts a client share link (vless, vmess, trojan, ss,
// socks, http, wireguard) into an Xray outbound object. tag is used as the
// outbound tag; empty means the link's name.
func ParseShareLink(link, tag string) (map[string]any, error) {
	link = strings.TrimSpace(link)
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return nil, terr("not a share link")
	}
	scheme = strings.ToLower(scheme)
	var out map[string]any
	var name string
	var err error
	switch scheme {
	case "vmess":
		out, name, err = parseVmess(link)
	case "vless", "trojan", "ss", "socks", "socks5", "http", "https", "wireguard", "wg":
		var u *url.URL
		u, err = url.Parse(link)
		if err != nil {
			return nil, terr("invalid link: %v", err)
		}
		name = u.Fragment
		switch scheme {
		case "vless":
			out, err = parseVless(u)
		case "trojan":
			out, err = parseTrojan(u)
		case "ss":
			out, err = parseSS(u, link)
		case "socks", "socks5", "http", "https":
			out, err = parseSocksHTTP(u, scheme)
		default:
			out, err = parseWireGuard(u)
		}
	default:
		return nil, terr("links of type %q are not supported", scheme)
	}
	if err != nil {
		return nil, err
	}
	if tag == "" {
		tag = sanitizeTag(name)
	}
	if tag == "" {
		tag = str(out, "protocol") + "-out"
	}
	out["tag"] = tag
	return out, nil
}

// sanitizeTag turns a link name into a valid outbound tag.
func sanitizeTag(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == '@':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-.")
}

func hostPortOf(u *url.URL) (string, int, error) {
	h := u.Hostname()
	p, err := strconv.Atoi(u.Port())
	if h == "" || err != nil || p <= 0 || p > 65535 {
		return "", 0, terr("the link has no valid host:port")
	}
	return h, p, nil
}

func splitList(s string) []any {
	var out []any
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// streamFromQuery builds streamSettings from the common share-link query.
func streamFromQuery(q url.Values, defSecurity string) map[string]any {
	network := q.Get("type")
	if network == "" || network == "tcp" {
		network = "raw"
	}
	if network == "h2" {
		network = "http"
	}
	ss := map[string]any{"network": network}
	switch network {
	case "ws":
		w := map[string]any{"path": orDefault(q.Get("path"), "/")}
		if h := q.Get("host"); h != "" {
			w["host"] = h
		}
		ss["wsSettings"] = w
	case "httpupgrade":
		w := map[string]any{"path": orDefault(q.Get("path"), "/")}
		if h := q.Get("host"); h != "" {
			w["host"] = h
		}
		ss["httpupgradeSettings"] = w
	case "xhttp", "splithttp":
		ss["network"] = "xhttp"
		w := map[string]any{"path": orDefault(q.Get("path"), "/")}
		if h := q.Get("host"); h != "" {
			w["host"] = h
		}
		if m := q.Get("mode"); m != "" {
			w["mode"] = m
		}
		ss["xhttpSettings"] = w
	case "grpc":
		g := map[string]any{"serviceName": q.Get("serviceName")}
		if q.Get("mode") == "multi" {
			g["multiMode"] = true
		}
		if a := q.Get("authority"); a != "" {
			g["authority"] = a
		}
		ss["grpcSettings"] = g
	case "raw":
		if q.Get("headerType") == "http" {
			req := map[string]any{"path": []any{orDefault(q.Get("path"), "/")}}
			if h := q.Get("host"); h != "" {
				req["headers"] = map[string]any{"Host": splitList(h)}
			}
			ss["rawSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": req}}
		}
	}
	sec := q.Get("security")
	if sec == "" {
		sec = defSecurity
	}
	switch sec {
	case "tls":
		t := map[string]any{}
		if v := q.Get("sni"); v != "" {
			t["serverName"] = v
		} else if v := q.Get("peer"); v != "" {
			t["serverName"] = v
		}
		if v := q.Get("fp"); v != "" {
			t["fingerprint"] = v
		}
		if v := q.Get("alpn"); v != "" {
			t["alpn"] = splitList(v)
		}
		if v := q.Get("allowInsecure"); v == "1" || v == "true" {
			t["allowInsecure"] = true
		}
		if v := q.Get("pcs"); v != "" {
			t["pinnedPeerCertificateChainSha256"] = []any{v}
		}
		ss["security"] = "tls"
		ss["tlsSettings"] = t
	case "reality":
		r := map[string]any{"publicKey": q.Get("pbk"), "fingerprint": orDefault(q.Get("fp"), "chrome")}
		if v := q.Get("sni"); v != "" {
			r["serverName"] = v
		}
		if v := q.Get("sid"); v != "" {
			r["shortId"] = v
		}
		if v := q.Get("spx"); v != "" {
			r["spiderX"] = v
		}
		ss["security"] = "reality"
		ss["realitySettings"] = r
	default:
		ss["security"] = "none"
	}
	return ss
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func parseVless(u *url.URL) (map[string]any, error) {
	h, p, err := hostPortOf(u)
	if err != nil {
		return nil, err
	}
	id := u.User.Username()
	if id == "" {
		return nil, terr("the vless link has no user id")
	}
	q := u.Query()
	user := map[string]any{"id": id, "encryption": orDefault(q.Get("encryption"), "none")}
	if f := q.Get("flow"); f != "" {
		user["flow"] = f
	}
	return map[string]any{"protocol": "vless",
		"settings":       map[string]any{"vnext": []any{map[string]any{"address": h, "port": p, "users": []any{user}}}},
		"streamSettings": streamFromQuery(q, "none")}, nil
}

func parseTrojan(u *url.URL) (map[string]any, error) {
	h, p, err := hostPortOf(u)
	if err != nil {
		return nil, err
	}
	pw := u.User.Username()
	if pw == "" {
		return nil, terr("the trojan link has no password")
	}
	return map[string]any{"protocol": "trojan",
		"settings":       map[string]any{"servers": []any{map[string]any{"address": h, "port": p, "password": pw}}},
		"streamSettings": streamFromQuery(u.Query(), "tls")}, nil
}

func b64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, terr("invalid base64")
}

func parseSS(u *url.URL, raw string) (map[string]any, error) {
	var method, pw, host string
	var port int
	if u.User != nil && u.Port() != "" {
		h, p, err := hostPortOf(u)
		if err != nil {
			return nil, err
		}
		host, port = h, p
		if pass, ok := u.User.Password(); ok {
			method, pw = u.User.Username(), pass // SIP022 plain form
		} else {
			dec, err := b64(u.User.Username())
			if err != nil {
				return nil, terr("the shadowsocks link has an invalid user part")
			}
			m, p, ok := strings.Cut(string(dec), ":")
			if !ok {
				return nil, terr("the shadowsocks link has an invalid user part")
			}
			method, pw = m, p
		}
	} else {
		// Legacy form: ss://base64(method:password@host:port)#name
		body := strings.TrimPrefix(raw, "ss://")
		body, _, _ = strings.Cut(body, "#")
		dec, err := b64(body)
		if err != nil {
			return nil, terr("invalid shadowsocks link")
		}
		cred, hp, ok := strings.Cut(string(dec), "@")
		if !ok {
			return nil, terr("invalid shadowsocks link")
		}
		m, p, _ := strings.Cut(cred, ":")
		method, pw = m, p
		h, ps, err := net.SplitHostPort(hp)
		if err != nil {
			return nil, terr("invalid shadowsocks link")
		}
		host = h
		port, _ = strconv.Atoi(ps)
	}
	if method == "" || pw == "" || port <= 0 {
		return nil, terr("the shadowsocks link lacks method, password or port")
	}
	return map[string]any{"protocol": "shadowsocks",
		"settings": map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "method": method, "password": pw}}}}, nil
}

func parseSocksHTTP(u *url.URL, scheme string) (map[string]any, error) {
	h, p, err := hostPortOf(u)
	if err != nil {
		return nil, err
	}
	srv := map[string]any{"address": h, "port": p}
	if u.User != nil {
		user := u.User.Username()
		pass, hasPass := u.User.Password()
		if !hasPass {
			// socks://base64(user:pass)@host:port
			if dec, err := b64(user); err == nil {
				if a, b, ok := strings.Cut(string(dec), ":"); ok {
					user, pass = a, b
				}
			}
		}
		if user != "" {
			srv["users"] = []any{map[string]any{"user": user, "pass": pass}}
		}
	}
	proto := "socks"
	if scheme == "http" || scheme == "https" {
		proto = "http"
	}
	out := map[string]any{"protocol": proto, "settings": map[string]any{"servers": []any{srv}}}
	if scheme == "https" {
		out["streamSettings"] = map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": h}}
	}
	return out, nil
}

func parseWireGuard(u *url.URL) (map[string]any, error) {
	h, p, err := hostPortOf(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	key, _ := url.PathUnescape(u.User.Username())
	if key == "" {
		key = q.Get("privatekey")
	}
	pub := orDefault(q.Get("publickey"), q.Get("peer"))
	if key == "" || pub == "" {
		return nil, terr("the wireguard link needs a private key and the peer public key")
	}
	addr := splitList(orDefault(q.Get("address"), q.Get("ip")))
	if len(addr) == 0 {
		addr = []any{"10.0.0.2/32"}
	}
	peer := map[string]any{"publicKey": pub, "endpoint": net.JoinHostPort(h, strconv.Itoa(p)), "allowedIPs": []any{"0.0.0.0/0", "::/0"}}
	if v := orDefault(q.Get("presharedkey"), q.Get("psk")); v != "" {
		peer["preSharedKey"] = v
	}
	s := map[string]any{"secretKey": key, "address": addr, "peers": []any{peer}}
	if v, err := strconv.Atoi(q.Get("mtu")); err == nil && v > 0 {
		s["mtu"] = v
	}
	if v := q.Get("reserved"); v != "" {
		var res []any
		for _, x := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(x))
			if err != nil || n < 0 || n > 255 {
				return nil, terr("reserved must be three numbers 0-255")
			}
			res = append(res, n)
		}
		s["reserved"] = res
	}
	return map[string]any{"protocol": "wireguard", "settings": s}, nil
}

func parseVmess(link string) (map[string]any, string, error) {
	dec, err := b64(strings.TrimPrefix(strings.TrimPrefix(link, "vmess://"), "VMESS://"))
	if err != nil {
		return nil, "", terr("invalid vmess link")
	}
	var v map[string]any
	if err := json.Unmarshal(dec, &v); err != nil {
		return nil, "", terr("invalid vmess link")
	}
	s := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return strings.TrimSpace(x)
		case float64:
			return strconv.Itoa(int(x))
		}
		return ""
	}
	port, _ := strconv.Atoi(s("port"))
	if s("add") == "" || port <= 0 || s("id") == "" {
		return nil, "", terr("the vmess link lacks address, port or id")
	}
	q := url.Values{}
	q.Set("type", s("net"))
	q.Set("path", s("path"))
	q.Set("host", s("host"))
	q.Set("headerType", s("type"))
	if s("net") == "grpc" {
		q.Set("serviceName", s("path"))
		q.Set("mode", s("type"))
	}
	if s("tls") == "tls" {
		q.Set("security", "tls")
		q.Set("sni", s("sni"))
		q.Set("fp", s("fp"))
		q.Set("alpn", s("alpn"))
	}
	user := map[string]any{"id": s("id"), "security": orDefault(s("scy"), "auto")}
	return map[string]any{"protocol": "vmess",
		"settings":       map[string]any{"vnext": []any{map[string]any{"address": s("add"), "port": port, "users": []any{user}}}},
		"streamSettings": streamFromQuery(q, "none")}, s("ps"), nil
}
