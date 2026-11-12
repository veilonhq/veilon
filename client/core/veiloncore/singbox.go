package veiloncore

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type obj = map[string]any

const (
	tagProxy  = "proxy"
	tagDirect = "direct"
	// Rule-set files expected in Settings.AssetDir.
	rsGeoIPIR    = "geoip-ir.srs"
	rsGeositeIR  = "geosite-ir.srs"
	rsGeositeAds = "geosite-category-ads-all.srs"
)

// singboxTLS renders outbound TLS for p.
func singboxTLS(p *Profile, defaultALPN []string) obj {
	t := obj{"enabled": true}
	sni := p.SNI
	if sni == "" && p.Host != "" && (p.Network == "ws" || p.Network == "httpupgrade" || p.Network == "h2") {
		sni = p.Host
	}
	if sni == "" && net.ParseIP(p.Server) == nil {
		sni = p.Server
	}
	if sni != "" {
		t["server_name"] = sni
	}
	alpn := p.ALPN
	if len(alpn) == 0 {
		alpn = defaultALPN
	}
	if len(alpn) > 0 {
		t["alpn"] = alpn
	}
	if p.Security == "reality" {
		fp := p.Fingerprint
		if fp == "" || fp == "random" || fp == "randomized" {
			fp = "chrome"
		}
		t["utls"] = obj{"enabled": true, "fingerprint": fp}
		t["reality"] = obj{"enabled": true, "public_key": p.PublicKey, "short_id": p.ShortID}
		delete(t, "alpn")
		return t
	}
	if p.Insecure {
		if p.PinSPKI != "" {
			t["certificate_public_key_sha256"] = []string{p.PinSPKI}
		} else {
			t["insecure"] = true
		}
	}
	if fp := p.Fingerprint; fp != "" && fp != "none" && !isQUICProto(p) {
		if fp == "randomized" {
			fp = "random"
		}
		t["utls"] = obj{"enabled": true, "fingerprint": fp}
	}
	return t
}

func isQUICProto(p *Profile) bool {
	return p.Type == TypeHysteria2 || p.Type == TypeTUIC
}

// wsPathEarlyData splits "/path?ed=2048" into path and early-data size.
func wsPathEarlyData(path string) (string, int) {
	u, err := url.Parse(path)
	if err != nil {
		return path, 0
	}
	q := u.Query()
	ed, _ := strconv.Atoi(q.Get("ed"))
	if ed <= 0 {
		return path, 0
	}
	q.Del("ed")
	u.RawQuery = q.Encode()
	return u.String(), ed
}

func singboxTransport(p *Profile) (obj, error) {
	switch p.Network {
	case "", "tcp":
		if p.HeaderType == "http" {
			t := obj{"type": "http", "method": "GET"}
			if p.Path != "" {
				t["path"] = p.Path
			}
			if p.Host != "" {
				t["host"] = splitList(p.Host)
			}
			if p.Security == "tls" {
				return nil, fmt.Errorf("tcp http header with TLS is not supported")
			}
			return t, nil
		}
		return nil, nil
	case "ws":
		path, ed := wsPathEarlyData(firstNonEmpty(p.Path, "/"))
		t := obj{"type": "ws", "path": path}
		if p.Host != "" {
			t["headers"] = obj{"Host": p.Host}
		}
		if ed > 0 {
			t["max_early_data"] = ed
			t["early_data_header_name"] = "Sec-WebSocket-Protocol"
		}
		return t, nil
	case "httpupgrade":
		t := obj{"type": "httpupgrade", "path": firstNonEmpty(p.Path, "/")}
		if p.Host != "" {
			t["host"] = p.Host
		}
		return t, nil
	case "grpc":
		return obj{"type": "grpc", "service_name": p.ServiceName}, nil
	case "h2":
		t := obj{"type": "http", "path": firstNonEmpty(p.Path, "/")}
		if p.Host != "" {
			t["host"] = splitList(p.Host)
		}
		return t, nil
	case "quic":
		return obj{"type": "quic"}, nil
	}
	return nil, fmt.Errorf("transport %q is not supported by sing-box", p.Network)
}

// singboxOutbound renders the sing-box outbound (or wireguard endpoint) for
// a profile carried by sing-box itself.
func singboxOutbound(p *Profile, tag string) (o obj, isEndpoint bool, err error) {
	o = obj{"tag": tag, "server": p.Server, "server_port": p.Port}
	switch p.Type {
	case TypeVLESS:
		o["type"] = "vless"
		o["uuid"] = p.UUID
		if p.Flow != "" {
			o["flow"] = p.Flow
		}
		o["packet_encoding"] = "xudp"
	case TypeVMess:
		o["type"] = "vmess"
		o["uuid"] = p.UUID
		o["security"] = firstNonEmpty(p.VMessSecurity, "auto")
		o["alter_id"] = p.AlterID
		o["packet_encoding"] = "xudp"
	case TypeTrojan:
		o["type"] = "trojan"
		o["password"] = p.Password
	case TypeShadowsocks:
		o["type"] = "shadowsocks"
		o["method"] = p.Method
		o["password"] = p.Password
		switch p.Plugin {
		case "":
		case "obfs-local", "simple-obfs", "obfs":
			o["plugin"] = "obfs-local"
			o["plugin_opts"] = p.PluginOpts
		case "v2ray-plugin":
			o["plugin"] = "v2ray-plugin"
			o["plugin_opts"] = p.PluginOpts
		default:
			return nil, false, fmt.Errorf("shadowsocks plugin %q is not supported", p.Plugin)
		}
	case TypeHysteria2:
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
		if p.Ports != "" {
			var ranges []string
			for _, r := range splitList(p.Ports) {
				r = strings.ReplaceAll(r, "-", ":")
				if !strings.Contains(r, ":") {
					r = r + ":" + r
				}
				ranges = append(ranges, r)
			}
			o["server_ports"] = ranges
			delete(o, "server_port")
		}
		o["tls"] = singboxTLS(p, []string{"h3"})
		return o, false, nil
	case TypeTUIC:
		o["type"] = "tuic"
		o["uuid"] = p.UUID
		o["password"] = p.Password
		if p.Congestion != "" {
			o["congestion_control"] = p.Congestion
		}
		if p.UDPRelayMode != "" {
			o["udp_relay_mode"] = p.UDPRelayMode
		}
		o["tls"] = singboxTLS(p, []string{"h3"})
		return o, false, nil
	case TypeHTTP:
		o["type"] = "http"
		if p.Username != "" {
			o["username"] = p.Username
		}
		if p.Password != "" {
			o["password"] = p.Password
		}
		if p.Security == "tls" {
			o["tls"] = singboxTLS(p, nil)
		}
		return o, false, nil
	case TypeSOCKS:
		// Plain SOCKS5 only: SOCKS over TLS is carried by Xray (CoreOf).
		o["type"] = "socks"
		o["version"] = "5"
		if p.Username != "" {
			o["username"] = p.Username
		}
		if p.Password != "" {
			o["password"] = p.Password
		}
		return o, false, nil
	case TypeWireGuard:
		w := p.WG
		peer := obj{"address": p.Server, "port": p.Port, "public_key": w.PeerPublicKey,
			"allowed_ips": []string{"0.0.0.0/0", "::/0"}}
		if w.PresharedKey != "" {
			peer["pre_shared_key"] = w.PresharedKey
		}
		if w.Keepalive > 0 {
			peer["persistent_keepalive_interval"] = w.Keepalive
		}
		if len(w.Reserved) == 3 {
			peer["reserved"] = w.Reserved
		}
		ep := obj{"type": "wireguard", "tag": tag, "address": w.Address, "private_key": w.PrivateKey, "peers": []any{peer}}
		mtu := w.MTU
		if mtu <= 0 {
			mtu = 1280
		}
		ep["mtu"] = mtu
		return ep, true, nil
	default:
		return nil, false, fmt.Errorf("profile type %q is not handled by sing-box", p.Type)
	}
	// v2 family: transport + security.
	tr, err := singboxTransport(p)
	if err != nil {
		return nil, false, err
	}
	if tr != nil {
		o["transport"] = tr
	}
	switch p.Security {
	case "tls", "reality":
		var alpn []string
		if p.Network == "grpc" {
			alpn = []string{"h2"}
		}
		o["tls"] = singboxTLS(p, alpn)
	}
	return o, false, nil
}

// socksOutbound is a sing-box SOCKS5 outbound to a local sidecar.
func socksOutbound(tag string, port int, user, pass string, tcpOnly bool) obj {
	o := obj{"type": "socks", "tag": tag, "server": "127.0.0.1", "server_port": port, "version": "5"}
	if user != "" {
		o["username"] = user
		o["password"] = pass
	}
	if tcpOnly {
		o["network"] = "tcp"
	}
	return o
}

// routeRule turns user rules into sing-box rule fields.
func routeRule(rules []string) obj {
	r := obj{}
	add := func(k, v string) {
		l, _ := r[k].([]string)
		r[k] = append(l, v)
	}
	for _, raw := range rules {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		low := strings.ToLower(s)
		switch {
		case strings.HasPrefix(low, "full:"):
			add("domain", s[5:])
		case strings.HasPrefix(low, "domain:"):
			add("domain_suffix", s[7:])
		case strings.HasPrefix(low, "keyword:"):
			add("domain_keyword", s[8:])
		case strings.HasPrefix(low, "regexp:"):
			add("domain_regex", s[7:])
		case strings.HasPrefix(low, "geosite:"), strings.HasPrefix(low, "geoip:"):
			// handled via bundled rule-sets (only "ir" is bundled)
			if strings.HasSuffix(low, ":ir") {
				add("_rule_set_ir", low)
			}
		default:
			if _, _, err := net.ParseCIDR(s); err == nil {
				add("ip_cidr", s)
			} else if ip := net.ParseIP(s); ip != nil {
				if ip.To4() != nil {
					add("ip_cidr", s+"/32")
				} else {
					add("ip_cidr", s+"/128")
				}
			} else {
				add("domain_suffix", strings.TrimPrefix(s, "."))
			}
		}
	}
	return r
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// buildPlan describes how a profile maps to sing-box outbounds.
type buildPlan struct {
	outbounds []any
	endpoints []any
	tcpOnly   bool // proxy cannot carry UDP (SSH / DNS-tunnel SOCKS backends)
	// ownSockets: a sidecar dials the server with sockets the core cannot
	// protect (AmneziaWG bind, DNS-tunnel clients, SSH transport wrapping).
	// Such sessions need the app excluded from the VPN, i.e. gVisor.
	ownSockets bool
	// internal is an authenticated local proxy used by the app itself
	// (subscription updates through the tunnel).
	internal *localProxy
}

type localProxy struct {
	Port int
	User string
	Pass string
}

// buildSingboxConfig renders the complete sing-box configuration.
// withTun=false renders a tun-less config (latency tests).
func buildSingboxConfig(plan *buildPlan, st Settings, withTun bool) obj {
	cfg := obj{}
	cfg["log"] = obj{"level": st.LogLevel, "timestamp": true}
	// sing-box always opens its cache file when a platform log writer is
	// set; give it an absolute path in app storage (never "cache.db" in cwd).
	cfg["experimental"] = obj{"cache_file": obj{"enabled": false, "path": singboxCachePath(st)}}

	assetDir := assetDirOf(st)
	haveRS := func(name string) bool { return assetDir != "" && fileExists(filepath.Join(assetDir, name)) }
	var ruleSets []any
	addRS := func(tag, file string) {
		ruleSets = append(ruleSets, obj{"type": "local", "tag": tag, "format": "binary", "path": filepath.Join(assetDir, file)})
	}

	remoteSpec := st.RemoteDNS
	if plan.tcpOnly {
		// UDP cannot cross the proxy; use DNS over TCP / HTTPS.
		if strings.HasPrefix(remoteSpec, "udp://") || !strings.Contains(remoteSpec, "://") {
			h := strings.TrimPrefix(remoteSpec, "udp://")
			remoteSpec = "tcp://" + h
		}
		if strings.HasPrefix(remoteSpec, "quic://") || strings.HasPrefix(remoteSpec, "h3://") {
			remoteSpec = "tcp://1.1.1.1"
		}
	}
	directDNS := dnsServer("dns-direct", st.DirectDNS, "")
	if _, ok := directDNS["domain_resolver"]; ok {
		directDNS["domain_resolver"] = "dns-bootstrap"
	}
	servers := []any{
		dnsServer("dns-remote", remoteSpec, tagProxy),
		directDNS,
		obj{"tag": "dns-bootstrap", "type": "udp", "server": "8.8.8.8"},
	}
	var dnsRules []any
	var routeRules []any
	routeRules = append(routeRules, obj{"action": "sniff"})
	if withTun {
		routeRules = append(routeRules, obj{"protocol": "dns", "action": "hijack-dns"})
	}
	if st.BypassLAN {
		routeRules = append(routeRules, obj{"ip_is_private": true, "outbound": tagDirect})
	}
	if st.DisableQUIC {
		routeRules = append(routeRules, obj{"network": "udp", "port": 443, "action": "reject"})
	}
	if st.BlockAds && haveRS(rsGeositeAds) {
		addRS("geosite-ads", rsGeositeAds)
		routeRules = append(routeRules, obj{"rule_set": "geosite-ads", "action": "reject"})
		dnsRules = append(dnsRules, obj{"rule_set": "geosite-ads", "action": "predefined", "rcode": "NXDOMAIN"})
	}
	used := map[string]bool{}
	userRule := func(list []string, outbound string, reject bool) {
		r := routeRule(list)
		irs, _ := r["_rule_set_ir"].([]string)
		delete(r, "_rule_set_ir")
		if len(irs) > 0 {
			var sets []string
			for _, x := range irs {
				if strings.HasPrefix(x, "geoip") && haveRS(rsGeoIPIR) {
					sets = append(sets, "geoip-ir")
				}
				if strings.HasPrefix(x, "geosite") && haveRS(rsGeositeIR) {
					sets = append(sets, "geosite-ir")
				}
				if strings.HasPrefix(x, "geosite") {
					l, _ := r["domain_suffix"].([]string)
					r["domain_suffix"] = append(l, "ir")
				}
			}
			if len(sets) > 0 {
				used[sets[0]] = true
				if len(sets) > 1 {
					used[sets[1]] = true
				}
				r["rule_set"] = sets
			}
		}
		if len(r) == 0 {
			return
		}
		rr := obj{}
		for k, v := range r {
			rr[k] = v
		}
		if reject {
			rr["action"] = "reject"
		} else {
			rr["outbound"] = outbound
		}
		routeRules = append(routeRules, rr)
		// Domain parts drive DNS server choice.
		dr := obj{}
		for _, k := range []string{"domain", "domain_suffix", "domain_keyword", "domain_regex"} {
			if v, ok := r[k]; ok {
				dr[k] = v
			}
		}
		if rs, ok := r["rule_set"].([]string); ok {
			var dsets []string
			for _, s := range rs {
				if s == "geosite-ir" {
					dsets = append(dsets, s)
				}
			}
			if len(dsets) > 0 {
				dr["rule_set"] = dsets
			}
		}
		if len(dr) > 0 {
			if reject {
				dr["action"] = "predefined"
				dr["rcode"] = "NXDOMAIN"
			} else if outbound == tagDirect {
				dr["server"] = "dns-direct"
			} else {
				dr["server"] = "dns-remote"
			}
			dnsRules = append(dnsRules, dr)
		}
	}
	userRule(st.BlockRules, "", true)
	userRule(st.DirectRules, tagDirect, false)
	userRule(st.ProxyRules, tagProxy, false)
	if st.BypassIran {
		r := obj{"domain_suffix": []string{"ir"}, "outbound": tagDirect}
		dr := obj{"domain_suffix": []string{"ir"}, "server": "dns-direct"}
		var sets []string
		if haveRS(rsGeositeIR) {
			sets = append(sets, "geosite-ir")
		}
		if haveRS(rsGeoIPIR) {
			sets = append(sets, "geoip-ir")
		}
		routeRules = append(routeRules, r)
		dnsRules = append(dnsRules, dr)
		if len(sets) > 0 {
			routeRules = append(routeRules, obj{"rule_set": sets, "outbound": tagDirect})
			for _, s := range sets {
				used[s] = true
			}
			if haveRS(rsGeositeIR) {
				dnsRules = append(dnsRules, obj{"rule_set": []string{"geosite-ir"}, "server": "dns-direct"})
			}
		}
	}
	if used["geosite-ir"] {
		addRS("geosite-ir", rsGeositeIR)
	}
	if used["geoip-ir"] {
		addRS("geoip-ir", rsGeoIPIR)
	}

	dnsObj := obj{"servers": servers, "final": "dns-remote"}
	if len(dnsRules) > 0 {
		dnsObj["rules"] = dnsRules
	}
	if !st.IPv6 {
		dnsObj["strategy"] = "ipv4_only"
	}
	cfg["dns"] = dnsObj

	var inbounds []any
	if withTun {
		t := obj{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30"}, "mtu": st.MTU,
			"auto_route": true, "stack": effectiveStack(plan, st)}
		if st.IPv6 {
			t["address"] = []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}
		}
		if st.BypassLAN {
			t["route_exclude_address"] = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10", "fc00::/7", "fe80::/10"}
		}
		switch st.PerAppMode {
		case "include":
			if len(st.PerAppPackages) > 0 {
				t["include_package"] = st.PerAppPackages
			}
		case "exclude":
			if len(st.PerAppPackages) > 0 {
				t["exclude_package"] = st.PerAppPackages
			}
		}
		inbounds = append(inbounds, t)
	}
	if st.MixedPort > 0 {
		inbounds = append(inbounds, obj{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": st.MixedPort})
	}
	if plan.internal != nil {
		inbounds = append(inbounds, obj{"type": "mixed", "tag": "internal-in", "listen": "127.0.0.1", "listen_port": plan.internal.Port,
			"users": []any{obj{"username": plan.internal.User, "password": plan.internal.Pass}}})
	}
	if len(inbounds) > 0 {
		cfg["inbounds"] = inbounds
	}
	outs := append([]any{}, plan.outbounds...)
	outs = append(outs, obj{"type": "direct", "tag": tagDirect})
	cfg["outbounds"] = outs
	if len(plan.endpoints) > 0 {
		cfg["endpoints"] = plan.endpoints
	}
	route := obj{"rules": routeRules, "final": tagProxy, "default_domain_resolver": "dns-direct"}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	}
	if (withTun && effectiveStack(plan, st) != "gvisor") || (!withTun && selfProtect.Load() != nil) {
		// App inside the VPN: sing-box must protect every socket it dials.
		route["auto_detect_interface"] = true
	}
	cfg["route"] = route
	return cfg
}

func b64std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// tunStack is the TCP/IP stack for the VPN tun. Always gVisor: the app's
// own package is excluded from the VPN (so every core's sockets bypass the
// tun without per-socket protect), and with that the kernel-based "system"
// stack - also the TCP half of "mixed" - cannot work on Android: it NATs
// each app's TCP SYN to a listener on 172.19.0.1 owned by the app's UID,
// and the kernel routes that listener's SYN-ACK by the app's UID, i.e. via
// the underlying network instead of back into the tun, so no TCP
// connection from another app is ever established (DNS over UDP still
// works). gVisor terminates TCP in user space and writes replies straight
// to the tun fd. Proof: TestTunAndroidRouting (system/mixed fail, gvisor
// passes). The "stack" setting is kept for compatibility only.
//
// Since 1.0.3 "system" / "mixed" can work, opt-in (Settings.
// ExperimentalStack, not in the UI): like sing-box's Android client (SFA),
// the app is then NOT excluded from the VPN, so the kernel routes the
// system stack's TCP replies (owned by the app's UID) back into the tun,
// and every socket the cores open to reach the server is protected
// instead (VpnService.protect): sing-box via auto_detect_interface →
// PlatformInterface.AutoDetectInterfaceControl, Xray via its dialer
// controller (xrayProtectController), the core's own dialers via
// protectControl. Proof: TestTunAndroidRouting (sing-box/xray ×
// system/mixed/gvisor) and TestTunAndroidRoutingAppIncluded.
//
// Since 1.0.4 the app offers gVisor only: Settings.Stack and
// ExperimentalStack are ignored and the tun always uses gVisor.
func tunStack(Settings) string {
	return "gvisor"
}

// effectiveStack is tunStack, except that sessions whose sidecar sockets
// cannot be protected stay on gVisor (with the app excluded).
func effectiveStack(plan *buildPlan, st Settings) string {
	s := tunStack(st)
	if s != "gvisor" && plan != nil && plan.ownSockets {
		pushLogLevel("warn", "tun: stack "+s+" is not available for this protocol; using gvisor")
		return "gvisor"
	}
	return s
}

// selfIncluded reports whether a rendered config keeps the app inside the
// VPN (non-gVisor tun stack).
func selfIncluded(cfg obj) bool {
	t := tunInbound(cfg)
	if t == nil {
		return false
	}
	s, _ := t["stack"].(string)
	return s != "" && s != "gvisor"
}
