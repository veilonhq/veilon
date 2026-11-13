package sub

import (
	"encoding/json"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/xray"
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
		u := obj{"id": p.UUID, "encryption": orNone(p.Encryption)}
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
	o["streamSettings"] = xrayStream(p, true)
	return o
}

// xrayStream renders a client's streamSettings. withDownload adds the
// XHTTP download split (not inside the download settings themselves).
func xrayStream(p *Proxy, withDownload bool) obj {
	ss := obj{"network": p.Network, "security": p.Security}
	switch p.Network {
	case "ws":
		w := obj{"path": p.Path, "host": p.Host}
		if x := p.WS; x != nil {
			if len(x.Headers) > 0 {
				w["headers"] = x.Headers
			}
			if x.HeartbeatPeriod > 0 {
				w["heartbeatPeriod"] = x.HeartbeatPeriod
			}
		}
		ss["wsSettings"] = w
	case "httpupgrade":
		h := obj{"path": p.Path, "host": p.Host}
		if x := p.WS; x != nil && len(x.Headers) > 0 {
			h["headers"] = x.Headers
		}
		ss["httpupgradeSettings"] = h
	case "xhttp":
		h := obj{"path": p.Path, "host": p.Host, "mode": p.Mode}
		for k, v := range xhttpClientExtra(p, withDownload) {
			h[k] = v
		}
		ss["xhttpSettings"] = h
	case "grpc":
		g := obj{"serviceName": p.ServiceName, "multiMode": p.Mode == "multi"}
		if x := p.GRPC; x != nil {
			if x.UserAgent != "" {
				g["user_agent"] = x.UserAgent
			}
			if x.IdleTimeout > 0 {
				g["idle_timeout"] = x.IdleTimeout
			}
			if x.HealthCheckTimeout > 0 {
				g["health_check_timeout"] = x.HealthCheckTimeout
			}
			if x.PermitWithoutStream {
				g["permit_without_stream"] = true
			}
			if x.InitialWindowsSize > 0 {
				g["initial_windows_size"] = x.InitialWindowsSize
			}
		}
		ss["grpcSettings"] = g
	case "tcp":
		if p.TCPHeader != nil {
			ss["rawSettings"] = obj{"header": xray.TCPHeaderJSON(p.TCPHeader)}
		}
	case "kcp":
		if k := xray.KCPSettings(p.KCP); k != nil {
			ss["kcpSettings"] = k
		}
		if m := xray.KCPMasks(p.KCP); len(m) > 0 {
			ss["finalmask"] = obj{"udp": m}
		}
	}
	switch p.Security {
	case "tls":
		// Current Xray-core rejects allowInsecure; pin the certificate instead.
		t := obj{"serverName": p.SNI}
		if (p.Insecure || p.PinAlways) && p.PinSHA256 != "" {
			t["pinnedPeerCertSha256"] = p.PinSHA256
		}
		if len(p.ALPN) > 0 {
			t["alpn"] = p.ALPN
		}
		if p.Fingerprint != "" {
			t["fingerprint"] = p.Fingerprint
		}
		if p.ECHConfig != "" {
			t["echConfigList"] = p.ECHConfig
		}
		ss["tlsSettings"] = t
	case "reality":
		r := obj{"serverName": p.SNI, "fingerprint": p.Fingerprint, "publicKey": p.PublicKey, "shortId": p.ShortID, "spiderX": p.SpiderX}
		if p.PQV != "" {
			r["mldsa65Verify"] = p.PQV
		}
		ss["realitySettings"] = r
	}
	return ss
}

// xhttpClientExtra returns the client-side XHTTP options (padding, POST
// size, xmux, download split) as xhttpSettings keys.
func xhttpClientExtra(p *Proxy, withDownload bool) obj {
	x := p.XHTTP
	if x == nil {
		return nil
	}
	o := obj{}
	if x.XPaddingBytes != "" {
		o["xPaddingBytes"] = x.XPaddingBytes
	}
	if x.ScMaxEachPostBytes != "" {
		o["scMaxEachPostBytes"] = x.ScMaxEachPostBytes
	}
	if m := x.Xmux; m != nil {
		xm := obj{}
		for k, v := range map[string]string{"maxConcurrency": m.MaxConcurrency, "maxConnections": m.MaxConnections,
			"cMaxReuseTimes": m.CMaxReuseTimes, "hMaxRequestTimes": m.HMaxRequestTimes, "hMaxReusableSecs": m.HMaxReusableSecs} {
			if v != "" {
				xm[k] = v
			}
		}
		if m.HKeepAlivePeriod > 0 {
			xm["hKeepAlivePeriod"] = m.HKeepAlivePeriod
		}
		if len(xm) > 0 {
			o["xmux"] = xm
		}
	}
	if d := x.Download; d != nil && withDownload {
		dp := *p
		dp.Server = d.Address
		if d.Port > 0 {
			dp.Port = d.Port
		}
		if d.SNI != "" {
			dp.SNI = d.SNI
		}
		if d.Host != "" {
			dp.Host = d.Host
		}
		ds := xrayStream(&dp, false)
		ds["address"] = dp.Server
		ds["port"] = dp.Port
		o["downloadSettings"] = ds
	}
	return o
}

// xhttpLinkExtra is the share link "extra" parameter: the client-side
// XHTTP options as JSON ("" when none).
func xhttpLinkExtra(p *Proxy) string {
	o := xhttpClientExtra(p, true)
	if len(o) == 0 {
		return ""
	}
	b, _ := json.Marshal(o)
	return string(b)
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
