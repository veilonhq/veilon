package inbounds

import (
	"encoding/base64"
	"net"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Advanced holds the optional security and transport settings of an
// inbound. Every zero value means "as before": an inbound without advanced
// settings renders exactly the config older panels wrote. It is stored in
// the settings column (no schema change; older panels ignore the key).
type Advanced struct {
	// Sniffing refines the inbound's sniffing switch (Xray inbounds).
	Sniffing *SniffOpts `json:"sniffing,omitempty"`
	// Sockopt: PROXY protocol and socket options (VLESS/VMess/Trojan/SS).
	Sockopt *Sockopt `json:"sockopt,omitempty"`
	// Fallbacks for VLESS/Trojan over RAW with TLS or Reality.
	Fallbacks []Fallback `json:"fallbacks,omitempty"`
	// TLS and Reality extras.
	TLS     *TLSExtra     `json:"tls,omitempty"`
	Reality *RealityExtra `json:"reality,omitempty"`
	// TCP is the HTTP header obfuscation of RAW (security none).
	TCP *TCPHeader `json:"tcp,omitempty"`
	// Transport knobs.
	WS       *WSExtra       `json:"ws,omitempty"`
	GRPC     *GRPCExtra     `json:"grpc,omitempty"`
	XHTTP    *XHTTPExtra    `json:"xhttp,omitempty"`
	KCP      *KCPExtra      `json:"kcp,omitempty"`
	Hysteria *HysteriaExtra `json:"hysteria,omitempty"`
	TUIC     *TUICExtra     `json:"tuic,omitempty"`
	SS       *SSExtra       `json:"ss,omitempty"`
	// Masks are server-side Xray finalmask entries.
	Masks *Masks `json:"masks,omitempty"`
	// Reverse is the VLESS reverse proxy (portal side).
	Reverse *Reverse `json:"reverse,omitempty"`
}

// SniffOpts are the sniffing details. Empty DestOverride = http, tls, quic.
type SniffOpts struct {
	DestOverride    []string `json:"dest_override,omitempty"`
	RouteOnly       bool     `json:"route_only,omitempty"`
	MetadataOnly    bool     `json:"metadata_only,omitempty"`
	DomainsExcluded []string `json:"domains_excluded,omitempty"`
}

// DefaultSniffDest is what sniffing has always looked at.
var DefaultSniffDest = []string{"http", "tls", "quic"}

// Sockopt are inbound socket options (Xray "sockopt").
type Sockopt struct {
	AcceptProxyProtocol bool   `json:"accept_proxy_protocol,omitempty"`
	TCPFastOpen         bool   `json:"tcp_fast_open,omitempty"`
	KeepAliveInterval   int    `json:"keepalive_interval,omitempty"`
	KeepAliveIdle       int    `json:"keepalive_idle,omitempty"`
	Mark                int    `json:"mark,omitempty"`
	V6Only              bool   `json:"v6only,omitempty"`
	Congestion          string `json:"congestion,omitempty"`
}

// Fallback sends connections that are not the proxy protocol elsewhere.
type Fallback struct {
	Name string `json:"name,omitempty"`
	ALPN string `json:"alpn,omitempty"`
	Path string `json:"path,omitempty"`
	Dest string `json:"dest"`
	Xver int    `json:"xver,omitempty"`
}

// TLSExtra are extra TLS settings (security tls).
type TLSExtra struct {
	MinVersion       string `json:"min_version,omitempty"`
	MaxVersion       string `json:"max_version,omitempty"`
	CipherSuites     string `json:"cipher_suites,omitempty"`
	RejectUnknownSNI bool   `json:"reject_unknown_sni,omitempty"`
	// ECHServerKeys (secret) enables Encrypted Client Hello; the client
	// ECHConfigList is derived from it.
	ECHServerKeys string `json:"ech_server_keys,omitempty"`
	ECHConfigList string `json:"ech_config_list,omitempty"`
	// PinCert puts the certificate's SHA-256 in client configs even for a
	// certificate from a CA (pinned clients refuse anything else).
	PinCert bool `json:"pin_cert,omitempty"`
}

// RealityExtra are extra Reality settings.
type RealityExtra struct {
	// MLDSA65Seed (secret) makes the server sign its certificate with
	// ML-DSA-65; clients verify with MLDSA65Verify (derived).
	MLDSA65Seed   string         `json:"mldsa65_seed,omitempty"`
	MLDSA65Verify string         `json:"mldsa65_verify,omitempty"`
	MinClientVer  string         `json:"min_client_ver,omitempty"`
	MaxClientVer  string         `json:"max_client_ver,omitempty"`
	MaxTimeDiff   int            `json:"max_time_diff,omitempty"`
	LimitUpload   *LimitFallback `json:"limit_fallback_upload,omitempty"`
	LimitDownload *LimitFallback `json:"limit_fallback_download,omitempty"`
}

// LimitFallback rate-limits traffic Reality passes to its target.
type LimitFallback struct {
	AfterBytes       int64 `json:"after_bytes,omitempty"`
	BytesPerSec      int64 `json:"bytes_per_sec,omitempty"`
	BurstBytesPerSec int64 `json:"burst_bytes_per_sec,omitempty"`
}

// TCPHeader is the HTTP header obfuscation of RAW ("header": {"type":
// "http"}).
type TCPHeader struct {
	Type string   `json:"type"`
	Host []string `json:"host,omitempty"`
	Path []string `json:"path,omitempty"`
}

// WSExtra: WebSocket / HTTPUpgrade. Headers are client request headers.
type WSExtra struct {
	Headers         map[string]string `json:"headers,omitempty"`
	HeartbeatPeriod int               `json:"heartbeat_period,omitempty"`
}

// GRPCExtra: gRPC options.
type GRPCExtra struct {
	UserAgent           string `json:"user_agent,omitempty"`
	IdleTimeout         int    `json:"idle_timeout,omitempty"`
	HealthCheckTimeout  int    `json:"health_check_timeout,omitempty"`
	PermitWithoutStream bool   `json:"permit_without_stream,omitempty"`
	InitialWindowsSize  int    `json:"initial_windows_size,omitempty"`
}

// XHTTPExtra: XHTTP options. Ranges are "n" or "a-b".
type XHTTPExtra struct {
	XPaddingBytes        string         `json:"x_padding_bytes,omitempty"`
	NoSSEHeader          bool           `json:"no_sse_header,omitempty"`
	ScMaxEachPostBytes   string         `json:"sc_max_each_post_bytes,omitempty"`
	ScMaxBufferedPosts   int            `json:"sc_max_buffered_posts,omitempty"`
	ScStreamUpServerSecs string         `json:"sc_stream_up_server_secs,omitempty"`
	Xmux                 *Xmux          `json:"xmux,omitempty"`
	Download             *XHTTPDownload `json:"download,omitempty"`
}

// Xmux is the client-side XHTTP connection reuse.
type Xmux struct {
	MaxConcurrency   string `json:"max_concurrency,omitempty"`
	MaxConnections   string `json:"max_connections,omitempty"`
	CMaxReuseTimes   string `json:"c_max_reuse_times,omitempty"`
	HMaxRequestTimes string `json:"h_max_request_times,omitempty"`
	HMaxReusableSecs string `json:"h_max_reusable_secs,omitempty"`
	HKeepAlivePeriod int    `json:"h_keep_alive_period,omitempty"`
}

// XHTTPDownload makes clients download through another address (e.g. a
// CDN) while uploading to the inbound itself.
type XHTTPDownload struct {
	Address string `json:"address"`
	Port    int    `json:"port,omitempty"`
	SNI     string `json:"sni,omitempty"`
	Host    string `json:"host,omitempty"`
}

// KCPExtra: mKCP options (0 = Xray default). Header/seed are carried by
// the "mkcp-legacy" finalmask.
type KCPExtra struct {
	MTU              int    `json:"mtu,omitempty"`
	TTI              int    `json:"tti,omitempty"`
	UplinkCapacity   int    `json:"uplink_capacity,omitempty"`
	DownlinkCapacity int    `json:"downlink_capacity,omitempty"`
	CwndMultiplier   int    `json:"cwnd_multiplier,omitempty"`
	MaxSendingWindow int    `json:"max_sending_window,omitempty"`
	Header           string `json:"header,omitempty"`
	HeaderDomain     string `json:"header_domain,omitempty"`
	Seed             string `json:"seed,omitempty"`
}

// HysteriaExtra: Hysteria 2 server options.
type HysteriaExtra struct {
	// Congestion: "" (auto), "bbr" (ignores the client's bandwidth),
	// "brutal", "force-brutal" (needs BrutalUpMbps).
	Congestion     string      `json:"congestion,omitempty"`
	BrutalUpMbps   int         `json:"brutal_up_mbps,omitempty"`
	BrutalDownMbps int         `json:"brutal_down_mbps,omitempty"`
	UDPIdleTimeout int         `json:"udp_idle_timeout,omitempty"`
	Masquerade     *Masquerade `json:"masquerade,omitempty"`
}

// Masquerade answers plain HTTP/3 requests: "proxy" (a site) or "string".
type Masquerade struct {
	Type        string            `json:"type"`
	URL         string            `json:"url,omitempty"`
	RewriteHost bool              `json:"rewrite_host,omitempty"`
	Content     string            `json:"content,omitempty"`
	StatusCode  int               `json:"status_code,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// TUICExtra: TUIC (sing-box) options.
type TUICExtra struct {
	ZeroRTT      bool   `json:"zero_rtt,omitempty"`
	Heartbeat    int    `json:"heartbeat,omitempty"` // seconds
	UDPRelayMode string `json:"udp_relay_mode,omitempty"`
}

// SSExtra: Shadowsocks options.
type SSExtra struct {
	NoUDP bool `json:"no_udp,omitempty"`
}

// Masks are server-side finalmask entries (one-sided, so clients need
// nothing): TCP fragment of the server's first packets, UDP noise.
type Masks struct {
	Fragment *FragmentMask `json:"fragment,omitempty"`
	Noise    *NoiseMask    `json:"noise,omitempty"`
}

// FragmentMask splits the server's first TCP writes.
type FragmentMask struct {
	Packets string `json:"packets"`
	Length  string `json:"length"`
	Delay   string `json:"delay,omitempty"`
}

// NoiseMask sends random datagrams ahead of UDP traffic.
type NoiseMask struct {
	Rand  string `json:"rand"`
	Delay string `json:"delay,omitempty"`
}

// Reverse is a VLESS reverse proxy (portal side). A bridge (e.g. a server
// abroad) dials this inbound with UUID; then the users of the routed
// inbounds leave the internet through the bridge.
type Reverse struct {
	Enabled bool   `json:"enabled"`
	UUID    string `json:"uuid"`
	// RouteSelf sends this inbound's own users through the bridge.
	RouteSelf bool `json:"route_self"`
	// RouteInboundIDs: other inbounds whose users go through the bridge.
	RouteInboundIDs []int64 `json:"route_inbound_ids,omitempty"`
}

// ReverseTag is the outbound tag a bridge's connection creates on Xray.
func ReverseTag(inboundID int64) string { return "reverse-" + strconv.FormatInt(inboundID, 10) }

// ReverseEmail is the stats identity of the bridge connection. "@" never
// appears in a custom proxy username, so it can never be a user's key.
func ReverseEmail(inboundID int64) string { return "@reverse-" + strconv.FormatInt(inboundID, 10) }

// ReverseOn reports an active reverse proxy.
func (v *View) ReverseOn() bool {
	return v.Advanced != nil && v.Advanced.Reverse != nil && v.Advanced.Reverse.Enabled && v.Protocol == VLESS
}

// Allowed values.
var (
	SniffDests       = []string{"http", "tls", "quic", "fakedns"}
	TLSVersions      = []string{"1.0", "1.1", "1.2", "1.3"}
	TCPCongestions   = []string{"bbr", "cubic", "reno"}
	KCPHeaders       = []string{"none", "srtp", "utp", "wechat-video", "dtls", "wireguard", "dns"}
	HyCongestions    = []string{"bbr", "brutal", "force-brutal"}
	TUICRelayModes   = []string{"native", "quic"}
	TLSFingerprints  = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized", "randomizednoalpn"}
	cipherRe         = regexp.MustCompile(`^[A-Z0-9_]+(:[A-Z0-9_]+)*$`)
	headerNameRe     = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]{1,64}$")
	rangeRe          = regexp.MustCompile(`^\d{1,9}(-\d{1,9})?$`)
	clientVerRe      = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){0,2}$`)
	b64urlRe         = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	kcpHeaderToXray  = map[string]string{"srtp": "srtp", "utp": "utp", "wechat-video": "wechat", "dtls": "dtls", "wireguard": "wireguard", "dns": "dns"}
	errHeaderInvalid = "header names must be HTTP tokens and values must not contain line breaks"
)

// KCPXrayHeader maps a share-link mKCP header type to the mkcp-legacy name.
func KCPXrayHeader(h string) string { return kcpHeaderToXray[h] }

func cleanList(l []string) []string {
	var out []string
	for _, x := range l {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func checkHeaders(h map[string]string) (map[string]string, error) {
	if len(h) == 0 {
		return nil, nil
	}
	if len(h) > 32 {
		return nil, verr("at most 32 headers")
	}
	out := map[string]string{}
	for k, v := range h {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !headerNameRe.MatchString(k) || strings.ContainsAny(v, "\r\n") || len(v) > 1024 {
			return nil, verr("%s", errHeaderInvalid)
		}
		out[k] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func checkRange(s, what string, min, max int) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if s == "" {
		return "", nil
	}
	if !rangeRe.MatchString(s) {
		return "", verr("%s must be a number or a range like 100-1000", what)
	}
	a, b, _ := strings.Cut(s, "-")
	x, _ := strconv.Atoi(a)
	y := x
	if b != "" {
		y, _ = strconv.Atoi(b)
	}
	if x > y || x < min || y > max {
		return "", verr("%s must be between %d and %d (low-high)", what, min, max)
	}
	return s, nil
}

func between(n, min, max int) bool { return n >= min && n <= max }

// isTCPNet reports a TCP-based transport (everything but mKCP).
func isTCPNet(n string) bool { return n != "kcp" }

// normalizeAdvanced validates v.Advanced against the inbound and drops
// what does not apply. It leaves nil when nothing is set.
func normalizeAdvanced(v *View) error {
	a := v.Advanced
	if a == nil {
		return nil
	}
	v2 := IsV2(v.Protocol)
	xr := Engine(v.Protocol) == EngineXray

	// Sniffing details (only while sniffing is on).
	if s := a.Sniffing; s != nil {
		if !v.Sniffing || !xr {
			a.Sniffing = nil
		} else {
			var d []string
			seen := map[string]bool{}
			for _, x := range s.DestOverride {
				x = strings.ToLower(strings.TrimSpace(x))
				if x == "" || seen[x] {
					continue
				}
				if !in(x, SniffDests) {
					return verr("sniffing.dest_override must be from %s", strings.Join(SniffDests, ", "))
				}
				seen[x] = true
				d = append(d, x)
			}
			sort.SliceStable(d, func(i, j int) bool { return idx(d[i], SniffDests) < idx(d[j], SniffDests) })
			if strings.Join(d, ",") == strings.Join(DefaultSniffDest, ",") {
				d = nil
			}
			s.DestOverride = d
			s.DomainsExcluded = cleanList(s.DomainsExcluded)
			if len(s.DomainsExcluded) > 200 {
				return verr("sniffing.domains_excluded: at most 200 entries")
			}
			for _, x := range s.DomainsExcluded {
				if len(x) > 253 || strings.ContainsAny(x, " \t\"") {
					return verr("sniffing.domains_excluded: %q is not a domain", x)
				}
			}
			if s.MetadataOnly && len(s.DestOverride) > 0 && !(len(s.DestOverride) == 1 && s.DestOverride[0] == "fakedns") {
				return verr("sniffing.metadata_only works with dest_override fakedns only")
			}
			if s.MetadataOnly && len(s.DestOverride) == 0 {
				s.DestOverride = []string{"fakedns"}
			}
			if len(s.DestOverride) == 0 && !s.RouteOnly && !s.MetadataOnly && len(s.DomainsExcluded) == 0 {
				a.Sniffing = nil
			}
		}
	}

	// Socket options.
	if s := a.Sockopt; s != nil {
		if !v2 {
			a.Sockopt = nil
		} else {
			s.Congestion = strings.ToLower(strings.TrimSpace(s.Congestion))
			if s.Congestion != "" && !in(s.Congestion, TCPCongestions) {
				return verr("sockopt.congestion must be one of %s", strings.Join(TCPCongestions, ", "))
			}
			if !between(s.KeepAliveInterval, 0, 86400) || !between(s.KeepAliveIdle, 0, 86400) {
				return verr("sockopt keepalive values must be 0-86400 seconds")
			}
			if s.Mark < 0 {
				return verr("sockopt.mark must be 0 or more")
			}
			if s.AcceptProxyProtocol && !isTCPNet(v.Network) {
				return verr("PROXY protocol needs a TCP transport (not mKCP)")
			}
			if !isTCPNet(v.Network) {
				s.TCPFastOpen, s.KeepAliveIdle, s.KeepAliveInterval, s.Congestion = false, 0, 0, ""
			}
			if *s == (Sockopt{}) {
				a.Sockopt = nil
			}
		}
	}

	// Fallbacks.
	if len(a.Fallbacks) > 0 {
		if (v.Protocol != VLESS && v.Protocol != Trojan) || v.Network != "tcp" || (v.Security != "tls" && v.Security != "reality") {
			return verr("fallbacks need VLESS or Trojan over tcp (RAW) with TLS or Reality")
		}
		if v.VLESSEnc.On() {
			return verr("fallbacks cannot be used together with VLESS Encryption")
		}
		if len(a.Fallbacks) > 32 {
			return verr("at most 32 fallbacks")
		}
		seen := map[string]bool{}
		for i := range a.Fallbacks {
			f := &a.Fallbacks[i]
			f.Name, f.ALPN, f.Path, f.Dest = strings.TrimSpace(f.Name), strings.TrimSpace(f.ALPN), strings.TrimSpace(f.Path), strings.TrimSpace(f.Dest)
			if f.Dest == "" {
				return verr("fallback %d: dest is required (a port, host:port or a unix socket path)", i+1)
			}
			if !validFallbackDest(f.Dest) {
				return verr("fallback %d: dest %q must be a port, host:port or an absolute unix socket path", i+1, f.Dest)
			}
			if f.Path != "" && !strings.HasPrefix(f.Path, "/") {
				return verr("fallback %d: path must start with /", i+1)
			}
			if f.ALPN != "" && !in(f.ALPN, []string{"h2", "http/1.1"}) {
				return verr("fallback %d: alpn must be h2 or http/1.1", i+1)
			}
			if f.Xver < 0 || f.Xver > 2 {
				return verr("fallback %d: xver must be 0, 1 or 2", i+1)
			}
			k := f.Name + "|" + f.ALPN + "|" + f.Path
			if seen[k] {
				return verr("fallback %d: the same name, alpn and path are used twice", i+1)
			}
			seen[k] = true
		}
	} else {
		a.Fallbacks = nil
	}

	// TLS extras.
	if t := a.TLS; t != nil {
		if v.Security != "tls" || !v2 {
			a.TLS = nil
		} else {
			t.MinVersion, t.MaxVersion = strings.TrimSpace(t.MinVersion), strings.TrimSpace(t.MaxVersion)
			if t.MinVersion != "" && !in(t.MinVersion, TLSVersions) || t.MaxVersion != "" && !in(t.MaxVersion, TLSVersions) {
				return verr("tls versions must be one of %s", strings.Join(TLSVersions, ", "))
			}
			if t.MinVersion != "" && t.MaxVersion != "" && t.MinVersion > t.MaxVersion {
				return verr("tls min_version must not be above max_version")
			}
			t.CipherSuites = strings.ReplaceAll(strings.TrimSpace(t.CipherSuites), " ", "")
			if t.CipherSuites != "" && !cipherRe.MatchString(t.CipherSuites) {
				return verr("tls.cipher_suites must be Go cipher suite names joined by ':'")
			}
			t.ECHServerKeys = strings.TrimSpace(t.ECHServerKeys)
			t.ECHConfigList = ""
			if t.ECHServerKeys != "" {
				list, err := ECHConfigListFromKeys(t.ECHServerKeys)
				if err != nil {
					return verr("tls.ech_server_keys: %v", err)
				}
				t.ECHConfigList = list
			}
			if *t == (TLSExtra{}) {
				a.TLS = nil
			}
		}
	}

	// Reality extras.
	if r := a.Reality; r != nil {
		if v.Security != "reality" {
			a.Reality = nil
		} else {
			r.MLDSA65Seed = strings.TrimSpace(r.MLDSA65Seed)
			r.MLDSA65Verify = ""
			if r.MLDSA65Seed != "" {
				pub, err := MLDSA65Verify(r.MLDSA65Seed)
				if err != nil {
					return verr("reality.mldsa65_seed: %v", err)
				}
				if v.Reality != nil && r.MLDSA65Seed == v.Reality.PrivateKey {
					return verr("reality.mldsa65_seed must differ from the private key")
				}
				r.MLDSA65Verify = pub
			}
			r.MinClientVer, r.MaxClientVer = strings.TrimSpace(r.MinClientVer), strings.TrimSpace(r.MaxClientVer)
			for _, x := range []string{r.MinClientVer, r.MaxClientVer} {
				if x == "" {
					continue
				}
				if !clientVerRe.MatchString(x) {
					return verr("reality client versions look like 26.3.27")
				}
				for _, p := range strings.Split(x, ".") {
					if n, _ := strconv.Atoi(p); n > 255 {
						return verr("reality client version parts must be below 256")
					}
				}
			}
			if r.MaxTimeDiff < 0 || r.MaxTimeDiff > 86400000 {
				return verr("reality.max_time_diff must be 0-86400000 ms")
			}
			for _, l := range []**LimitFallback{&r.LimitUpload, &r.LimitDownload} {
				if *l == nil {
					continue
				}
				if (*l).AfterBytes < 0 || (*l).BytesPerSec < 0 || (*l).BurstBytesPerSec < 0 {
					return verr("reality fallback limits must not be negative")
				}
				if **l == (LimitFallback{}) {
					*l = nil
				}
			}
			if *r == (RealityExtra{}) {
				a.Reality = nil
			}
		}
	}

	// HTTP header obfuscation.
	if h := a.TCP; h != nil {
		h.Type = strings.ToLower(strings.TrimSpace(h.Type))
		if h.Type == "" || h.Type == "none" || !v2 || v.Network != "tcp" {
			a.TCP = nil
		} else {
			if h.Type != "http" {
				return verr("tcp header type must be none or http")
			}
			if v.Security != "none" {
				return verr("HTTP header obfuscation works without TLS/Reality only")
			}
			if v.Flow != "" {
				return verr("HTTP header obfuscation cannot be used with Vision")
			}
			h.Host, h.Path = cleanList(h.Host), cleanList(h.Path)
			for _, p := range h.Path {
				if !strings.HasPrefix(p, "/") {
					return verr("tcp header paths must start with /")
				}
			}
			for _, x := range append(append([]string{}, h.Host...), h.Path...) {
				if strings.ContainsAny(x, ", \r\n") {
					return verr("tcp header host/path must not contain spaces or commas")
				}
			}
		}
	}

	// WebSocket / HTTPUpgrade.
	if w := a.WS; w != nil {
		if !v2 || (v.Network != "ws" && v.Network != "httpupgrade") {
			a.WS = nil
		} else {
			hs, err := checkHeaders(w.Headers)
			if err != nil {
				return err
			}
			w.Headers = hs
			if v.Network != "ws" {
				w.HeartbeatPeriod = 0
			}
			if !between(w.HeartbeatPeriod, 0, 3600) {
				return verr("ws heartbeat_period must be 0-3600 seconds")
			}
			if len(w.Headers) == 0 && w.HeartbeatPeriod == 0 {
				a.WS = nil
			}
		}
	}

	// gRPC.
	if g := a.GRPC; g != nil {
		if !v2 || v.Network != "grpc" {
			a.GRPC = nil
		} else {
			g.UserAgent = strings.TrimSpace(g.UserAgent)
			if strings.ContainsAny(g.UserAgent, "\r\n") || len(g.UserAgent) > 256 {
				return verr("grpc.user_agent is invalid")
			}
			if !between(g.IdleTimeout, 0, 86400) || !between(g.HealthCheckTimeout, 0, 3600) || g.InitialWindowsSize < 0 {
				return verr("grpc timeouts must be 0-86400 seconds and the window size 0 or more")
			}
			if *g == (GRPCExtra{}) {
				a.GRPC = nil
			}
		}
	}

	// XHTTP.
	if x := a.XHTTP; x != nil {
		if !v2 || v.Network != "xhttp" {
			a.XHTTP = nil
		} else {
			var err error
			if x.XPaddingBytes, err = checkRange(x.XPaddingBytes, "xhttp x_padding_bytes", 0, 1<<20); err != nil {
				return err
			}
			if x.ScMaxEachPostBytes, err = checkRange(x.ScMaxEachPostBytes, "xhttp sc_max_each_post_bytes", 1, 1<<26); err != nil {
				return err
			}
			if x.ScStreamUpServerSecs, err = checkRange(x.ScStreamUpServerSecs, "xhttp sc_stream_up_server_secs", 1, 86400); err != nil {
				return err
			}
			if !between(x.ScMaxBufferedPosts, 0, 100000) {
				return verr("xhttp sc_max_buffered_posts must be 0-100000")
			}
			if m := x.Xmux; m != nil {
				for _, f := range []*string{&m.MaxConcurrency, &m.MaxConnections, &m.CMaxReuseTimes, &m.HMaxRequestTimes, &m.HMaxReusableSecs} {
					if *f, err = checkRange(*f, "xmux values", 0, 1<<30); err != nil {
						return err
					}
				}
				if m.MaxConcurrency != "" && m.MaxConnections != "" {
					return verr("xmux: set max_concurrency or max_connections, not both")
				}
				if !between(m.HKeepAlivePeriod, 0, 86400) {
					return verr("xmux h_keep_alive_period must be 0-86400")
				}
				if *m == (Xmux{}) {
					x.Xmux = nil
				}
			}
			if d := x.Download; d != nil {
				d.Address, d.SNI, d.Host = strings.TrimSpace(d.Address), strings.TrimSpace(d.SNI), strings.TrimSpace(d.Host)
				if d.Address == "" && d.Port == 0 && d.SNI == "" && d.Host == "" {
					x.Download = nil
				} else {
					if d.Address == "" {
						return verr("xhttp download address is required")
					}
					if net.ParseIP(strings.Trim(d.Address, "[]")) == nil && !hostRe.MatchString(d.Address) {
						return verr("xhttp download address must be an IP address or a host name")
					}
					if !between(d.Port, 0, 65535) {
						return verr("xhttp download port must be 1-65535")
					}
					if d.SNI != "" && !hostRe.MatchString(d.SNI) || d.Host != "" && !hostRe.MatchString(d.Host) {
						return verr("xhttp download sni/host must be host names")
					}
					if v.Transport.Mode == "stream-one" {
						return verr("xhttp download through another address needs mode auto, packet-up or stream-up")
					}
				}
			}
			if *x == (XHTTPExtra{}) {
				a.XHTTP = nil
			}
		}
	}

	// mKCP.
	if k := a.KCP; k != nil {
		if !v2 || v.Network != "kcp" {
			a.KCP = nil
		} else {
			k.Header = strings.ToLower(strings.TrimSpace(k.Header))
			if k.Header == "none" {
				k.Header = ""
			}
			if k.Header != "" && !in(k.Header, KCPHeaders) {
				return verr("kcp.header must be one of %s", strings.Join(KCPHeaders, ", "))
			}
			k.HeaderDomain = strings.TrimSpace(k.HeaderDomain)
			if k.Header != "dns" {
				k.HeaderDomain = ""
			} else if k.HeaderDomain != "" && !hostRe.MatchString(k.HeaderDomain) {
				return verr("kcp.header_domain must be a host name")
			}
			k.Seed = strings.TrimSpace(k.Seed)
			if len(k.Seed) > 128 || strings.ContainsAny(k.Seed, "\r\n") {
				return verr("kcp.seed is too long")
			}
			if k.MTU != 0 && !between(k.MTU, 576, 1460) {
				return verr("kcp.mtu must be 576-1460")
			}
			if k.TTI != 0 && !between(k.TTI, 10, 100) {
				return verr("kcp.tti must be 10-100 ms")
			}
			if !between(k.UplinkCapacity, 0, 10000) || !between(k.DownlinkCapacity, 0, 10000) {
				return verr("kcp capacities must be 0-10000 MB/s")
			}
			if !between(k.CwndMultiplier, 0, 100) || !between(k.MaxSendingWindow, 0, 1<<24) {
				return verr("kcp cwnd_multiplier must be 0-100 and max_sending_window 0 or more")
			}
			if k.MaxSendingWindow != 0 && k.MaxSendingWindow < max(k.MTU, 1350) {
				return verr("kcp.max_sending_window must be at least the MTU")
			}
			if *k == (KCPExtra{}) {
				a.KCP = nil
			}
		}
	}

	// Hysteria 2.
	if h := a.Hysteria; h != nil {
		if v.Protocol != Hysteria {
			a.Hysteria = nil
		} else {
			h.Congestion = strings.ToLower(strings.TrimSpace(h.Congestion))
			if h.Congestion != "" && !in(h.Congestion, HyCongestions) {
				return verr("hysteria.congestion must be empty (auto), bbr, brutal or force-brutal")
			}
			if !between(h.BrutalUpMbps, 0, 100000) || !between(h.BrutalDownMbps, 0, 100000) {
				return verr("hysteria brutal speeds must be 0-100000 Mbps")
			}
			if h.Congestion == "force-brutal" && h.BrutalUpMbps == 0 {
				return verr("hysteria force-brutal needs brutal_up_mbps")
			}
			if h.Congestion == "bbr" || h.Congestion == "" {
				h.BrutalUpMbps, h.BrutalDownMbps = 0, 0
			}
			if h.UDPIdleTimeout != 0 && !between(h.UDPIdleTimeout, 2, 600) {
				return verr("hysteria.udp_idle_timeout must be 2-600 seconds")
			}
			if m := h.Masquerade; m != nil {
				m.Type = strings.ToLower(strings.TrimSpace(m.Type))
				switch m.Type {
				case "", "404":
					h.Masquerade = nil
				case "proxy":
					m.URL = strings.TrimSpace(m.URL)
					if !strings.HasPrefix(m.URL, "http://") && !strings.HasPrefix(m.URL, "https://") || len(m.URL) > 2048 || strings.ContainsAny(m.URL, " \r\n") {
						return verr("hysteria masquerade url must start with http:// or https://")
					}
					m.Content, m.StatusCode, m.Headers = "", 0, nil
				case "string":
					if len(m.Content) > 65536 {
						return verr("hysteria masquerade content is too long")
					}
					if m.StatusCode != 0 && !between(m.StatusCode, 100, 599) {
						return verr("hysteria masquerade status_code must be 100-599")
					}
					hs, err := checkHeaders(m.Headers)
					if err != nil {
						return err
					}
					m.Headers, m.URL, m.RewriteHost = hs, "", false
				default:
					return verr("hysteria masquerade type must be proxy or string")
				}
			}
			if *h == (HysteriaExtra{}) {
				a.Hysteria = nil
			}
		}
	}

	// TUIC.
	if t := a.TUIC; t != nil {
		if v.Protocol != TUIC {
			a.TUIC = nil
		} else {
			t.UDPRelayMode = strings.ToLower(strings.TrimSpace(t.UDPRelayMode))
			if t.UDPRelayMode == "native" {
				t.UDPRelayMode = ""
			}
			if t.UDPRelayMode != "" && !in(t.UDPRelayMode, TUICRelayModes) {
				return verr("tuic.udp_relay_mode must be native or quic")
			}
			if !between(t.Heartbeat, 0, 600) {
				return verr("tuic.heartbeat must be 0-600 seconds")
			}
			if *t == (TUICExtra{}) {
				a.TUIC = nil
			}
		}
	}

	// Shadowsocks.
	if s := a.SS; s != nil && (v.Protocol != Shadowsocks || !s.NoUDP) {
		a.SS = nil
	}

	// Server-side masks.
	if m := a.Masks; m != nil {
		if !v2 && v.Protocol != Hysteria {
			a.Masks = nil
		} else {
			if f := m.Fragment; f != nil {
				f.Packets = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(f.Packets), " ", ""))
				if f.Packets == "" && f.Length == "" && f.Delay == "" {
					m.Fragment = nil
				} else {
					if tcp, _ := Transports(v); !tcp {
						return verr("a TCP fragment mask needs a TCP transport")
					}
					if f.Packets == "" {
						f.Packets = "tlshello"
					}
					if f.Packets != "tlshello" {
						if _, err := checkRange(f.Packets, "fragment packets", 1, 100); err != nil {
							return err
						}
					}
					var err error
					if f.Length, err = checkRange(f.Length, "fragment length", 1, 16384); err != nil {
						return err
					}
					if f.Length == "" {
						return verr("fragment length is required (e.g. 100-200)")
					}
					if f.Delay, err = checkRange(f.Delay, "fragment delay", 0, 10000); err != nil {
						return err
					}
				}
			}
			if n := m.Noise; n != nil {
				var err error
				if n.Rand, err = checkRange(n.Rand, "noise rand", 1, 1400); err != nil {
					return err
				}
				if n.Delay, err = checkRange(n.Delay, "noise delay", 0, 10000); err != nil {
					return err
				}
				if n.Rand == "" {
					m.Noise = nil
				} else if !(v.Network == "kcp" || v.Protocol == Hysteria) {
					return verr("a UDP noise mask needs mKCP or Hysteria 2")
				}
			}
			if m.Fragment == nil && m.Noise == nil {
				a.Masks = nil
			}
		}
	}

	// Reverse proxy.
	if r := a.Reverse; r != nil {
		if v.Protocol != VLESS || !r.Enabled {
			a.Reverse = nil
		} else {
			r.UUID = strings.TrimSpace(r.UUID)
			if r.UUID == "" {
				r.UUID = uuid.NewString()
			}
			if _, err := uuid.Parse(r.UUID); err != nil {
				return verr("reverse.uuid must be a UUID")
			}
			r.UUID = strings.ToLower(r.UUID)
			ids := r.RouteInboundIDs[:0]
			seen := map[int64]bool{}
			for _, id := range r.RouteInboundIDs {
				if id > 0 && id != v.ID && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			r.RouteInboundIDs = ids
			if len(ids) == 0 {
				r.RouteInboundIDs = nil
			}
		}
	}

	if a.empty() {
		v.Advanced = nil
	}
	return nil
}

func (a *Advanced) empty() bool {
	return a.Sniffing == nil && a.Sockopt == nil && len(a.Fallbacks) == 0 && a.TLS == nil && a.Reality == nil &&
		a.TCP == nil && a.WS == nil && a.GRPC == nil && a.XHTTP == nil && a.KCP == nil && a.Hysteria == nil &&
		a.TUIC == nil && a.SS == nil && a.Masks == nil && a.Reverse == nil
}

func idx(s string, l []string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return len(l)
}

func validFallbackDest(d string) bool {
	if n, err := strconv.Atoi(d); err == nil {
		return n >= 1 && n <= 65535
	}
	if strings.HasPrefix(d, "@") || filepath.IsAbs(d) {
		return len(d) > 1 && len(d) < 108 && !strings.ContainsAny(d, " \r\n")
	}
	h, p, err := net.SplitHostPort(d)
	if err != nil || h == "" {
		return false
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535 && (net.ParseIP(h) != nil || hostRe.MatchString(h) || h == "localhost")
}

// b64urlOK reports unpadded base64url of n bytes.
func b64urlOK(s string, n int) bool {
	if !b64urlRe.MatchString(s) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == n
}
