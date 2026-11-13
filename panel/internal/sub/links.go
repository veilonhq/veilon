package sub

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// fragment escapes a remark for use after '#'.
func fragment(s string) string { return url.PathEscape(s) }

func streamQuery(p *Proxy, q url.Values) {
	q.Set("type", p.Network)
	switch p.Network {
	case "ws", "httpupgrade":
		q.Set("path", p.Path)
		if p.Host != "" {
			q.Set("host", p.Host)
		}
	case "xhttp":
		q.Set("path", p.Path)
		if p.Host != "" {
			q.Set("host", p.Host)
		}
		if p.Mode != "" {
			q.Set("mode", p.Mode)
		}
	case "grpc":
		q.Set("serviceName", p.ServiceName)
		if p.Mode != "" {
			q.Set("mode", p.Mode)
		}
	case "tcp":
		q.Set("headerType", "none")
	}
	switch p.Security {
	case "tls":
		q.Set("security", "tls")
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if len(p.ALPN) > 0 {
			q.Set("alpn", strings.Join(p.ALPN, ","))
		}
		if p.Fingerprint != "" {
			q.Set("fp", p.Fingerprint)
		}
		if p.Insecure {
			q.Set("allowInsecure", "1")
			if p.PinSHA256 != "" {
				q.Set("pcs", p.PinSHA256)
			}
		}
	case "reality":
		q.Set("security", "reality")
		q.Set("pbk", p.PublicKey)
		if p.ShortID != "" {
			q.Set("sid", p.ShortID)
		}
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		q.Set("fp", p.Fingerprint)
		if p.SpiderX != "" {
			q.Set("spx", p.SpiderX)
		}
	default:
		q.Set("security", "none")
	}
}

// URI returns the share link of a proxy.
func URI(p *Proxy) string {
	switch p.Protocol {
	case "vless":
		q := url.Values{}
		q.Set("encryption", orNone(p.Encryption))
		streamQuery(p, q)
		if p.Flow != "" {
			q.Set("flow", p.Flow)
		}
		return "vless://" + p.UUID + "@" + hostPort(p.Server, p.Port) + "?" + q.Encode() + "#" + fragment(p.Name)
	case "trojan":
		q := url.Values{}
		streamQuery(p, q)
		return "trojan://" + url.PathEscape(p.Password) + "@" + hostPort(p.Server, p.Port) + "?" + q.Encode() + "#" + fragment(p.Name)
	case "vmess":
		return vmessURI(p)
	case "shadowsocks":
		var userinfo string
		if inbounds.IsSS2022(p.Method) {
			// SIP022: plain, percent-encoded userinfo for 2022 ciphers.
			userinfo = url.PathEscape(p.Method) + ":" + url.QueryEscape(strings.SplitN(p.Password, ":", 2)[0]) + ":" + url.QueryEscape(strings.SplitN(p.Password, ":", 2)[1])
		} else {
			// SIP002: URL-safe base64 of method:password.
			userinfo = base64.RawURLEncoding.EncodeToString([]byte(p.Method + ":" + p.Password))
		}
		return "ss://" + userinfo + "@" + hostPort(p.Server, p.Port) + "#" + fragment(p.Name)
	}
	return extraURI(p)
}

func vmessURI(p *Proxy) string {
	m := map[string]string{
		"v": "2", "ps": p.Name, "add": p.Server, "port": strconv.Itoa(p.Port), "id": p.UUID,
		"aid": "0", "scy": "auto", "net": p.Network, "type": "none", "host": "", "path": "", "tls": "",
	}
	switch p.Network {
	case "ws", "httpupgrade":
		m["host"], m["path"] = p.Host, p.Path
	case "xhttp":
		m["host"], m["path"] = p.Host, p.Path
		if p.Mode != "" {
			m["type"] = p.Mode
		}
	case "grpc":
		m["path"] = p.ServiceName
		m["type"] = p.Mode
	}
	if p.Security == "tls" {
		m["tls"] = "tls"
		m["sni"] = p.SNI
		if len(p.ALPN) > 0 {
			m["alpn"] = strings.Join(p.ALPN, ",")
		}
		if p.Fingerprint != "" {
			m["fp"] = p.Fingerprint
		}
		if p.Insecure {
			m["allowInsecure"] = "1"
			if p.PinSHA256 != "" {
				m["pcs"] = p.PinSHA256
			}
		}
	}
	b, _ := json.Marshal(m)
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

// V2Ray renders the base64 subscription body.
func V2Ray(ps []Proxy) string {
	lines := make([]string, 0, len(ps))
	for i := range ps {
		if u := URI(&ps[i]); u != "" {
			lines = append(lines, u)
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}
