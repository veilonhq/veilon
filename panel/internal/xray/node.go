package xray

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// NodeConfig renders the Xray config a remote node runs: the same users,
// outbounds, routing and DNS as this server, but only the given inbounds.
// Certificate files are inlined (the node has no copy of them), TUN
// inbounds and the sing-box bridge are left out. apiPort is the node's
// local stats API port. The second result lists inbounds left out and why.
func NodeConfig(gdb *gorm.DB, dataDir string, inboundIDs []int64, apiPort int) ([]byte, []string, error) {
	in, err := LoadInput(gdb, dataDir, apiPort)
	if err != nil {
		return nil, nil, err
	}
	want := map[int64]bool{}
	for _, id := range inboundIDs {
		want[id] = true
	}
	kept := in.Inbounds[:0]
	for _, ib := range in.Inbounds {
		if !want[ib.ID] {
			continue
		}
		if inbounds.Engine(ib.Protocol) != inbounds.EngineXray && inbounds.NodeCapable(ib.Protocol) {
			continue // AmneziaWG / MTProto: the node's own engines run them
		}
		if !NodeCapable(ib.Protocol) {
			in.Skipped = append(in.Skipped, fmt.Sprintf("%s: %s inbounds cannot run on a node", ib.Remark, ib.Protocol))
			continue
		}
		kept = append(kept, ib)
	}
	in.Inbounds = kept
	in.BridgePort = 0
	in.TunOK = false
	b, err := Build(in)
	if err != nil {
		return nil, in.Skipped, err
	}
	var cfg any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, in.Skipped, err
	}
	if err := inlineFiles(cfg); err != nil {
		return nil, in.Skipped, err
	}
	if m, ok := cfg.(map[string]any); ok && len(in.ProxyAliases) > 0 {
		al := map[string]int64{}
		for email, name := range in.ProxyAliases {
			if id, ok := ParseEmail(email); ok {
				al[name] = id
			}
		}
		m[AliasKey] = al
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	return out, in.Skipped, err
}

// AliasKey is the top-level key of a node config that maps custom proxy
// usernames to user ids, so the agent attributes their traffic, online
// state and client IPs (Xray ignores unknown top-level keys). It is left
// out when no user has a custom proxy username, so such configs are
// byte-identical to older ones.
const AliasKey = "veilon_aliases"

// ConfigAliases reads the AliasKey table of a node config (nil if none).
func ConfigAliases(cfg []byte) map[string]int64 {
	var c map[string]json.RawMessage
	if json.Unmarshal(cfg, &c) != nil {
		return nil
	}
	raw, ok := c[AliasKey]
	if !ok {
		return nil
	}
	var m map[string]int64
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// NodeCapable reports whether inbounds of this protocol can be served by a
// node (see inbounds.NodeCapable).
func NodeCapable(protocol string) bool { return inbounds.NodeCapable(protocol) }

// inlineFiles replaces certificateFile / keyFile references with the file
// contents ("certificate" / "key" as line arrays), everywhere in the config.
func inlineFiles(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for _, pair := range [][2]string{{"certificateFile", "certificate"}, {"keyFile", "key"}} {
			p, ok := x[pair[0]].(string)
			if !ok {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read %s: %w", pair[0], err)
			}
			x[pair[1]] = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
			delete(x, pair[0])
		}
		for _, c := range x {
			if err := inlineFiles(c); err != nil {
				return err
			}
		}
	case []any:
		for _, c := range x {
			if err := inlineFiles(c); err != nil {
				return err
			}
		}
	}
	return nil
}
