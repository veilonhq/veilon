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
	out, err := json.MarshalIndent(cfg, "", "  ")
	return out, in.Skipped, err
}

// NodeCapable reports whether inbounds of this protocol can be served by a
// node (Xray-core inbounds, except TUN which needs the host's network).
func NodeCapable(protocol string) bool {
	return inbounds.Engine(protocol) == inbounds.EngineXray && protocol != "tun"
}

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
