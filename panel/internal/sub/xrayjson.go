package sub

import (
	"encoding/json"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// XraySupported reports whether an Xray client config can express the proxy.
func XraySupported(p *Proxy) bool {
	switch p.Protocol {
	case inbounds.TUIC, inbounds.AmneziaWG, inbounds.MTProto:
		return false
	}
	return true
}

func xrayClientTLS(p *Proxy, alpn []string) obj {
	t := obj{"serverName": p.SNI}
	if p.Insecure && p.PinSHA256 != "" {
		t["pinnedPeerCertSha256"] = p.PinSHA256
	}
	if len(alpn) > 0 {
		t["alpn"] = alpn
	}
	return t
}

func xrayExtraOutbound(p *Proxy) obj {
	o := obj{"tag": "proxy"}
	switch p.Protocol {
	case inbounds.Hysteria:
		o["protocol"] = "hysteria"
		o["settings"] = obj{"version": 2, "address": p.Server, "port": p.Port}
		ss := obj{"network": "hysteria", "hysteriaSettings": obj{"version": 2, "auth": p.Password},
			"security": "tls", "tlsSettings": xrayClientTLS(p, []string{"h3"})}
		if p.ObfsPassword != "" {
			ss["finalmask"] = obj{"udp": []any{obj{"type": "salamander", "settings": obj{"password": p.ObfsPassword}}}}
		}
		o["streamSettings"] = ss
	case inbounds.Masque:
		o["protocol"] = "masque"
		o["settings"] = obj{"address": p.Server, "port": p.Port}
		alpn := p.ALPN
		if len(alpn) == 0 {
			alpn = []string{"h3"}
		}
		o["streamSettings"] = obj{"network": "masque", "masqueSettings": obj{"path": p.MasquePath, "user": p.Username, "pass": p.Password},
			"security": "tls", "tlsSettings": xrayClientTLS(p, alpn)}
	case inbounds.WireGuard:
		w := p.WG
		peer := obj{"publicKey": w.PeerPublicKey, "endpoint": hostPort(p.Server, p.Port), "allowedIPs": []string{"0.0.0.0/0", "::/0"}}
		if w.PresharedKey != "" {
			peer["preSharedKey"] = w.PresharedKey
		}
		if w.Keepalive > 0 {
			peer["keepAlive"] = w.Keepalive
		}
		o["protocol"] = "wireguard"
		o["settings"] = obj{"secretKey": w.PrivateKey, "address": []string{w.Address}, "peers": []any{peer}, "mtu": w.MTU}
	case inbounds.HTTP:
		o["protocol"] = "http"
		o["settings"] = obj{"servers": []any{obj{"address": p.Server, "port": p.Port, "users": []any{obj{"user": p.Username, "pass": p.Password}}}}}
		if p.Security == "tls" {
			o["streamSettings"] = obj{"security": "tls", "tlsSettings": xrayClientTLS(p, nil)}
		}
	case inbounds.Mixed, inbounds.Socks:
		srv := obj{"address": p.Server, "port": p.Port}
		if !p.NoAuth {
			srv["users"] = []any{obj{"user": p.Username, "pass": p.Password}}
		}
		o["protocol"] = "socks"
		o["settings"] = obj{"servers": []any{srv}}
	}
	return o
}

func xrayOutbound(p *Proxy) obj {
	if !inbounds.IsV2(p.Protocol) {
		return xrayExtraOutbound(p)
	}
	o := obj{"tag": "proxy", "protocol": p.Protocol}
	switch p.Protocol {
	case "vless":
		u := obj{"id": p.UUID, "encryption": "none"}
		if p.Flow != "" {
			u["flow"] = p.Flow
		}
		o["settings"] = obj{"vnext": []any{obj{"address": p.Server, "port": p.Port, "users": []any{u}}}}
	case "vmess":
		o["settings"] = obj{"vnext": []any{obj{"address": p.Server, "port": p.Port, "users": []any{obj{"id": p.UUID, "security": "auto"}}}}}
	case "trojan":
		o["settings"] = obj{"servers": []any{obj{"address": p.Server, "port": p.Port, "password": p.Password}}}
	case "shadowsocks":
		o["settings"] = obj{"servers": []any{obj{"address": p.Server, "port": p.Port, "method": p.Method, "password": p.Password}}}
	}
	ss := obj{"network": p.Network, "security": p.Security}
	switch p.Network {
	case "ws":
		ss["wsSettings"] = obj{"path": p.Path, "host": p.Host}
	case "httpupgrade":
		ss["httpupgradeSettings"] = obj{"path": p.Path, "host": p.Host}
	case "xhttp":
		ss["xhttpSettings"] = obj{"path": p.Path, "host": p.Host, "mode": p.Mode}
	case "grpc":
		ss["grpcSettings"] = obj{"serviceName": p.ServiceName, "multiMode": p.Mode == "multi"}
	}
	switch p.Security {
	case "tls":
		// Current Xray-core rejects allowInsecure; pin the certificate instead.
		t := obj{"serverName": p.SNI}
		if p.Insecure && p.PinSHA256 != "" {
			t["pinnedPeerCertSha256"] = p.PinSHA256
		}
		if len(p.ALPN) > 0 {
			t["alpn"] = p.ALPN
		}
		if p.Fingerprint != "" {
			t["fingerprint"] = p.Fingerprint
		}
		ss["tlsSettings"] = t
	case "reality":
		ss["realitySettings"] = obj{"serverName": p.SNI, "fingerprint": p.Fingerprint, "publicKey": p.PublicKey, "shortId": p.ShortID, "spiderX": p.SpiderX}
	}
	o["streamSettings"] = ss
	return o
}

// XrayJSON renders an array of complete Xray client configs, one per proxy.
func XrayJSON(ps []Proxy) ([]byte, error) {
	out := []any{}
	for i := range ps {
		p := &ps[i]
		if !XraySupported(p) {
			continue
		}
		out = append(out, obj{
			"remarks": p.Name,
			"log":     obj{"loglevel": "warning"},
			"dns":     obj{"servers": []string{"1.1.1.1", "8.8.8.8"}},
			"inbounds": []any{
				obj{"tag": "socks", "port": 10808, "listen": "127.0.0.1", "protocol": "socks",
					"settings": obj{"udp": true}, "sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls"}}},
				obj{"tag": "http", "port": 10809, "listen": "127.0.0.1", "protocol": "http", "settings": obj{}},
			},
			"outbounds": []any{
				xrayOutbound(p),
				obj{"tag": "direct", "protocol": "freedom", "settings": obj{}},
				obj{"tag": "block", "protocol": "blackhole", "settings": obj{}},
			},
			"routing": obj{"domainStrategy": "AsIs", "rules": []any{
				obj{"type": "field", "ip": []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, "outboundTag": "direct"},
			}},
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

// DetectFormat picks an output format from the User-Agent.
func DetectFormat(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "clash") || strings.Contains(l, "mihomo") || strings.Contains(l, "stash"):
		return "clash"
	case strings.Contains(l, "sing-box") || strings.Contains(l, "singbox") ||
		strings.HasPrefix(l, "sfa/") || strings.HasPrefix(l, "sfi/") || strings.HasPrefix(l, "sfm/") || strings.HasPrefix(l, "sft/"):
		return "singbox"
	}
	return "v2ray"
}
