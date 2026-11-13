package sub

import (
	"encoding/json"
	"errors"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// ReverseBridgeInTag is the inbound tag a bridge gives the traffic that
// arrives through its reverse tunnel (routed straight to "direct").
const ReverseBridgeInTag = "reverse-in"

// PrivateCIDRs are the networks a bridge never opens to its portal.
var PrivateCIDRs = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.168.0.0/16", "224.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10"}

// ErrNoReverse: the inbound has no reverse proxy.
var ErrNoReverse = errors.New("the inbound has no reverse proxy")

// ReverseBridgeTargets lists where a bridge can connect: the inbound's
// proxies with the bridge identity (main server and/or nodes, hosts).
func ReverseBridgeTargets(ib *inbounds.View, s db.Settings, reqHost string, ex Extras) ([]Proxy, error) {
	if !ib.ReverseOn() {
		return nil, ErrNoReverse
	}
	bridge := &db.User{ID: 0, Username: "bridge", UUID: ib.Advanced.Reverse.UUID}
	ps := BuildProxiesX(bridge, []inbounds.View{*ib}, s, reqHost, ex)
	if len(ps) == 0 {
		return nil, errors.New("the inbound has no reachable address")
	}
	return ps, nil
}

// ReverseBridgeConfig renders a complete Xray config for the bridge server
// (e.g. a server abroad): it dials the portal (this inbound) and sends
// everything that comes back through the tunnel to the internet.
func ReverseBridgeConfig(p *Proxy) ([]byte, error) {
	out := xrayOutbound(p)
	st, _ := out["settings"].(obj)
	vn, _ := st["vnext"].([]any)
	if len(vn) != 1 {
		return nil, errors.New("unexpected outbound")
	}
	srv := vn[0].(obj)
	u := srv["users"].([]any)[0].(obj)
	// Xray takes "reverse" only in the flat VLESS outbound form.
	flat := obj{"address": srv["address"], "port": srv["port"], "id": u["id"], "encryption": u["encryption"],
		"reverse": obj{"tag": ReverseBridgeInTag}}
	if f, ok := u["flow"]; ok {
		flat["flow"] = f
	}
	out["settings"] = flat
	out["tag"] = "portal"
	cfg := obj{
		"remarks": "Veilon reverse bridge: " + p.Name,
		"log":     obj{"loglevel": "warning"},
		"outbounds": []any{
			out,
			// Xray blocks everything that arrives through a reverse tunnel
			// unless allowed: allow the internet, keep the bridge's own
			// private networks closed to the portal.
			obj{"tag": "direct", "protocol": "freedom", "settings": obj{"finalRules": []any{
				obj{"action": "block", "ip": PrivateCIDRs},
				obj{"action": "allow"},
			}}},
		},
		"routing": obj{"rules": []any{
			obj{"type": "field", "inboundTag": []string{ReverseBridgeInTag}, "outboundTag": "direct"},
		}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}
