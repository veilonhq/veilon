package veiloncore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
)

// Profile types.
const (
	TypeVLESS       = "vless"
	TypeVMess       = "vmess"
	TypeTrojan      = "trojan"
	TypeShadowsocks = "shadowsocks"
	TypeHysteria2   = "hysteria2"
	TypeTUIC        = "tuic"
	TypeWireGuard   = "wireguard"
	TypeAmneziaWG   = "amneziawg"
	TypeHTTP        = "http"
	TypeSOCKS       = "socks"
	TypeMTProto     = "mtproto"
	TypeMasque      = "masque"
)

// removedTypes are profile types dropped in 1.0.4 (SlipNet family: SSH,
// StunTLS, DNSTT, NoizDNS, VayDNS, Slipstream, Soar; plus the NaiveProxy
// stub). Their links are rejected with ErrSlipnetRemoved / ErrTypeRemoved
// and stored profiles of these types are dropped by the app. Generic SOCKS5
// (socks:// links, SOCKS outbounds in subscriptions) is a standard
// Xray/sing-box protocol and stays supported.
var removedTypes = map[string]bool{
	"ssh": true, "dnstt": true, "noizdns": true, "vaydns": true, "slipstream": true, "soar": true,
	"naive": true,
}

// ErrSlipnetRemoved is returned (as an app string key) for slipnet:// /
// slipgate:// / ssh:// links: SlipNet protocols are no longer supported.
var ErrSlipnetRemoved = errors.New("err.slipnetRemoved")

// ErrTypeRemoved is returned (as an app string key) for naive:// links and
// for profiles of a removed type.
var ErrTypeRemoved = errors.New("err.typeRemoved")

// Profile is one server, independent of the core that runs it. It is the
// JSON exchanged with the app (stored by the app, passed back to Start).
type Profile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server,omitempty"`
	Port     int    `json:"port,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Username string `json:"username,omitempty"`
	Method   string `json:"method,omitempty"`
	Flow     string `json:"flow,omitempty"`
	// VLESS encryption (Xray "mlkem768x25519plus..." etc). "" or "none" = none.
	Encryption string `json:"encryption,omitempty"`
	// VMess security / alterId.
	VMessSecurity string `json:"vmessSecurity,omitempty"`
	AlterID       int    `json:"alterId,omitempty"`

	// Transport.
	Network     string `json:"network,omitempty"` // tcp ws grpc httpupgrade xhttp h2 quic
	Path        string `json:"path,omitempty"`
	Host        string `json:"host,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	Mode        string `json:"mode,omitempty"`
	HeaderType  string `json:"headerType,omitempty"`
	// XHTTP "extra" JSON (raw), passed through to Xray.
	XHTTPExtra string `json:"xhttpExtra,omitempty"`
	// XHTTP xPaddingBytes ("100-1000"); link "x_padding_bytes".
	XPaddingBytes string `json:"xPaddingBytes,omitempty"`
	// gRPC :authority override; link "authority".
	Authority string `json:"authority,omitempty"`
	// mKCP (network "kcp"): tuning, plus the legacy header obfuscation
	// (HeaderType none/srtp/utp/wechat-video/dtls/wireguard/dns) and Seed,
	// which current Xray carries as a finalmask "mkcp-legacy" UDP mask.
	KCP  *KCPConfig `json:"kcp,omitempty"`
	Seed string     `json:"seed,omitempty"`
	// XDRIVE (network "xdrive"): Xray xdriveSettings as raw JSON (service,
	// remoteFolder, secrets, template, s3, tuning). Only from JSON
	// subscriptions / manual edit: there is no share-link format.
	XDrive string `json:"xdrive,omitempty"`
	// Xray streamSettings.finalmask as raw JSON ({"tcp":[…],"udp":[…],
	// "quicParams":{…}}); link "fm". Shown as "TCP Masks" in some panels.
	FinalMask string `json:"finalMask,omitempty"`
	// Xray streamSettings.sockopt as raw JSON (tcpFastOpen, tcpcongestion,
	// domainStrategy, mark, interface, happyEyeballs, …).
	Sockopt string `json:"sockopt,omitempty"`
	// Xray outbound "mux" as raw JSON ({"enabled":true,"concurrency":8}).
	// XDRIVE always gets mux enabled.
	Mux string `json:"mux,omitempty"`

	// Security.
	Security    string   `json:"security,omitempty"` // none tls reality
	SNI         string   `json:"sni,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Fingerprint string   `json:"fp,omitempty"`
	Insecure    bool     `json:"insecure,omitempty"`
	// PinSHA256 is the hex SHA-256 of the server leaf certificate (DER).
	PinSHA256 string `json:"pinSHA256,omitempty"`
	// PinSPKI is the base64 SHA-256 of the leaf public key (sing-box pin).
	PinSPKI string `json:"pinSPKI,omitempty"`
	// TLS extras (Xray): ECH config list (link "ech"), verifyPeerCertByName
	// (link "vcn"), cipherSuites (link "cs"); REALITY mldsa65Verify ("pqv").
	ECH          string `json:"ech,omitempty"`
	VerifyName   string `json:"vcn,omitempty"`
	CipherSuites string `json:"cipherSuites,omitempty"`
	PQV          string `json:"pqv,omitempty"`
	PublicKey    string `json:"pbk,omitempty"`
	ShortID      string `json:"sid,omitempty"`
	SpiderX      string `json:"spx,omitempty"`

	// Hysteria2 / TUIC.
	ObfsPassword string `json:"obfsPassword,omitempty"`
	UpMbps       int    `json:"upMbps,omitempty"`
	DownMbps     int    `json:"downMbps,omitempty"`
	Congestion   string `json:"congestion,omitempty"`
	UDPRelayMode string `json:"udpRelayMode,omitempty"`
	// Hysteria2 port hopping ("20000-30000").
	Ports string `json:"ports,omitempty"`

	// Shadowsocks SIP003 plugin (only obfs/v2ray-plugin parsed).
	Plugin     string `json:"plugin,omitempty"`
	PluginOpts string `json:"pluginOpts,omitempty"`

	WG *WGConfig `json:"wg,omitempty"`

	// MTProto: secret; the app opens a tg:// link instead of connecting.
	Secret string `json:"secret,omitempty"`
	// MASQUE URI template path.
	MasquePath string `json:"masquePath,omitempty"`

	// AddressOverride, when set, replaces the address the client connects
	// to, for every profile type, without touching the stored link:
	//   - "host" or "host:port" ("[v6]:port"); a port replaces Port too.
	//   - every type (vless vmess trojan ss hysteria2 tuic wireguard
	//     amneziawg http socks masque): replaces Server. The original hostname is
	//     kept as TLS SNI and as the WS/HTTP Host header when those are
	//     empty, so a user-set SNI / Host always wins.
	AddressOverride string `json:"addressOverride,omitempty"`
	// CorePref overrides Settings.Core for V2Ray-family profiles (VLESS,
	// VMess, Trojan, Shadowsocks): "" / "auto", "sing-box" or "xray".
	CorePref string `json:"core,omitempty"`

	// Warning is set by the link parser (not part of the identity):
	// "privateHost" = the address the client must dial is a private /
	// link-local / CGNAT IP (e.g. a link generated with the server's
	// internal address), so it is unreachable from a phone unless an
	// AddressOverride is set.
	Warning string `json:"warning,omitempty"`

	// Source link (for sharing / re-export).
	Link string `json:"link,omitempty"`
	// Subscription the profile came from ("" = manual).
	SubID string `json:"subId,omitempty"`
}

// KCPConfig is Xray kcpSettings tuning (0 = Xray default).
type KCPConfig struct {
	MTU              int `json:"mtu,omitempty"`
	TTI              int `json:"tti,omitempty"`
	UplinkCapacity   int `json:"uplinkCapacity,omitempty"`
	DownlinkCapacity int `json:"downlinkCapacity,omitempty"`
	CwndMultiplier   int `json:"cwndMultiplier,omitempty"`
	MaxSendingWindow int `json:"maxSendingWindow,omitempty"`
}

// WGConfig is a WireGuard / AmneziaWG client.
type WGConfig struct {
	PrivateKey    string   `json:"privateKey"`
	PeerPublicKey string   `json:"peerPublicKey"`
	PresharedKey  string   `json:"presharedKey,omitempty"`
	Address       []string `json:"address"`
	MTU           int      `json:"mtu,omitempty"`
	DNS           []string `json:"dns,omitempty"`
	Keepalive     int      `json:"keepalive,omitempty"`
	Reserved      []int    `json:"reserved,omitempty"`
	AllowedIPs    []string `json:"allowedIPs,omitempty"`
	AWG           *AWG     `json:"awg,omitempty"`
}

// AWG holds AmneziaWG obfuscation parameters (1.x and 2.0). Header values
// are kept as strings because AWG 2.0 allows ranges ("100-200").
type AWG struct {
	Jc   int    `json:"jc,omitempty"`
	Jmin int    `json:"jmin,omitempty"`
	Jmax int    `json:"jmax,omitempty"`
	S1   int    `json:"s1,omitempty"`
	S2   int    `json:"s2,omitempty"`
	S3   int    `json:"s3,omitempty"`
	S4   int    `json:"s4,omitempty"`
	H1   string `json:"h1,omitempty"`
	H2   string `json:"h2,omitempty"`
	H3   string `json:"h3,omitempty"`
	H4   string `json:"h4,omitempty"`
	I1   string `json:"i1,omitempty"`
	I2   string `json:"i2,omitempty"`
	I3   string `json:"i3,omitempty"`
	I4   string `json:"i4,omitempty"`
	I5   string `json:"i5,omitempty"`
	// AWG 3.0: header protection (base64 32-byte key, must match the
	// server), random transport padding and randomised timers (ranges
	// "a-b" or "a", seconds / count). AWG 3.1: RandomTrailers (must match
	// the server) and DisableCookies.
	HeaderProtectionKey    string `json:"headerProtectionKey,omitempty"`
	ContentPaddingAddition string `json:"contentPaddingAddition,omitempty"`
	RekeyAfterTime         string `json:"rekeyAfterTime,omitempty"`
	RekeyTimeout           string `json:"rekeyTimeout,omitempty"`
	RejectAfterTime        string `json:"rejectAfterTime,omitempty"`
	KeepaliveTimeout       string `json:"keepaliveTimeout,omitempty"`
	MaxHandshakeAttempts   string `json:"maxHandshakeAttempts,omitempty"`
	RandomTrailers         bool   `json:"randomTrailers,omitempty"`
	DisableCookies         bool   `json:"disableCookies,omitempty"`
	// PersistentKeepalive as a range ("22-30"); a plain number stays in
	// WGConfig.Keepalive.
	PersistentKeepalive string `json:"persistentKeepalive,omitempty"`
}

// awg3 reports whether any AWG 3.x parameter is set.
func (a *AWG) awg3() bool {
	return a.HeaderProtectionKey != "" || a.ContentPaddingAddition != "" || a.RekeyAfterTime != "" || a.RekeyTimeout != "" ||
		a.RejectAfterTime != "" || a.KeepaliveTimeout != "" || a.MaxHandshakeAttempts != "" || a.RandomTrailers || a.DisableCookies
}

// Core identifies the engine that carries a profile.
type Core string

const (
	CoreSingbox Core = "sing-box"
	CoreXray    Core = "xray"
	CoreAWG     Core = "amneziawg"
	CoreNone    Core = "none"
)

// CoreOf returns the engine used for p.
func CoreOf(p *Profile) Core {
	switch p.Type {
	case TypeMTProto:
		return CoreNone
	case TypeAmneziaWG:
		return CoreAWG
	case TypeMasque:
		return CoreXray
	case TypeSOCKS:
		// sing-box's socks outbound has no TLS layer; Xray's has
		// streamSettings, so SOCKS over TLS runs on Xray.
		if p.Security == "tls" {
			return CoreXray
		}
		return CoreSingbox
	}
	if removedTypes[p.Type] {
		return CoreNone
	}
	if needsXray(p) || prefersXray(p) {
		return CoreXray
	}
	// Everything else, including cleartext VLESS/Trojan to a public address
	// (which Xray refuses to build), is carried by sing-box.
	return CoreSingbox
}

// ErrSingboxNeedsXray is returned (as an app string key, see API.md) when
// the user forced sing-box for a profile that uses an Xray-only feature.
var ErrSingboxNeedsXray = errors.New("err.singboxNeedsXray")

// normCorePref maps a user core choice to "", "sing-box" or "xray".
func normCorePref(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sing-box", "singbox", "sing_box":
		return string(CoreSingbox)
	case "xray":
		return string(CoreXray)
	}
	return ""
}

// ResolveCore returns the engine for p under the user's choice: the
// profile's CorePref wins over st.Core; "auto" is CoreOf. Only V2Ray-family
// profiles honour a choice. A choice that cannot carry the profile is an
// error, never a silent switch.
func ResolveCore(p *Profile, st Settings) (Core, error) {
	base := CoreOf(p)
	if !isV2(p.Type) {
		return base, nil
	}
	pref := normCorePref(p.CorePref)
	if pref == "" {
		pref = normCorePref(st.Core)
	}
	switch Core(pref) {
	case CoreSingbox:
		if needsXray(p) {
			return CoreSingbox, ErrSingboxNeedsXray
		}
		return CoreSingbox, nil
	case CoreXray:
		if xrayRefusesCleartext(p) {
			return CoreXray, ErrXrayCleartextPublicIP
		}
		return CoreXray, nil
	}
	return base, nil
}

// prefersXray: the "auto" choice is Xray although sing-box could build it.
// REALITY: Xray is the reference implementation; sing-box 1.14's REALITY
// client fails against current Xray servers ("reality verification failed"
// with every uTLS fingerprint) where Xray and v2rayNG connect (Run 105,
// live: sublive105_test.go). A user who forces sing-box still gets it.
func prefersXray(p *Profile) bool {
	return isV2(p.Type) && p.Security == "reality"
}

// needsXray reports whether a V2Ray-family profile uses a feature only Xray
// implements.
func needsXray(p *Profile) bool {
	if p.Type == TypeVLESS && p.Encryption != "" && p.Encryption != "none" {
		return true // VLESS post-quantum encryption
	}
	switch p.Network {
	case "xhttp", "splithttp", "kcp", "mkcp", "xdrive":
		return true
	}
	if p.Type == TypeShadowsocks && ((p.Network != "" && p.Network != "tcp") || (p.Security != "" && p.Security != "none") || p.HeaderType == "http") {
		// sing-box shadowsocks has no v2ray transport / TLS layer.
		return true
	}
	if isV2(p.Type) && (p.FinalMask != "" || p.Sockopt != "" || p.XDrive != "" || p.PQV != "" || p.VerifyName != "" || p.Mux != "") {
		// sing-box has no finalmask / Xray sockopt / mldsa65 / vcn.
		return true
	}
	return false
}

// computeID derives a stable id from the parts that identify a server.
func computeID(p *Profile) string {
	c := *p
	c.ID, c.Name, c.Link, c.SubID, c.AddressOverride, c.CorePref = "", "", "", "", "", ""
	c.Warning = ""
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func (p *Profile) normalize() {
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	p.CorePref = normCorePref(p.CorePref)
	p.Server = strings.Trim(strings.TrimSpace(p.Server), "[]")
	if p.Name == "" {
		p.Name = p.Server
	}
	if p.Network == "" && isV2(p.Type) {
		p.Network = "tcp"
	}
	switch p.Network {
	case "raw":
		p.Network = "tcp"
	case "mkcp":
		p.Network = "kcp"
	case "splithttp":
		p.Network = "xhttp"
	}
	// Hysteria2 / TUIC always run over TLS (QUIC); their links carry no
	// security key, so a form-made profile must say "tls" too.
	if (p.Type == TypeHysteria2 || p.Type == TypeTUIC) && (p.Security == "" || p.Security == "none") {
		p.Security = "tls"
	}
	if p.Security == "" {
		p.Security = "none"
	}
	if p.ID == "" {
		p.ID = computeID(p)
	}
}

// splitOverride parses "host", "host:port", "[v6]" or "[v6]:port".
func splitOverride(o string) (string, int) {
	o = strings.TrimSpace(o)
	if h, ps, err := net.SplitHostPort(o); err == nil {
		if n, err := strconv.Atoi(ps); err == nil && n > 0 && n <= 65535 {
			return strings.Trim(h, "[]"), n
		}
		return strings.Trim(h, "[]"), 0
	}
	return strings.Trim(o, "[]"), 0
}

func isHostname(s string) bool { return s != "" && net.ParseIP(s) == nil }

// withAddressOverride returns p itself when no override is set, otherwise a
// copy (nested configs copied too) with AddressOverride applied. See
// Profile.AddressOverride.
func (p *Profile) withAddressOverride() *Profile {
	host, port := splitOverride(p.AddressOverride)
	if host == "" {
		return p
	}
	c := *p
	if p.WG != nil {
		w := *p.WG
		c.WG = &w
	}
	orig := c.Server
	c.Server = host
	if port > 0 {
		c.Port = port
	}
	if isHostname(orig) && orig != host {
		if c.SNI == "" && (c.Security == "tls" || c.Security == "reality" ||
			c.Type == TypeHysteria2 || c.Type == TypeTUIC || c.Type == TypeMasque) {
			c.SNI = orig
		}
		switch c.Network {
		case "ws", "httpupgrade", "xhttp", "splithttp", "h2", "http":
			if c.Host == "" {
				c.Host = orig
			}
		}
	}
	return &c
}

func isV2(t string) bool {
	switch t {
	case TypeVLESS, TypeVMess, TypeTrojan, TypeShadowsocks:
		return true
	}
	return false
}

// WarnPrivateHost is Profile.Warning for links that dial a private address.
const WarnPrivateHost = "privateHost"

// dialHost is the address the phone itself connects to (before any
// AddressOverride).
func (p *Profile) dialHost() string {
	return p.Server
}

// isPrivateAddr: RFC 1918 / ULA, link-local, CGNAT 100.64/10. Loopback is
// not flagged (local proxies are legitimate).
func isPrivateAddr(h string) bool {
	ip := net.ParseIP(strings.Trim(h, "[]"))
	if ip == nil || ip.IsLoopback() {
		return false
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 {
		return true
	}
	return false
}

func (p *Profile) setWarnings() {
	if isPrivateAddr(p.dialHost()) {
		p.Warning = WarnPrivateHost
	} else if p.Warning == WarnPrivateHost {
		p.Warning = ""
	}
}
