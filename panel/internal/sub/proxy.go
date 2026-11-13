// Package sub renders subscription content (share links, sing-box JSON,
// Clash Meta YAML, Xray JSON) for a user.
package sub

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/users"
)

// Proxy is a client-side description of one endpoint, independent of the
// output format.
type Proxy struct {
	InboundID int64
	Name      string
	Protocol  string
	Server    string
	Port      int
	UUID      string
	Password  string
	Method    string
	Flow      string
	// Encryption is the VLESS client "encryption" string ("" = none).
	Encryption  string
	Network     string
	Path        string
	Host        string
	ServiceName string
	Mode        string
	Security    string
	SNI         string
	ALPN        []string
	Fingerprint string
	Insecure    bool
	// PinSHA256 is the hex SHA-256 of the server's leaf certificate (DER),
	// set for TLS inbounds that allow insecure (self-signed) certificates.
	PinSHA256 string
	// PinSPKI is the base64 SHA-256 of the leaf's public key (sing-box pin).
	PinSPKI   string
	PublicKey string
	ShortID   string
	SpiderX   string

	// Username for http, mixed, socks and masque: the user's custom proxy
	// username (http, mixed, socks) or its stats identity.
	Username string
	// NoAuth: a socks inbound without authentication (no Username/Password).
	NoAuth bool
	// UDP: the socks/mixed inbound allows UDP ASSOCIATE.
	UDP bool
	// Hysteria 2.
	ObfsPassword string
	UpMbps       int
	DownMbps     int
	// TUIC.
	Congestion string
	// WireGuard / AmneziaWG client side.
	WG *WGClient
	// MTProto FakeTLS secret.
	Secret string
	// MASQUE URI template path.
	MasquePath string

	// Advanced settings clients need (zero = none).
	// PinAlways: carry the certificate pin even for a CA certificate.
	PinAlways bool
	// ECHConfig is the base64 ECH config list (TLS).
	ECHConfig string
	// PQV is the Reality ML-DSA-65 verify key.
	PQV       string
	TCPHeader *inbounds.TCPHeader
	WS        *inbounds.WSExtra
	GRPC      *inbounds.GRPCExtra
	XHTTP     *inbounds.XHTTPExtra
	KCP       *inbounds.KCPExtra
	TUIC      *inbounds.TUICExtra
}

// WGClient is the client half of a WireGuard / AmneziaWG peer.
type WGClient struct {
	PrivateKey    string
	PeerPublicKey string
	PresharedKey  string
	Address       string
	MTU           int
	DNS           []string
	Keepalive     int
	AWG           *inbounds.AWG
}

// Extras carries per-user data that is not part of the inbound itself.
type Extras struct {
	// Pins by certificate id.
	Pins map[int64]certs.Pins
	// Peers by inbound id (the user's WireGuard/AmneziaWG peer).
	Peers map[int64]db.WGPeer
	// Hosts by inbound id (enabled hosts, in order). An inbound with hosts
	// gets one proxy per host instead of the plain one.
	Hosts map[int64][]db.Host
	// Nodes by inbound id: each enabled node serving the inbound adds one
	// more proxy that connects to the node's address.
	Nodes map[int64][]inbounds.NodeTarget
	// Placed marks inbounds assigned to nodes: their proxies connect to the
	// nodes only, never to the main server's address (even when the main
	// server still runs them, as inbounds placed before v0.3.2 do).
	Placed map[int64]bool
	// NodeOnly marks placed inbounds the main server does not run.
	NodeOnly map[int64]bool
}

// Address picks the address clients should connect to for an inbound.
func Address(ib *inbounds.View, s db.Settings, reqHost string) string {
	if ib.AddressOverride != "" {
		return ib.AddressOverride
	}
	if s.ServerAddress != "" {
		return s.ServerAddress
	}
	if s.PanelDomain != "" {
		return s.PanelDomain
	}
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		return h
	}
	return reqHost
}

// BuildProxies converts the user's inbounds into client proxies. pins maps
// certificate ids to leaf SHA-256 fingerprints (may be nil).
func BuildProxies(u *db.User, ibs []inbounds.View, s db.Settings, reqHost string, pins map[int64]string) []Proxy {
	ex := Extras{Pins: map[int64]certs.Pins{}}
	for k, v := range pins {
		ex.Pins[k] = certs.Pins{CertSHA256: v}
	}
	return BuildProxiesX(u, ibs, s, reqHost, ex)
}

// BuildProxiesX is BuildProxies with full pins and WireGuard peers.
func BuildProxiesX(u *db.User, ibs []inbounds.View, s db.Settings, reqHost string, ex Extras) []Proxy {
	out := make([]Proxy, 0, len(ibs))
	used := map[string]int{}
	for i := range ibs {
		ib := &ibs[i]
		if !inbounds.HasUsers(ib.Protocol) {
			continue // tunnel / tun have no client links
		}
		if inbounds.IsWG(ib.Protocol) {
			if _, ok := ex.Peers[ib.ID]; !ok {
				continue
			}
		}
		p := Proxy{
			InboundID: ib.ID, Protocol: ib.Protocol, Server: Address(ib, s, reqHost), Port: ib.Port,
			Network: ib.Network, Security: ib.Security, Flow: ib.Flow,
			Path: ib.Transport.Path, Host: ib.Transport.Host, ServiceName: ib.Transport.ServiceName, Mode: ib.Transport.Mode,
		}
		switch ib.Protocol {
		case "vless", "vmess":
			p.UUID = u.UUID
			if ib.Protocol == "vless" && ib.VLESSEnc.On() {
				p.Encryption = ib.VLESSEnc.ClientEncryption()
			}
		case "trojan":
			p.Password = u.Password
		case "shadowsocks":
			p.Method = ib.SSMethod
			if inbounds.IsSS2022(ib.SSMethod) {
				p.Password = ib.SSServerKey + ":" + inbounds.SS2022UserKey(ib.SSMethod, u.Password)
			} else {
				p.Password = u.Password
			}
		}
		st := ib.Settings
		switch ib.Protocol {
		case inbounds.Hysteria:
			p.Password, p.ObfsPassword, p.UpMbps, p.DownMbps = u.Password, st.ObfsPassword, st.UpMbps, st.DownMbps
		case inbounds.TUIC:
			p.UUID, p.Password, p.Congestion = u.UUID, u.Password, st.Congestion
		case inbounds.HTTP, inbounds.Mixed, inbounds.Socks:
			p.Username, p.Password = users.ProxyLogin(u)
			p.UDP = st.UDP
			if ib.Protocol == inbounds.Socks && st.NoAuth {
				p.Username, p.Password, p.NoAuth = "", "", true
			}
		case inbounds.Masque:
			p.Username, p.Password = xrayEmail(u), u.Password
			p.MasquePath = st.Path
		case inbounds.MTProto:
			p.Secret = inbounds.MTProtoSecret(st.FakeTLSDomain, u.Password)
		case inbounds.WireGuard, inbounds.AmneziaWG:
			peer := ex.Peers[ib.ID]
			p.WG = &WGClient{PrivateKey: peer.PrivateKey, PeerPublicKey: st.PublicKey, PresharedKey: peer.PresharedKey,
				Address: peer.Address, MTU: st.MTU, DNS: st.DNS, Keepalive: st.Keepalive}
			if ib.Protocol == inbounds.AmneziaWG {
				p.WG.AWG = st.AWG
			}
		}
		if ib.Network == "grpc" && p.Mode == "" {
			p.Mode = "gun"
		}
		clientAdvanced(&p, ib)
		switch ib.Security {
		case "tls":
			if ib.TLS != nil {
				p.SNI = ib.TLS.SNI
				p.ALPN = ib.TLS.ALPN
				p.Fingerprint = ib.TLS.Fingerprint
				p.Insecure = ib.TLS.AllowInsecure
				if (p.Insecure || p.PinAlways) && ib.TLS.CertID != nil {
					p.PinSHA256 = ex.Pins[*ib.TLS.CertID].CertSHA256
					p.PinSPKI = ex.Pins[*ib.TLS.CertID].SPKISHA256
				}
			}
		case "reality":
			if r := ib.Reality; r != nil {
				if len(r.ServerNames) > 0 {
					p.SNI = r.ServerNames[0]
				}
				if len(r.ShortIDs) > 0 {
					p.ShortID = r.ShortIDs[0]
				}
				p.PublicKey = r.PublicKey
				p.Fingerprint = r.Fingerprint
				if p.Fingerprint == "" {
					p.Fingerprint = "chrome"
				}
				p.SpiderX = r.SpiderX
			}
		}
		// The main server's address is used only for inbounds without
		// nodes, or for one the main server still runs whose nodes are all
		// switched off (so its users keep a working link).
		var main []Proxy
		if !ex.Placed[ib.ID] || (len(ex.Nodes[ib.ID]) == 0 && !ex.NodeOnly[ib.ID]) {
			main = withHosts(p, ib, ex.Hosts[ib.ID])
		}
		for _, v := range append(main, withNodes(p, ib, ex.Nodes[ib.ID], ex.Hosts[ib.ID])...) {
			name := v.Name
			used[name]++
			if used[name] > 1 {
				name = fmt.Sprintf("%s %d", name, used[name])
			}
			v.Name = name
			out = append(out, v)
		}
	}
	return out
}

// withNodes returns the proxies of the nodes that run the inbound: one per
// node, connecting to the node's address and named "<remark> · <node>".
// With hosts, each node gets one proxy per host: the host's overrides apply
// on top of the node (a host address, when set, wins over the node's), and
// proxies that end up identical are listed once.
func withNodes(p Proxy, ib *inbounds.View, nodes []inbounds.NodeTarget, hosts []db.Host) []Proxy {
	out := make([]Proxy, 0, len(nodes)*max(1, len(hosts)))
	seen := map[string]bool{}
	for _, n := range nodes {
		np := p
		np.Server = n.Address
		for _, v := range withHosts(np, ib, hosts) {
			v.Name = v.Name + " · " + n.Name
			k := fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s", v.Server, v.Port, v.SNI, v.Host, v.Path, v.Fingerprint, strings.Join(v.ALPN, ","))
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

// withHosts expands a proxy into one proxy per host. Empty host fields keep
// the inbound's own value; with no hosts the proxy is returned unchanged.
func withHosts(p Proxy, ib *inbounds.View, hosts []db.Host) []Proxy {
	p.Name = ib.Remark
	if len(hosts) == 0 {
		return []Proxy{p}
	}
	out := make([]Proxy, 0, len(hosts))
	for _, h := range hosts {
		v := p
		if h.Remark != "" {
			v.Name = h.Remark
		}
		if h.Address != "" {
			v.Server = h.Address
		}
		if h.Port > 0 {
			v.Port = h.Port
		}
		if h.SNI != "" {
			v.SNI = h.SNI
		}
		if h.Host != "" {
			v.Host = h.Host
		}
		if h.Path != "" {
			v.Path = h.Path
		}
		if h.Fingerprint != "" {
			v.Fingerprint = h.Fingerprint
		}
		if h.ALPN != "" {
			v.ALPN = strings.Split(h.ALPN, ",")
		}
		if p.WG != nil {
			wg := *p.WG
			v.WG = &wg
		}
		out = append(out, v)
	}
	return out
}

// clientAdvanced copies the client-side parts of an inbound's advanced
// settings into the proxy.
func clientAdvanced(p *Proxy, ib *inbounds.View) {
	a := ib.Advanced
	if a == nil {
		return
	}
	if t := a.TLS; t != nil && ib.Security == "tls" {
		p.PinAlways = t.PinCert
		p.ECHConfig = t.ECHConfigList
	}
	if r := a.Reality; r != nil && ib.Security == "reality" {
		p.PQV = r.MLDSA65Verify
	}
	p.TCPHeader, p.WS, p.GRPC, p.XHTTP, p.KCP, p.TUIC = a.TCP, a.WS, a.GRPC, a.XHTTP, a.KCP, a.TUIC
}

// xrayEmail mirrors xray.Email (the identity Xray reports in statistics).
func xrayEmail(u *db.User) string { return fmt.Sprintf("%d.%s", u.ID, u.Username) }

func hostPort(h string, port int) string {
	return net.JoinHostPort(strings.Trim(h, "[]"), strconv.Itoa(port))
}
