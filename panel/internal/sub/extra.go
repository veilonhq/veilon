package sub

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// extraURI returns share links for the protocols added after the v2 family.
// It returns "" for protocols without a share-link standard (AmneziaWG,
// MASQUE, ShadowTLS), which are delivered as config files / JSON instead.
func extraURI(p *Proxy) string {
	frag := "#" + fragment(p.Name)
	switch p.Protocol {
	case inbounds.Hysteria:
		q := url.Values{}
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if p.Insecure {
			q.Set("insecure", "1")
			if p.PinSHA256 != "" {
				q.Set("pinSHA256", p.PinSHA256)
			}
		}
		if p.ObfsPassword != "" {
			q.Set("obfs", "salamander")
			q.Set("obfs-password", p.ObfsPassword)
		}
		if p.HopPorts != "" {
			// Port hopping: the common "mport" parameter (the link's own
			// port stays the listen port for clients without hopping).
			q.Set("mport", p.HopPorts)
		}
		return "hysteria2://" + url.PathEscape(p.Password) + "@" + hostPort(p.Server, p.Port) + "/?" + q.Encode() + frag
	case inbounds.AnyTLS:
		// anytls://password@host:port/?sni=...&insecure=1 (the AnyTLS URI
		// scheme).
		q := url.Values{}
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if p.Insecure {
			q.Set("insecure", "1")
		}
		return "anytls://" + url.PathEscape(p.Password) + "@" + hostPort(p.Server, p.Port) + "/?" + q.Encode() + frag
	case inbounds.Naive:
		// naive+https://user:pass@host:port (NaiveProxy clients verify the
		// certificate: a self-signed one needs the sing-box JSON instead).
		q := ""
		if p.SNI != "" && p.SNI != strings.Trim(p.Server, "[]") {
			q = "?sni=" + url.QueryEscape(p.SNI)
		}
		return "naive+https://" + url.UserPassword(p.Username, p.Password).String() + "@" + hostPort(p.Server, p.Port) + q + frag
	case inbounds.TUIC:
		q := url.Values{}
		q.Set("congestion_control", p.Congestion)
		q.Set("udp_relay_mode", "native")
		q.Set("alpn", "h3")
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if p.Insecure {
			q.Set("allow_insecure", "1")
			q.Set("insecure", "1")
		}
		return "tuic://" + p.UUID + ":" + url.PathEscape(p.Password) + "@" + hostPort(p.Server, p.Port) + "?" + q.Encode() + frag
	case inbounds.HTTP:
		scheme := "http"
		q := ""
		if p.Security == "tls" {
			scheme = "https"
			if p.SNI != "" {
				q = "?sni=" + url.QueryEscape(p.SNI)
			}
		}
		return scheme + "://" + url.UserPassword(p.Username, p.Password).String() + "@" + hostPort(p.Server, p.Port) + q + frag
	case inbounds.Mixed, inbounds.Socks:
		// The common "socks://" share form: base64(user:pass) as userinfo,
		// none for a socks inbound without authentication.
		if p.NoAuth {
			return "socks://" + hostPort(p.Server, p.Port) + frag
		}
		ui := base64.RawURLEncoding.EncodeToString([]byte(p.Username + ":" + p.Password))
		return "socks://" + ui + "@" + hostPort(p.Server, p.Port) + frag
	case inbounds.WireGuard:
		w := p.WG
		q := url.Values{}
		q.Set("publickey", w.PeerPublicKey)
		if w.PresharedKey != "" {
			q.Set("presharedkey", w.PresharedKey)
		}
		q.Set("address", w.Address)
		q.Set("mtu", strconv.Itoa(w.MTU))
		if w.Keepalive > 0 {
			q.Set("keepalive", strconv.Itoa(w.Keepalive))
		}
		return "wireguard://" + url.PathEscape(w.PrivateKey) + "@" + hostPort(p.Server, p.Port) + "?" + q.Encode() + frag
	case inbounds.MTProto:
		q := url.Values{}
		q.Set("server", strings.Trim(p.Server, "[]"))
		q.Set("port", strconv.Itoa(p.Port))
		q.Set("secret", p.Secret)
		return "tg://proxy?" + q.Encode()
	}
	return ""
}

// ConfFile returns a WireGuard / AmneziaWG client configuration file (and a
// file name) for WG based proxies.
func ConfFile(p *Proxy) (name, body string, ok bool) {
	if p.WG == nil {
		return "", "", false
	}
	w := p.WG
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", w.PrivateKey)
	fmt.Fprintf(&b, "Address = %s\n", w.Address)
	if len(w.DNS) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(w.DNS, ", "))
	}
	if w.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", w.MTU)
	}
	if a := w.AWG; a != nil {
		fmt.Fprintf(&b, "Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\nH1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n",
			a.Jc, a.Jmin, a.Jmax, a.S1, a.S2, a.H1, a.H2, a.H3, a.H4)
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", w.PeerPublicKey)
	if w.PresharedKey != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", w.PresharedKey)
	}
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	fmt.Fprintf(&b, "Endpoint = %s\n", hostPort(p.Server, p.Port))
	if w.Keepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", w.Keepalive)
	} else {
		b.WriteString("PersistentKeepalive = 25\n")
	}
	n := safeFilename(p.Name)
	if strings.Trim(n, "_.-") == "" {
		n = fmt.Sprintf("veilon-%d", p.InboundID)
	}
	if len(n) > 15 {
		n = n[:15]
	}
	return n + ".conf", b.String(), true
}
