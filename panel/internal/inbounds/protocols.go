package inbounds

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"regexp"
	"strings"
)

// Protocol identifiers.
const (
	VLESS       = "vless"
	VMess       = "vmess"
	Trojan      = "trojan"
	Shadowsocks = "shadowsocks"
	Hysteria    = "hysteria" // Hysteria 2
	TUIC        = "tuic"
	WireGuard   = "wireguard"
	AmneziaWG   = "amneziawg"
	HTTP        = "http"
	Mixed       = "mixed" // SOCKS5 + HTTP on one port
	Socks       = "socks" // SOCKS5 only (password or no authentication)
	Tunnel      = "tunnel"
	TUN         = "tun"
	MTProto     = "mtproto"
	Masque      = "masque"
)

// Engines that run inbounds.
const (
	EngineXray    = "xray"
	EngineSingbox = "sing-box"
	EngineMTG     = "mtg"
	EngineAWG     = "amneziawg"
)

// Engine returns the process that serves a protocol.
func Engine(protocol string) string {
	switch protocol {
	case TUIC:
		return EngineSingbox
	case MTProto:
		return EngineMTG
	case AmneziaWG:
		return EngineAWG
	}
	return EngineXray
}

// NodeCapable reports whether inbounds of this protocol can run on a node:
// Xray-core inbounds (except TUN, which needs the host's network),
// AmneziaWG and MTProto.
func NodeCapable(protocol string) bool {
	switch Engine(protocol) {
	case EngineXray:
		return protocol != TUN
	case EngineAWG, EngineMTG:
		return true
	}
	return false
}

// Node agent capabilities needed by protocols other than Xray's.
const (
	CapAWG     = "amneziawg"
	CapMTProto = "mtproto"
)

// NodeCap returns the capability a node agent needs to run inbounds of this
// protocol ("" for Xray inbounds, which every agent runs). Agents before
// v0.3.8 have none of them.
func NodeCap(protocol string) string {
	switch protocol {
	case AmneziaWG:
		return CapAWG
	case MTProto:
		return CapMTProto
	}
	return ""
}

// HasUsers reports whether the protocol authenticates individual users.
func HasUsers(protocol string) bool { return protocol != Tunnel && protocol != TUN }

// IsProxyLogin reports whether users sign in to the protocol with a proxy
// username and password (HTTP, Mixed, SOCKS): these use the users' custom
// proxy login when set.
func IsProxyLogin(protocol string) bool {
	return protocol == HTTP || protocol == Mixed || protocol == Socks
}

// IsV2 reports whether the protocol uses the shared stream settings
// (network/security/transport).
func IsV2(protocol string) bool {
	return protocol == VLESS || protocol == VMess || protocol == Trojan || protocol == Shadowsocks
}

// TLSRequired reports whether the protocol always runs over TLS (QUIC based).
func TLSRequired(protocol string) bool {
	return protocol == Hysteria || protocol == TUIC || protocol == Masque
}

// IsWG reports whether the protocol is WireGuard based (per-user peers).
func IsWG(protocol string) bool { return protocol == WireGuard || protocol == AmneziaWG }

// AWG holds AmneziaWG obfuscation parameters (protocol version 1.x).
type AWG struct {
	Jc   int    `json:"jc"`
	Jmin int    `json:"jmin"`
	Jmax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	H1   uint32 `json:"h1"`
	H2   uint32 `json:"h2"`
	H3   uint32 `json:"h3"`
	H4   uint32 `json:"h4"`
}

// Settings holds protocol-specific options. Fields that do not apply to the
// inbound's protocol are cleared by Normalize.
type Settings struct {
	// Hysteria 2: optional Salamander obfuscation and bandwidth hints for clients.
	ObfsPassword string `json:"obfs_password"`
	UpMbps       int    `json:"up_mbps"`
	DownMbps     int    `json:"down_mbps"`
	// TUIC congestion control: bbr, cubic or new_reno.
	Congestion string `json:"congestion"`
	// WireGuard / AmneziaWG server interface.
	PrivateKey string   `json:"private_key"`
	PublicKey  string   `json:"public_key"`
	Address    string   `json:"address"` // server address with prefix, e.g. 10.66.66.1/24
	DNS        []string `json:"dns"`
	Keepalive  int      `json:"keepalive"`
	AWG        *AWG     `json:"awg"`
	// MTU for wireguard, amneziawg, masque and tun.
	MTU int `json:"mtu"`
	// Mixed, Socks: allow SOCKS5 UDP ASSOCIATE.
	UDP bool `json:"udp"`
	// Socks: no authentication. Anyone who reaches the port can use it and
	// its traffic cannot be told apart per user.
	NoAuth bool `json:"no_auth"`
	// Tunnel (port forward).
	TargetAddress string `json:"target_address"`
	TargetPort    int    `json:"target_port"`
	TunnelNetwork string `json:"tunnel_network"` // tcp | udp | tcp,udp
	// TUN interface.
	TunName string   `json:"tun_name"`
	Gateway []string `json:"gateway"`
	Routes  []string `json:"routes"`
	// MTProto FakeTLS fronting domain.
	FakeTLSDomain string `json:"faketls_domain"`
	// MASQUE address pool and path.
	Pool []string `json:"pool"`
	Path string   `json:"path"`
}

// MasqueDefaultPath is the RFC 9484 default URI template.
const MasqueDefaultPath = "/.well-known/masque/ip/{target}/{ipproto}/"

var (
	ifaceRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,14}$`)
	hostRe  = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)
)

func randInt(min, max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	return min + int(n.Int64())
}

// GenerateWGKeys returns a new WireGuard private/public key pair (base64).
func GenerateWGKeys() (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// WGPublicKey derives the public key of a base64 WireGuard private key.
func WGPublicKey(priv string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(priv))
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("private key must be 32 bytes of base64")
	}
	k, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// GeneratePSK returns a random 32-byte base64 preshared key.
func GeneratePSK() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// DefaultAWG returns randomized AmneziaWG parameters within the ranges the
// AmneziaWG clients generate.
func DefaultAWG() *AWG {
	a := &AWG{Jc: randInt(3, 8), Jmin: 40, Jmax: 70, S1: randInt(15, 150)}
	for {
		a.S2 = randInt(15, 150)
		if a.S1+56 != a.S2 {
			break
		}
	}
	seen := map[uint32]bool{}
	hs := make([]uint32, 0, 4)
	for len(hs) < 4 {
		h := uint32(randInt(5, 2147483647))
		if !seen[h] {
			seen[h] = true
			hs = append(hs, h)
		}
	}
	a.H1, a.H2, a.H3, a.H4 = hs[0], hs[1], hs[2], hs[3]
	return a
}

// MTProtoSecret returns the FakeTLS ("ee") secret of a user for a fronting
// domain. The 16-byte key is derived from the user's password.
func MTProtoSecret(domain, password string) string {
	h := sha256.Sum256([]byte("veilon-mtproto|" + password))
	return "ee" + hex.EncodeToString(h[:16]) + hex.EncodeToString([]byte(domain))
}

// Transports reports which socket types an inbound listens on.
func Transports(v *View) (tcp, udp bool) {
	switch v.Protocol {
	case VLESS, VMess, Trojan:
		tcp = true
		udp = v.Network == "xhttp" && v.TLS != nil && containsStr(v.TLS.ALPN, "h3")
		if v.Network == "kcp" {
			tcp, udp = false, true
		}
	case Shadowsocks:
		// RAW also listens on UDP unless switched off; other transports
		// carry everything in their own stream.
		tcp, udp = true, (v.Network == "tcp" || v.Network == "") && !(v.Advanced != nil && v.Advanced.SS != nil && v.Advanced.SS.NoUDP)
		if v.Network == "kcp" {
			tcp, udp = false, true
		}
		if v.Network == "xhttp" && v.TLS != nil && containsStr(v.TLS.ALPN, "h3") {
			udp = true
		}
	case Hysteria, TUIC, WireGuard, AmneziaWG:
		udp = true
	case Masque:
		alpn := []string{"h3"}
		if v.TLS != nil && len(v.TLS.ALPN) > 0 {
			alpn = v.TLS.ALPN
		}
		tcp, udp = containsStr(alpn, "h2"), containsStr(alpn, "h3")
	case HTTP, MTProto:
		tcp = true
	case Mixed, Socks:
		tcp, udp = true, v.Settings.UDP
	case Tunnel:
		n := v.Settings.TunnelNetwork
		tcp, udp = strings.Contains(n, "tcp"), strings.Contains(n, "udp")
	}
	return
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func parseCIDRList(list []string, what string, maxV4, maxV6 int) ([]string, error) {
	var out []string
	v4, v6 := 0, 0
	for _, c := range list {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, verr("%s: %q is not a valid CIDR (e.g. 10.0.0.1/24)", what, c)
		}
		if p.Addr().Is4() {
			v4++
		} else {
			v6++
		}
		out = append(out, p.String())
	}
	if (maxV4 >= 0 && v4 > maxV4) || (maxV6 >= 0 && v6 > maxV6) {
		return nil, verr("%s: at most %d IPv4 and %d IPv6 prefix", what, maxV4, maxV6)
	}
	return out, nil
}

// normalizeWGAddress validates the server tunnel address (IPv4 prefix 16-30).
func normalizeWGAddress(a string) (string, error) {
	a = strings.TrimSpace(a)
	if a == "" {
		return fmt.Sprintf("10.%d.%d.1/24", randInt(64, 127), randInt(0, 255)), nil
	}
	p, err := netip.ParsePrefix(a)
	if err != nil || !p.Addr().Is4() {
		return "", verr("settings.address must be an IPv4 address with prefix, e.g. 10.66.66.1/24")
	}
	if p.Bits() < 16 || p.Bits() > 30 {
		return "", verr("settings.address prefix must be between /16 and /30")
	}
	if p.Addr() == p.Masked().Addr() {
		return "", verr("settings.address must be a host address inside the subnet (e.g. .1), not the network address")
	}
	return p.String(), nil
}

// normalizeProtocol validates and defaults protocol-specific settings for
// the non-v2 protocols. Called by Normalize.
func normalizeProtocol(v *View) error {
	src := v.Settings
	s := Settings{}
	v.Flow, v.SSMethod, v.SSServerKey, v.Reality = "", "", "", nil
	v.Transport = Transport{}
	switch v.Protocol {
	case Hysteria:
		s.ObfsPassword = strings.TrimSpace(src.ObfsPassword)
		s.UpMbps, s.DownMbps = src.UpMbps, src.DownMbps
		if s.UpMbps < 0 || s.DownMbps < 0 || s.UpMbps > 100000 || s.DownMbps > 100000 {
			return verr("settings.up_mbps/down_mbps must be 0-100000")
		}
	case TUIC:
		s.Congestion = src.Congestion
		if s.Congestion == "" {
			s.Congestion = "bbr"
		}
		if !in(s.Congestion, []string{"bbr", "cubic", "new_reno"}) {
			return verr("settings.congestion must be bbr, cubic or new_reno")
		}
	case WireGuard, AmneziaWG:
		s.PrivateKey = strings.TrimSpace(src.PrivateKey)
		if s.PrivateKey == "" {
			var err error
			if s.PrivateKey, _, err = GenerateWGKeys(); err != nil {
				return err
			}
		}
		pub, err := WGPublicKey(s.PrivateKey)
		if err != nil {
			return verr("settings.private_key: %v", err)
		}
		s.PublicKey = pub
		if s.Address, err = normalizeWGAddress(src.Address); err != nil {
			return err
		}
		s.MTU = src.MTU
		if s.MTU == 0 {
			s.MTU = 1420
		}
		if s.MTU < 1280 || s.MTU > 1500 {
			return verr("settings.mtu must be 1280-1500")
		}
		for _, d := range src.DNS {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if net.ParseIP(d) == nil {
				return verr("settings.dns: %q is not an IP address", d)
			}
			s.DNS = append(s.DNS, d)
		}
		if src.DNS == nil && len(s.DNS) == 0 {
			s.DNS = []string{"1.1.1.1", "8.8.8.8"}
		}
		if s.DNS == nil {
			s.DNS = []string{}
		}
		s.Keepalive = src.Keepalive
		if s.Keepalive < 0 || s.Keepalive > 600 {
			return verr("settings.keepalive must be 0-600 seconds")
		}
		if v.Protocol == AmneziaWG {
			a := src.AWG
			if a == nil {
				a = DefaultAWG()
			}
			if a.Jc < 0 || a.Jc > 128 || a.Jmin < 0 || a.Jmax > 1280 || a.Jmin > a.Jmax || (a.Jc > 0 && a.Jmax == 0) {
				return verr("awg: need 0 <= jc <= 128 and 0 <= jmin <= jmax <= 1280")
			}
			if a.S1 < 0 || a.S1 > 1132 || a.S2 < 0 || a.S2 > 1188 || (a.S1 > 0 && a.S1+56 == a.S2) {
				return verr("awg: s1 must be 0-1132, s2 0-1188 and s1+56 must differ from s2")
			}
			hs := []uint32{a.H1, a.H2, a.H3, a.H4}
			seen := map[uint32]bool{}
			for _, h := range hs {
				if h < 5 || seen[h] {
					return verr("awg: h1-h4 must be distinct numbers greater than 4")
				}
				seen[h] = true
			}
			s.AWG = a
		}
	case HTTP:
		if v.Security != "none" && v.Security != "tls" {
			return verr("http inbounds support security none or tls")
		}
	case Mixed:
		if v.Security != "none" {
			return verr("mixed inbounds support security none only")
		}
		s.UDP = src.UDP
	case Socks:
		if v.Security != "none" {
			return verr("socks inbounds support security none only")
		}
		s.UDP, s.NoAuth = src.UDP, src.NoAuth
	case Tunnel:
		s.TargetAddress = strings.TrimSpace(src.TargetAddress)
		if s.TargetAddress == "" {
			return verr("settings.target_address is required (where to forward)")
		}
		if net.ParseIP(s.TargetAddress) == nil && !hostRe.MatchString(s.TargetAddress) && s.TargetAddress != "localhost" {
			return verr("settings.target_address must be an IP address or a host name")
		}
		s.TargetPort = src.TargetPort
		if s.TargetPort < 1 || s.TargetPort > 65535 {
			return verr("settings.target_port must be 1-65535")
		}
		s.TunnelNetwork = src.TunnelNetwork
		if s.TunnelNetwork == "" {
			s.TunnelNetwork = "tcp"
		}
		if !in(s.TunnelNetwork, []string{"tcp", "udp", "tcp,udp"}) {
			return verr("settings.tunnel_network must be tcp, udp or tcp,udp")
		}
	case TUN:
		s.TunName = strings.TrimSpace(src.TunName)
		if s.TunName == "" {
			s.TunName = "veilon-tun"
		}
		if !ifaceRe.MatchString(s.TunName) {
			return verr("settings.tun_name must be 1-15 letters, digits, - or _")
		}
		if reservedTunName(s.TunName) {
			return verr("settings.tun_name must not start with vt-, ve-, vb-, vz- or be host0 (reserved by the host)")
		}
		s.MTU = src.MTU
		if s.MTU == 0 {
			s.MTU = 1500
		}
		if s.MTU < 576 || s.MTU > 9000 {
			return verr("settings.mtu must be 576-9000")
		}
		g := src.Gateway
		if len(g) == 0 {
			g = []string{"10.77.0.1/24"}
		}
		var err error
		if s.Gateway, err = parseCIDRList(g, "settings.gateway", 1, 1); err != nil {
			return err
		}
		if len(s.Gateway) == 0 {
			return verr("settings.gateway is required")
		}
		if s.Routes, err = parseCIDRList(src.Routes, "settings.routes", -1, -1); err != nil {
			return err
		}
		for _, r := range s.Routes {
			if r == "0.0.0.0/0" || r == "::/0" {
				return verr("settings.routes: a default route would send the server's own traffic into the TUN; route specific subnets only")
			}
		}
		if s.Routes == nil {
			s.Routes = []string{}
		}
		v.Port = 0
		v.Listen = ""
	case MTProto:
		s.FakeTLSDomain = strings.ToLower(strings.TrimSpace(src.FakeTLSDomain))
		if s.FakeTLSDomain == "" {
			s.FakeTLSDomain = "www.cloudflare.com"
		}
		if !hostRe.MatchString(s.FakeTLSDomain) {
			return verr("settings.faketls_domain must be a host name, e.g. www.cloudflare.com")
		}
	case Masque:
		pool := src.Pool
		if len(pool) == 0 {
			pool = []string{"10.14.0.1/24", "fd14::1/64"}
		}
		var err error
		if s.Pool, err = parseCIDRList(pool, "settings.pool", 1, 1); err != nil {
			return err
		}
		for _, p := range s.Pool {
			pp := netip.MustParsePrefix(p)
			if (pp.Addr().Is4() && pp.Bits() > 30) || (pp.Addr().Is6() && pp.Bits() > 126) {
				return verr("settings.pool: prefix %s is too small", p)
			}
		}
		if len(s.Pool) == 0 {
			return verr("settings.pool is required")
		}
		s.MTU = src.MTU
		if s.MTU == 0 {
			s.MTU = 1280
		}
		if s.MTU < 1280 || s.MTU > 65535 {
			return verr("settings.mtu must be 1280-65535")
		}
		s.Path = strings.TrimSpace(src.Path)
		if s.Path == "" {
			s.Path = MasqueDefaultPath
		}
		if !strings.HasPrefix(s.Path, "/") {
			return verr("settings.path must start with /")
		}
	}
	switch {
	case TLSRequired(v.Protocol):
	case v.Protocol == HTTP && v.Security == "tls":
		if v.TLS == nil || v.TLS.CertID == nil {
			return verr("tls.cert_id is required for an HTTPS proxy")
		}
	default:
		v.Security, v.TLS = "none", nil
	}
	if TLSRequired(v.Protocol) {
		v.Security = "tls"
		if v.TLS == nil {
			v.TLS = &TLS{}
		}
		switch v.Protocol {
		case Hysteria, TUIC:
			v.TLS.ALPN = []string{"h3"}
		case Masque:
			var a []string
			for _, x := range v.TLS.ALPN {
				if x == "h3" || x == "h2" {
					a = append(a, x)
				}
			}
			if len(a) == 0 {
				a = []string{"h3"}
			}
			v.TLS.ALPN = a
		}
	}
	if v.TLS != nil {
		if v.TLS.ALPN == nil {
			v.TLS.ALPN = []string{}
		}
		v.TLS.SNI = strings.TrimSpace(v.TLS.SNI)
	}
	switch v.Protocol {
	case Hysteria, TUIC, WireGuard, AmneziaWG:
		v.Network = "udp"
	case Masque:
		v.Network = "quic"
		if !containsStr(v.TLS.ALPN, "h3") {
			v.Network = "h2"
		}
	case TUN:
		v.Network = "tun"
	case Tunnel:
		v.Network = s.TunnelNetwork
	default:
		v.Network = "tcp"
	}
	if v.Protocol == TUN || v.Protocol == Tunnel || v.Protocol == AmneziaWG || v.Protocol == MTProto {
		v.Sniffing = false
	}
	v.Settings = s
	return nil
}

// reservedTunName reports interface names the host's container and
// virtualization tooling owns.
func reservedTunName(n string) bool {
	l := strings.ToLower(n)
	if l == "host0" {
		return true
	}
	for _, p := range []string{"vt-", "ve-", "vb-", "vz-"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}
