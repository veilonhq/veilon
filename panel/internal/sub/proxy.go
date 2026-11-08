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
)

// Proxy is a client-side description of one endpoint, independent of the
// output format.
type Proxy struct {
	InboundID   int64
	Name        string
	Protocol    string
	Server      string
	Port        int
	UUID        string
	Password    string
	Method      string
	Flow        string
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

	// Username for http, mixed and masque (the user's stats identity).
	Username string
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
		name := ib.Remark
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%s %d", name, used[name])
		}
		p.Name = name
		switch ib.Protocol {
		case "vless", "vmess":
			p.UUID = u.UUID
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
		case inbounds.HTTP, inbounds.Mixed, inbounds.Masque:
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
		switch ib.Security {
		case "tls":
			if ib.TLS != nil {
				p.SNI = ib.TLS.SNI
				p.ALPN = ib.TLS.ALPN
				p.Fingerprint = ib.TLS.Fingerprint
				p.Insecure = ib.TLS.AllowInsecure
				if p.Insecure && ib.TLS.CertID != nil {
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
		out = append(out, p)
	}
	return out
}

// xrayEmail mirrors xray.Email (the identity Xray reports in statistics).
func xrayEmail(u *db.User) string { return fmt.Sprintf("%d.%s", u.ID, u.Username) }

func hostPort(h string, port int) string {
	return net.JoinHostPort(strings.Trim(h, "[]"), strconv.Itoa(port))
}
