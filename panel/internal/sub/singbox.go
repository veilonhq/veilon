package sub

import (
	"encoding/json"
	"fmt"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

type obj = map[string]any

// SingboxSupported reports whether sing-box can express this proxy.
func SingboxSupported(p *Proxy) bool {
	switch p.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks":
		// sing-box cannot complete a handshake with current Xray Reality
		// servers, so Reality entries are left out of its config.
		// sing-box has no VLESS Encryption: such inbounds are left out too.
		if p.Network == "kcp" || p.TCPHeader != nil {
			return false
		}
		if p.Protocol == "shadowsocks" && ((p.Network != "tcp" && p.Network != "") || p.Security != "none") {
			return false // sing-box's shadowsocks has no Xray transports
		}
		return p.Network != "xhttp" && p.Security != "reality" && p.Encryption == ""
	case inbounds.Hysteria, inbounds.TUIC, inbounds.HTTP, inbounds.Mixed, inbounds.Socks, inbounds.WireGuard,
		inbounds.ShadowTLS, inbounds.AnyTLS, inbounds.Naive:
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
		if rs, err := inbounds.ParseHopPorts(p.HopPorts); err == nil && len(rs) > 0 {
			// sing-box ranges are "from:to"; server_ports replaces server_port.
			ports := make([]string, len(rs))
			for i, r := range rs {
				ports[i] = fmt.Sprintf("%d:%d", r.From, r.To)
			}
			delete(o, "server_port")
			o["server_ports"] = ports
			if p.HopInterval > 0 {
				o["hop_interval"] = fmt.Sprintf("%ds", p.HopInterval)
			}
		}
		o["tls"] = singboxTLS(p, []string{"h3"})
	case inbounds.ShadowTLS:
		// Shadowsocks 2022 over the ShadowTLS outbound (singboxDetour).
		o["type"] = "shadowsocks"
		o["method"] = p.Method
		o["password"] = p.Password
		o["detour"] = p.Name + " · shadowtls"
	case inbounds.AnyTLS:
		o["type"] = "anytls"
		o["password"] = p.Password
		o["tls"] = singboxTLS(p, nil)
	case inbounds.Naive:
		// Needs a sing-box build with NaiveProxy (with_naive_outbound).
		o["type"] = "naive"
		o["username"] = p.Username
		o["password"] = p.Password
		o["tls"] = singboxTLS(p, nil)
	case inbounds.TUIC:
		o["type"] = "tuic"
		o["uuid"] = p.UUID
		o["password"] = p.Password
		o["congestion_control"] = p.Congestion
		o["udp_relay_mode"] = "native"
		if t := p.TUIC; t != nil {
			if t.UDPRelayMode != "" {
				o["udp_relay_mode"] = t.UDPRelayMode
			}
			if t.ZeroRTT {
				o["zero_rtt_handshake"] = true
			}
			if t.Heartbeat > 0 {
				o["heartbeat"] = fmt.Sprintf("%ds", t.Heartbeat)
			}
		}
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

// singboxDetour is the extra outbound a proxy's main outbound dials
// through (ShadowTLS), or nil.
func singboxDetour(p *Proxy) obj {
	if p.Protocol != inbounds.ShadowTLS {
		return nil
	}
	return obj{"type": "shadowtls", "tag": p.Name + " · shadowtls", "server": p.Server, "server_port": p.Port,
		"version": 3, "password": p.ShadowTLSPassword,
		"tls": obj{"enabled": true, "server_name": p.SNI, "utls": obj{"enabled": true, "fingerprint": p.Fingerprint}}}
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
		if h := clashHeaders(p); h != nil {
			t["headers"] = h
		}
		o["transport"] = t
	case "httpupgrade":
		t := obj{"type": "httpupgrade", "path": p.Path}
		if p.Host != "" {
			t["host"] = p.Host
		}
		if p.WS != nil && len(p.WS.Headers) > 0 {
			t["headers"] = p.WS.Headers
		}
		o["transport"] = t
	case "grpc":
		t := obj{"type": "grpc", "service_name": p.ServiceName}
		if g := p.GRPC; g != nil {
			if g.IdleTimeout > 0 {
				t["idle_timeout"] = fmt.Sprintf("%ds", g.IdleTimeout)
			}
			if g.HealthCheckTimeout > 0 {
				t["ping_timeout"] = fmt.Sprintf("%ds", g.HealthCheckTimeout)
			}
			if g.PermitWithoutStream {
				t["permit_without_stream"] = true
			}
		}
		o["transport"] = t
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
		if p.PinAlways && p.PinSPKI != "" {
			t["certificate_public_key_sha256"] = []string{p.PinSPKI}
		}
		if p.ECHConfig != "" {
			t["ech"] = obj{"enabled": true, "config": echPEM(p.ECHConfig)}
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

// echPEM wraps a base64 ECH config list as the PEM lines sing-box reads.
func echPEM(list string) []string {
	out := []string{"-----BEGIN ECH CONFIGS-----"}
	for len(list) > 64 {
		out = append(out, list[:64])
		list = list[64:]
	}
	if list != "" {
		out = append(out, list)
	}
	return append(out, "-----END ECH CONFIGS-----")
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
			if d := singboxDetour(&ps[i]); d != nil {
				outs = append(outs, d)
			}
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
