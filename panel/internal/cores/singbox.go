package cores

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// SingboxVersion is the sing-box release Veilon is tested with.
const SingboxVersion = "1.14.4"

// Options configures an engine.
type Options struct {
	DB           *gorm.DB
	DataDir      string
	BinPath      string
	Version      string // release version or "latest"
	AutoDownload bool
	BridgePort   int // Xray loopback bridge port (sing-box)
	// OnLine receives every output line of the engine (sing-box: the
	// connection log feeds the connection guard).
	OnLine func(line string)
}

type obj = map[string]any

// SingboxConfig renders the sing-box server config for the TUIC inbounds.
// Authenticated traffic is handed to Xray's loopback bridge inbound
// (bridgePort) with the user's identity, so Xray applies the same routing
// rules and records exact per-user traffic. It returns nil when there is
// nothing to serve.
func SingboxConfig(in *xray.Input, bridgePort int) ([]byte, error) {
	var ibs, outs, rules []any
	outs = append(outs, obj{"type": "block", "tag": "block"})
	seenUser := map[int64]bool{}
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if inbounds.Engine(ib.Protocol) != inbounds.EngineSingbox {
			continue
		}
		if ib.TLS == nil || ib.TLS.CertID == nil {
			slog.Warn("skipping inbound without certificate", "inbound", ib.Remark)
			continue
		}
		cf, kf := certs.Paths(in.DataDir, *ib.TLS.CertID)
		if _, err := os.Stat(cf); err != nil {
			slog.Warn("skipping inbound: certificate files missing", "inbound", ib.Remark)
			continue
		}
		listen := ib.Listen
		if listen == "" {
			listen = "::"
		}
		users := []any{}
		for _, u := range in.Users[ib.ID] {
			name := xray.Email(&u)
			users = append(users, obj{"name": name, "uuid": u.UUID, "password": u.Password})
			if !seenUser[u.ID] {
				seenUser[u.ID] = true
				tag := fmt.Sprintf("u-%d", u.ID)
				outs = append(outs, obj{"type": "socks", "tag": tag, "server": "127.0.0.1", "server_port": bridgePort,
					"version": "5", "username": name, "password": u.Password})
				if cidrs := blockedCIDRs(in.Policy.BlockedIPs[name]); len(cidrs) > 0 {
					rules = append(rules, obj{"auth_user": []string{name}, "source_ip_cidr": cidrs, "action": "reject"})
				}
				rules = append(rules, obj{"auth_user": []string{name}, "outbound": tag})
			}
		}
		tls := obj{"enabled": true, "alpn": []string{"h3"}, "certificate_path": cf, "key_path": kf}
		if ib.TLS.SNI != "" {
			tls["server_name"] = ib.TLS.SNI
		}
		ibs = append(ibs, obj{
			"type": "tuic", "tag": ib.Tag(), "listen": listen, "listen_port": ib.Port,
			"users": users, "congestion_control": ib.Settings.Congestion, "tls": tls,
		})
	}
	if len(ibs) == 0 {
		return nil, nil
	}
	cfg := obj{
		"log":       obj{"level": "info", "timestamp": true},
		"inbounds":  ibs,
		"outbounds": outs,
		"route":     obj{"rules": rules, "final": "block"},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// blockedCIDRs turns blocked client IPs into host prefixes; invalid
// entries are skipped (a bad row must never break the config).
func blockedCIDRs(ips []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ips {
		p := net.ParseIP(strings.TrimSpace(s))
		if p == nil {
			continue
		}
		c := p.String() + "/128"
		if v4 := p.To4(); v4 != nil {
			c = v4.String() + "/32"
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// safeLine hands a line to fn; a panic there must not kill the reader
// goroutine (and with it the panel).
func safeLine(fn func(string), line string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("engine log hook panicked", "panic", r)
		}
	}()
	fn(line)
}

// Singbox is the sing-box engine. Its traffic is counted by Xray (bridge).
type Singbox struct {
	*Supervisor
	opt Options
}

// NewSingbox creates the sing-box engine.
func NewSingbox(opt Options) *Singbox {
	e := &Singbox{opt: opt}
	cfgPath := filepath.Join(opt.DataDir, "sing-box", "config.json")
	e.Supervisor = NewSupervisor(Spec{
		DB:         opt.DB,
		Name:       "sing-box",
		Protocols:  []string{inbounds.TUIC},
		Bin:        func() string { return opt.BinPath },
		Ensure:     e.ensure,
		Version:    func() string { return binVersion(opt.BinPath, "version", 2) },
		ConfigGlob: cfgPath,
		Plan: func() ([]Instance, error) {
			in, err := xray.LoadInput(opt.DB, opt.DataDir, 0)
			if err != nil {
				return nil, err
			}
			b, err := SingboxConfig(in, opt.BridgePort)
			if err != nil || b == nil {
				return nil, err
			}
			return []Instance{{Key: "main", Config: b, ConfigFile: cfgPath, Args: []string{"run", "-c", cfgPath}}}, nil
		},
		LineHook: func(_ string, _ uint64, line string) bool {
			if opt.OnLine != nil {
				safeLine(opt.OnLine, line)
			}
			return false
		},
	})
	return e
}

func (e *Singbox) ensure(ctx context.Context) error {
	if _, err := os.Stat(e.opt.BinPath); err == nil {
		return nil
	}
	if !e.opt.AutoDownload {
		return fmt.Errorf("sing-box binary not found at %s (auto-download disabled)", e.opt.BinPath)
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
	arch, err := goArchName(runtime.GOARCH)
	if err != nil {
		return err
	}
	ver := strings.TrimPrefix(e.opt.Version, "v")
	if ver == "" || ver == "latest" {
		tag, err := ResolveLatest(ctx, "SagerNet/sing-box")
		if err != nil {
			return err
		}
		ver = strings.TrimPrefix(tag, "v")
	}
	url := fmt.Sprintf("https://github.com/SagerNet/sing-box/releases/download/v%s/sing-box-%s-linux-%s.tar.gz", ver, ver, arch)
	return downloadTarBinary(ctx, url, "", "sing-box", e.opt.BinPath)
}

// counter turns cumulative counters into deltas, resetting when the
// process generation changes or a counter goes backwards.
type counter map[string]struct {
	gen      uint64
	up, down int64
}

func (c counter) delta(key string, gen uint64, up, down int64) (int64, int64) {
	last, ok := c[key]
	c[key] = struct {
		gen      uint64
		up, down int64
	}{gen, up, down}
	if !ok || last.gen != gen || up < last.up || down < last.down {
		return up, down
	}
	return up - last.up, down - last.down
}

// binVersion runs "<bin> <arg>" and returns field idx of the first line.
func binVersion(bin, arg string, idx int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, arg).CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	f := strings.Fields(line)
	if idx < len(f) {
		return strings.TrimPrefix(f[idx], "v")
	}
	return ""
}
