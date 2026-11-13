package sub

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// ClashSupported reports whether Clash Meta can express this proxy.
func ClashSupported(p *Proxy) bool {
	switch p.Protocol {
	case "trojan":
		// Clash Meta's trojan always speaks TLS.
		return p.Network != "xhttp" && p.Security != "none"
	case "vless", "vmess", "shadowsocks":
		return p.Network != "xhttp"
	case inbounds.Hysteria, inbounds.TUIC, inbounds.HTTP, inbounds.Mixed, inbounds.Socks, inbounds.WireGuard, inbounds.AmneziaWG:
		return true
	}
	return false // mtproto, masque (Clash Meta's masque is WARP-only)
}

type clashProxy struct {
	Name              string         `yaml:"name"`
	Type              string         `yaml:"type"`
	Server            string         `yaml:"server"`
	Port              int            `yaml:"port"`
	UUID              string         `yaml:"uuid,omitempty"`
	AlterID           *int           `yaml:"alterId,omitempty"`
	Cipher            string         `yaml:"cipher,omitempty"`
	Password          string         `yaml:"password,omitempty"`
	UDP               bool           `yaml:"udp"`
	Network           string         `yaml:"network,omitempty"`
	TLS               bool           `yaml:"tls,omitempty"`
	ServerName        string         `yaml:"servername,omitempty"`
	SNI               string         `yaml:"sni,omitempty"`
	Flow              string         `yaml:"flow,omitempty"`
	ALPN              []string       `yaml:"alpn,omitempty"`
	ClientFingerprint string         `yaml:"client-fingerprint,omitempty"`
	SkipCertVerify    bool           `yaml:"skip-cert-verify,omitempty"`
	RealityOpts       map[string]any `yaml:"reality-opts,omitempty"`
	WSOpts            map[string]any `yaml:"ws-opts,omitempty"`
	GRPCOpts          map[string]any `yaml:"grpc-opts,omitempty"`
	// Newer protocols.
	Username             string         `yaml:"username,omitempty"`
	Fingerprint          string         `yaml:"fingerprint,omitempty"`
	Obfs                 string         `yaml:"obfs,omitempty"`
	ObfsPassword         string         `yaml:"obfs-password,omitempty"`
	Up                   string         `yaml:"up,omitempty"`
	Down                 string         `yaml:"down,omitempty"`
	CongestionController string         `yaml:"congestion-controller,omitempty"`
	UDPRelayMode         string         `yaml:"udp-relay-mode,omitempty"`
	IP                   string         `yaml:"ip,omitempty"`
	PrivateKey           string         `yaml:"private-key,omitempty"`
	PublicKey            string         `yaml:"public-key,omitempty"`
	PreSharedKey         string         `yaml:"pre-shared-key,omitempty"`
	MTU                  int            `yaml:"mtu,omitempty"`
	DNS                  []string       `yaml:"dns,omitempty"`
	AmneziaWGOption      map[string]any `yaml:"amnezia-wg-option,omitempty"`
}

type clashGroup struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
}

type clashConfig struct {
	MixedPort   int          `yaml:"mixed-port"`
	AllowLan    bool         `yaml:"allow-lan"`
	Mode        string       `yaml:"mode"`
	LogLevel    string       `yaml:"log-level"`
	IPv6        bool         `yaml:"ipv6"`
	Proxies     []clashProxy `yaml:"proxies"`
	ProxyGroups []clashGroup `yaml:"proxy-groups"`
	Rules       []string     `yaml:"rules"`
}

func clashExtra(p *Proxy) clashProxy {
	c := clashProxy{Name: p.Name, Server: p.Server, Port: p.Port, UDP: true}
	tls := func(alpn []string) {
		c.SNI, c.ALPN, c.SkipCertVerify = p.SNI, alpn, p.Insecure
		if p.Insecure && p.PinSHA256 != "" {
			c.SkipCertVerify, c.Fingerprint = false, p.PinSHA256
		}
	}
	switch p.Protocol {
	case inbounds.Hysteria:
		c.Type, c.Password = "hysteria2", p.Password
		if p.ObfsPassword != "" {
			c.Obfs, c.ObfsPassword = "salamander", p.ObfsPassword
		}
		if p.UpMbps > 0 {
			c.Up = fmt.Sprintf("%d Mbps", p.UpMbps)
		}
		if p.DownMbps > 0 {
			c.Down = fmt.Sprintf("%d Mbps", p.DownMbps)
		}
		tls([]string{"h3"})
	case inbounds.TUIC:
		c.Type, c.UUID, c.Password = "tuic", p.UUID, p.Password
		c.CongestionController, c.UDPRelayMode = p.Congestion, "native"
		tls([]string{"h3"})
	case inbounds.HTTP:
		c.Type, c.Username, c.Password, c.UDP = "http", p.Username, p.Password, false
		if p.Security == "tls" {
			c.TLS = true
			tls(nil)
		}
	case inbounds.Mixed:
		c.Type, c.Username, c.Password = "socks5", p.Username, p.Password
	case inbounds.Socks:
		c.Type, c.Username, c.Password, c.UDP = "socks5", p.Username, p.Password, p.UDP
	case inbounds.WireGuard, inbounds.AmneziaWG:
		w := p.WG
		c.Type, c.IP, c.PrivateKey, c.PublicKey, c.PreSharedKey, c.MTU = "wireguard", w.Address, w.PrivateKey, w.PeerPublicKey, w.PresharedKey, w.MTU
		if len(c.IP) > 3 && c.IP[len(c.IP)-3:] == "/32" {
			c.IP = c.IP[:len(c.IP)-3]
		}
		c.DNS = w.DNS
		if a := w.AWG; a != nil {
			c.AmneziaWGOption = map[string]any{"jc": a.Jc, "jmin": a.Jmin, "jmax": a.Jmax, "s1": a.S1, "s2": a.S2,
				"h1": a.H1, "h2": a.H2, "h3": a.H3, "h4": a.H4}
		}
	}
	return c
}

func clashOf(p *Proxy) clashProxy {
	if !inbounds.IsV2(p.Protocol) {
		return clashExtra(p)
	}
	c := clashProxy{Name: p.Name, Server: p.Server, Port: p.Port, UDP: true}
	switch p.Protocol {
	case "vless":
		c.Type, c.UUID, c.Flow = "vless", p.UUID, p.Flow
	case "vmess":
		zero := 0
		c.Type, c.UUID, c.AlterID, c.Cipher = "vmess", p.UUID, &zero, "auto"
	case "trojan":
		c.Type, c.Password = "trojan", p.Password
	case "shadowsocks":
		c.Type, c.Cipher, c.Password = "ss", p.Method, p.Password
	}
	switch p.Network {
	case "ws":
		c.Network = "ws"
		c.WSOpts = map[string]any{"path": p.Path}
		if p.Host != "" {
			c.WSOpts["headers"] = map[string]string{"Host": p.Host}
		}
	case "httpupgrade":
		c.Network = "ws"
		c.WSOpts = map[string]any{"path": p.Path, "v2ray-http-upgrade": true}
		if p.Host != "" {
			c.WSOpts["headers"] = map[string]string{"Host": p.Host}
		}
	case "grpc":
		c.Network = "grpc"
		c.GRPCOpts = map[string]any{"grpc-service-name": p.ServiceName}
	case "tcp":
		if p.Protocol != "shadowsocks" {
			c.Network = "tcp"
		}
	}
	if p.Security == "tls" || p.Security == "reality" {
		if p.Protocol != "trojan" {
			c.TLS = true
			c.ServerName = p.SNI
		} else {
			c.SNI = p.SNI
		}
		c.ALPN = p.ALPN
		c.ClientFingerprint = p.Fingerprint
		c.SkipCertVerify = p.Insecure
	}
	if p.Security == "reality" {
		// Current Xray Reality servers negotiate the post-quantum
		// X25519MLKEM768 key share, and the chrome uTLS hello is the one
		// mihomo sends it with.
		c.RealityOpts = map[string]any{"public-key": p.PublicKey, "short-id": p.ShortID, "support-x25519mlkem768": true}
		c.ClientFingerprint = "chrome"
	}
	return c
}

// Clash renders a Clash Meta (mihomo) YAML configuration.
func Clash(ps []Proxy) ([]byte, error) {
	cfg := clashConfig{MixedPort: 7890, Mode: "rule", LogLevel: "info", IPv6: true, Proxies: []clashProxy{}}
	var names []string
	for i := range ps {
		if !ClashSupported(&ps[i]) {
			continue
		}
		cfg.Proxies = append(cfg.Proxies, clashOf(&ps[i]))
		names = append(names, ps[i].Name)
	}
	if len(names) > 0 {
		cfg.ProxyGroups = []clashGroup{
			{Name: "Proxy", Type: "select", Proxies: append([]string{"Auto"}, names...)},
			{Name: "Auto", Type: "url-test", Proxies: names, URL: "https://www.gstatic.com/generate_204", Interval: 300},
		}
	} else {
		cfg.ProxyGroups = []clashGroup{{Name: "Proxy", Type: "select", Proxies: []string{"DIRECT"}}}
	}
	cfg.Rules = []string{"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "MATCH,Proxy"}
	return yaml.Marshal(cfg)
}
