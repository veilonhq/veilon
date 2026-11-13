package inbounds

// The choices the inbound wizard offers, shared by the web wizard
// (web/src/lib/protocols.ts mirrors them), the Telegram bot wizard and
// Normalize. TestWizardRulesGolden writes them to testdata/wizard_rules.json
// and the web test (tests/wizardRules.test.ts) checks the TypeScript side
// against the same file, so the two tables cannot drift apart.

// RealityNetworks are the transports Reality can carry.
var RealityNetworks = []string{"tcp", "grpc", "xhttp"}

// Choice lists of the wizard.
var (
	// TLSALPNs are the ALPN values of a VLESS/VMess/Trojan/Shadowsocks TLS inbound.
	TLSALPNs = []string{"h3", "h2", "http/1.1"}
	// MasqueALPNs: MASQUE runs over HTTP/3 and/or HTTP/2.
	MasqueALPNs = []string{"h3", "h2"}
	// GRPCModes are the gRPC transport modes.
	GRPCModes = []string{"gun", "multi"}
	// TUICCongestions are the TUIC congestion controls.
	TUICCongestions = []string{"bbr", "cubic", "new_reno"}
	// TunnelNetworks are the networks a tunnel (dokodemo-door) forwards.
	TunnelNetworks = []string{"tcp", "udp", "tcp,udp"}
	// Flows are the VLESS flows ("" = none).
	Flows = []string{"", "xtls-rprx-vision"}
)

// NetworksFor returns the transports the admin can pick for a protocol
// (nil: the protocol has no transport choice).
func NetworksFor(protocol string) []string {
	if IsV2(protocol) {
		return Networks
	}
	return nil
}

// RealityAllowed reports whether Reality can secure protocol over network.
func RealityAllowed(protocol, network string) bool {
	return (protocol == VLESS || protocol == Trojan) && in(network, RealityNetworks)
}

// SecuritiesFor returns the securities the admin can pick for a protocol
// over network (one entry: nothing to choose).
func SecuritiesFor(protocol, network string) []string {
	switch {
	case IsV2(protocol):
		if RealityAllowed(protocol, network) {
			return []string{"none", "tls", "reality"}
		}
		return []string{"none", "tls"}
	case protocol == HTTP:
		return []string{"none", "tls"}
	case TLSRequired(protocol):
		return []string{"tls"}
	}
	return []string{"none"}
}

// FlowAllowed reports whether Vision (xtls-rprx-vision) can be chosen: VLESS
// with VLESS Encryption on any transport, or VLESS over tcp with TLS/Reality.
func FlowAllowed(protocol, network, security string, encOn bool) bool {
	return protocol == VLESS && (encOn || (network == "tcp" && security != "none"))
}

// SniffingApplies reports whether the sniffing switch means something for
// the protocol.
func SniffingApplies(protocol string) bool {
	return protocol != TUN && protocol != Tunnel && protocol != AmneziaWG && protocol != MTProto
}

// HasPort reports whether the inbound listens on a port (TUN does not).
func HasPort(protocol string) bool { return protocol != TUN }

// WizardRules is the table the golden file holds.
type WizardRules struct {
	Protocols []string `json:"protocols"`
	// Per protocol: transports, securities per transport, Vision per
	// transport/security (and with VLESS Encryption), sniffing, port.
	Per map[string]ProtocolRules `json:"per"`
	// Option lists.
	Options map[string][]string `json:"options"`
}

// ProtocolRules are the wizard choices of one protocol.
type ProtocolRules struct {
	Networks    []string            `json:"networks"`
	Securities  map[string][]string `json:"securities"`
	Flow        []string            `json:"flow"`
	Sniffing    bool                `json:"sniffing"`
	Port        bool                `json:"port"`
	NodeCapable bool                `json:"node_capable"`
	TLSOnly     bool                `json:"tls_required"`
}

// Rules builds the wizard table.
func Rules() WizardRules {
	out := WizardRules{Protocols: Protocols, Per: map[string]ProtocolRules{}, Options: map[string][]string{
		"networks":         Networks,
		"reality_networks": RealityNetworks,
		"ss_methods":       SSMethods,
		"shadowtls_ss":     ShadowTLSMethods,
		"xhttp_modes":      XHTTPModes,
		"grpc_modes":       GRPCModes,
		"tls_alpn":         TLSALPNs,
		"masque_alpn":      MasqueALPNs,
		"tls_fingerprints": TLSFingerprints,
		"reality_fps":      RealityFingerprints,
		"tuic_congestion":  TUICCongestions,
		"tunnel_networks":  TunnelNetworks,
		"enc_auths":        EncAuths,
		"enc_modes":        EncModes,
		"enc_rtts":         EncRTTs,
	}}
	for _, p := range Protocols {
		nets := NetworksFor(p)
		keys := nets
		if keys == nil {
			keys = []string{""}
		}
		pr := ProtocolRules{Networks: nets, Securities: map[string][]string{}, Flow: []string{}, Sniffing: SniffingApplies(p), Port: HasPort(p), NodeCapable: NodeCapable(p), TLSOnly: TLSRequired(p)}
		if pr.Networks == nil {
			pr.Networks = []string{}
		}
		for _, n := range keys {
			pr.Securities[n] = SecuritiesFor(p, n)
			for _, s := range SecuritiesFor(p, n) {
				for _, enc := range []bool{false, true} {
					if p != VLESS && enc {
						continue
					}
					if FlowAllowed(p, n, s, enc) {
						k := n + "/" + s
						if enc {
							k += "/enc"
						}
						pr.Flow = append(pr.Flow, k)
					}
				}
			}
		}
		out.Per[p] = pr
	}
	return out
}
