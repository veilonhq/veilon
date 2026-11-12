package veiloncore

import (
	"encoding/json"
	"net"
	"strings"
)

// Settings are the user's connection settings (from the app).
type Settings struct {
	MTU int `json:"mtu"`
	// RemoteDNS is resolved through the proxy: "tcp://1.1.1.1",
	// "https://1.1.1.1/dns-query", "tls://8.8.8.8", "udp://8.8.8.8" or an IP.
	RemoteDNS string `json:"remoteDns"`
	// DirectDNS is used for direct (bypassed) domains and for resolving
	// proxy server names.
	DirectDNS   string `json:"directDns"`
	DisableQUIC bool   `json:"disableQuic"`
	BypassIran  bool   `json:"bypassIran"`
	BypassLAN   bool   `json:"bypassLan"`
	BlockAds    bool   `json:"blockAds"`
	IPv6        bool   `json:"ipv6"`
	// Domain routing: lists of domains / suffixes ("example.com",
	// "domain:example.com", "full:x.com", "keyword:abc", "regexp:..") or
	// CIDRs.
	ProxyRules  []string `json:"proxyRules"`
	DirectRules []string `json:"directRules"`
	BlockRules  []string `json:"blockRules"`
	// Per-app: "off", "include" (only listed apps use the VPN), "exclude".
	PerAppMode     string   `json:"perAppMode"`
	PerAppPackages []string `json:"perAppPackages"`
	// AppPackage is the app's own package (always kept off the VPN so the
	// cores' sockets reach the network directly).
	AppPackage string `json:"appPackage"`
	LogLevel   string `json:"logLevel"`
	// Local mixed (SOCKS5+HTTP) proxy port exposed on 127.0.0.1, 0 = off.
	MixedPort int `json:"mixedPort"`
	// Directory holding rule-set files (geoip-ir.srs, geosite-ir.srs,
	// geosite-category-ads-all.srs).
	AssetDir string `json:"assetDir"`
	// DataDir is a writable, app-owned absolute directory for core state
	// (sing-box cache.db, working directory). Android: filesDir/core.
	// Empty = parent of AssetDir. See paths.go.
	DataDir string `json:"dataDir"`
	// Core is the engine for V2Ray-family profiles: "auto" (default),
	// "sing-box" or "xray". A profile's own "core" wins. See ResolveCore.
	Core string `json:"core"`
	// TempDir is a writable absolute temp directory (Android: cacheDir).
	TempDir string `json:"tempDir"`
	// Stack / ExperimentalStack are accepted for compatibility with older
	// app builds but ignored: the tun always uses gVisor (see tunStack).
	Stack             string `json:"stack"`
	ExperimentalStack bool   `json:"experimentalStack"`
	// FullHandshakePing: latency test opens a new proxy connection per
	// sample (includes handshakes). Default false: median of 3 HTTP round
	// trips over one kept-alive connection (see measureRTT).
	FullHandshakePing bool `json:"fullHandshakePing"`
	// TLS fragment for TCP+TLS proxies (anti-DPI).
	TLSFragment bool `json:"tlsFragment"`
}

// DefaultSettings returns sane defaults.
func DefaultSettings() Settings {
	return Settings{
		MTU: 9000, RemoteDNS: "https://1.1.1.1/dns-query", DirectDNS: "8.8.8.8",
		BypassLAN: true, PerAppMode: "off", LogLevel: "warn", Stack: "gvisor", Core: "auto",
	}
}

func parseSettings(s string) (Settings, error) {
	st := DefaultSettings()
	if strings.TrimSpace(s) == "" {
		st.normalizePaths()
		return st, nil
	}
	if err := json.Unmarshal([]byte(s), &st); err != nil {
		return st, err
	}
	if st.MTU < 1280 || st.MTU > 65535 {
		st.MTU = 9000
	}
	if st.RemoteDNS == "" {
		st.RemoteDNS = "https://1.1.1.1/dns-query"
	}
	if st.DirectDNS == "" {
		st.DirectDNS = "8.8.8.8"
	}
	// Only gVisor is supported; stored stack choices are ignored.
	st.Stack, st.ExperimentalStack = "gvisor", false
	switch st.LogLevel {
	case "trace", "debug", "info", "warn", "error":
	default:
		st.LogLevel = "warn"
	}
	if st.Core = normCorePref(st.Core); st.Core == "" {
		st.Core = "auto"
	}
	st.normalizePaths()
	return st, nil
}

// dnsServer turns a user DNS string into a sing-box (1.12+) DNS server.
func dnsServer(tag, spec, detour string) map[string]any {
	spec = strings.TrimSpace(spec)
	typ, rest := "udp", spec
	if i := strings.Index(spec, "://"); i > 0 {
		typ, rest = strings.ToLower(spec[:i]), spec[i+3:]
	}
	o := map[string]any{"tag": tag}
	switch typ {
	case "https", "h3":
		host := rest
		path := ""
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			host, path = rest[:j], rest[j:]
		}
		o["type"] = typ
		h, port := splitServerPort(host)
		o["server"] = h
		if port != 0 {
			o["server_port"] = port
		}
		if path != "" && path != "/dns-query" {
			o["path"] = path
		}
		if net.ParseIP(h) == nil {
			o["domain_resolver"] = "dns-direct"
		}
	case "tls", "tcp", "udp", "quic":
		o["type"] = typ
		h, port := splitServerPort(strings.TrimSuffix(rest, "/"))
		o["server"] = h
		if port != 0 {
			o["server_port"] = port
		}
		if net.ParseIP(h) == nil {
			o["domain_resolver"] = "dns-direct"
		}
	default:
		o["type"] = "udp"
		o["server"] = rest
	}
	if detour != "" {
		o["detour"] = detour
	}
	return o
}

func splitServerPort(s string) (string, int) {
	if h, p, err := splitHostPort(s); err == nil {
		return h, p
	}
	return strings.Trim(s, "[]"), 0
}
