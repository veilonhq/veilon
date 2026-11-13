package sub

import (
	"encoding/json"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

type obj = map[string]any

// SingboxSupported reports whether sing-box can express this proxy.
func SingboxSupported(p *Proxy) bool {
	switch p.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks":
		// sing-box cannot complete a handshake with current Xray Reality
		// servers, so Reality entries are left out of its config.
		return p.Network != "xhttp" && p.Security != "reality"
	case inbounds.Hysteria, inbounds.TUIC, inbounds.HTTP, inbounds.Mixed, inbounds.Socks, inbounds.WireGuard:
		return true
	}
	return false // amneziawg, mtproto, masque: not in sing-box stable
}

// singboxTLS renders client TLS for the QUIC/HTTP protocols.
func singboxTLS(p *Proxy, alpn []string) obj {
	t := obj{"enabled": true}
	if p.SNI != "" {
		t["server_name"] = p.SNI
	}
	if len(alpn) > 0 {
		t["alpn"] = alpn
	}
	if p.Insecure {
		if p.PinSPKI != "" {
			t["certificate_public_key_sha256"] = []string{p.PinSPKI}
		} else {
			t["insecure"] = true
		}
	}
	return t
}

// singboxExtra renders outbounds (or the wireguard endpoint) for the
// non-v2 protocols. isEndpoint reports a wireguard endpoint.
func singboxExtra(p *Proxy) (o obj, isEndpoint bool) {
	o = obj{"tag": p.Name, "server": p.Server, "server_port": p.Port}
	switch p.Protocol {
	case inbounds.Hysteria:
		o["type"] = "hysteria2"
		o["password"] = p.Password
		if p.ObfsPassword != "" {
			o["obfs"] = obj{"type": "salamander", "password": p.ObfsPassword}
		}
		if p.UpMbps > 0 {
			o["up_mbps"] = p.UpMbps
		}
		if p.DownMbps > 0 {
			o["down_mbps"] = p.DownMbps
		}
		o["tls"] = singboxTLS(p, []string{"h3"})
	case inbounds.TUIC:
		o["type"] = "tuic"
		o["uuid"] = p.UUID
		o["password"] = p.Password
		o["congestion_control"] = p.Congestion
		o["udp_relay_mode"] = "native"
		o["tls"] = singboxTLS(p, []string{"h3"})
	case inbounds.HTTP:
		o["type"] = "http"
		o["username"] = p.Username
		o["password"] = p.Password
		if p.Security == "tls" {
			o["tls"] = singboxTLS(p, nil)
		}
	case inbounds.Mixed:
		o["type"] = "socks"
		o["version"] = "5"
		o["username"] = p.Username
		o["password"] = p.Password
	case inbounds.Socks:
		o["type"] = "socks"
		o["version"] = "5"
		if !p.NoAuth {
			o["username"] = p.Username
			o["password"] = p.Password
		}
		if !p.UDP {
			o["network"] = "tcp"
		}
	case inbounds.WireGuard:
		w := p.WG
		peer := obj{"address": p.Server, "port": p.Port, "public_key": w.PeerPublicKey, "allowed_ips": []string{"0.0.0.0/0", "::/0"}}
		if w.PresharedKey != "" {
			peer["pre_shared_key"] = w.PresharedKey
		}
		if w.Keepalive > 0 {
			peer["persistent_keepalive_interval"] = w.Keepalive
		}
		return obj{"type": "wireguard", "tag": p.Name, "address": []string{w.Address}, "private_key": w.PrivateKey,
			"mtu": w.MTU, "peers": []any{peer}}, true
	}
	return o, false
}

func singboxOutbound(p *Proxy) obj {
	if !inbounds.IsV2(p.Protocol) {
		o, _ := singboxExtra(p)
		return o
	}
	o := obj{"tag": p.Name, "server": p.Server, "server_port": p.Port}
	switch p.Protocol {
	case "vless":
		o["type"] = "vless"
		o["uuid"] = p.UUID
		if p.Flow != "" {
			o["flow"] = p.Flow
		}
		o["packet_encoding"] = "xudp"
	case "vmess":
		o["type"] = "vmess"
		o["uuid"] = p.UUID
		o["security"] = "auto"
		o["alter_id"] = 0
	case "trojan":
		o["type"] = "trojan"
		o["password"] = p.Password
	case "shadowsocks":
		o["type"] = "shadowsocks"
		o["method"] = p.Method
		o["password"] = p.Password
	}
	switch p.Network {
	case "ws":
		t := obj{"type": "ws", "path": p.Path}
		if p.Host != "" {
			t["headers"] = obj{"Host": p.Host}
		}
		o["transport"] = t
	case "httpupgrade":
		t := obj{"type": "httpupgrade", "path": p.Path}
		if p.Host != "" {
			t["host"] = p.Host
		}
		o["transport"] = t
	case "grpc":
		o["transport"] = obj{"type": "grpc", "service_name": p.ServiceName}
	}
	switch p.Security {
	case "tls":
		t := obj{"enabled": true, "insecure": p.Insecure}
		if p.SNI != "" {
			t["server_name"] = p.SNI
		}
		if len(p.ALPN) > 0 {
			t["alpn"] = p.ALPN
		}
		if p.Fingerprint != "" {
			t["utls"] = obj{"enabled": true, "fingerprint": p.Fingerprint}
		}
		o["tls"] = t
	case "reality":
		o["tls"] = obj{
			"enabled":     true,
			"server_name": p.SNI,
			"utls":        obj{"enabled": true, "fingerprint": p.Fingerprint},
			"reality":     obj{"enabled": true, "public_key": p.PublicKey, "short_id": p.ShortID},
		}
	}
	return o
}

// Singbox renders a complete sing-box client configuration.
func Singbox(ps []Proxy) ([]byte, error) {
	var outs, eps []any
	var tags []string
	for i := range ps {
		if !SingboxSupported(&ps[i]) {
			continue
		}
		if ps[i].Protocol == inbounds.WireGuard {
			ep, _ := singboxExtra(&ps[i])
			eps = append(eps, ep)
		} else {
			outs = append(outs, singboxOutbound(&ps[i]))
		}
		tags = append(tags, ps[i].Name)
	}
	selector := append([]string{}, tags...)
	head := []any{}
	if len(tags) > 0 {
		selector = append([]string{"auto"}, selector...)
		head = append(head,
			obj{"type": "selector", "tag": "proxy", "outbounds": selector, "default": "auto"},
			obj{"type": "urltest", "tag": "auto", "outbounds": tags, "url": "https://www.gstatic.com/generate_204", "interval": "10m"},
		)
	} else {
		head = append(head, obj{"type": "selector", "tag": "proxy", "outbounds": []string{"direct"}})
	}
	all := append(head, outs...)
	all = append(all, obj{"type": "direct", "tag": "direct"})
	cfg := obj{
		"log": obj{"level": "warn", "timestamp": true},
		"dns": obj{
			"servers": []any{
				obj{"tag": "remote", "type": "https", "server": "1.1.1.1", "detour": "proxy"},
				obj{"tag": "local", "type": "local"},
			},
			"final": "remote",
		},
		"inbounds": []any{
			obj{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route": true, "strict_route": true, "stack": "mixed"},
			obj{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080},
		},
		"outbounds": all,
		"route": obj{
			"rules": []any{
				obj{"action": "sniff"},
				obj{"protocol": "dns", "action": "hijack-dns"},
				obj{"ip_is_private": true, "outbound": "direct"},
			},
			"final":                   "proxy",
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
	}
	if len(eps) > 0 {
		cfg["endpoints"] = eps
	}
	return json.MarshalIndent(cfg, "", "  ")
}
