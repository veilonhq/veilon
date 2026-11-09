package cores

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/awgd"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// AWGIfaceName is the TUN interface name of an AmneziaWG inbound.
func AWGIfaceName(id int64) string { return fmt.Sprintf("vawg%d", id) }

// AWGConfig renders the sidecar config. reloadKey changes only when
// something other than the peer list changed.
func AWGConfig(in *xray.Input) (cfg []byte, reloadKey string, owners map[string]int64, err error) {
	c, keys, owners := awgIfaces(in)
	if len(c.Interfaces) == 0 {
		return nil, "", owners, nil
	}
	b, err := json.MarshalIndent(c, "", "  ")
	return b, strings.Join(keys, "|"), owners, err
}

// AWGInstance is the sidecar config of one AmneziaWG inbound.
type AWGInstance struct {
	Iface     string
	Config    []byte
	ReloadKey string
}

// AWGConfigs renders one sidecar config per AmneziaWG inbound. Each inbound
// runs in its own sidecar process: amneziawg-go keeps the obfuscation
// headers (H1-H4) in package-level variables, so two devices with different
// headers in one process would overwrite each other's and break both.
func AWGConfigs(in *xray.Input) ([]AWGInstance, map[string]int64, error) {
	c, keys, owners := awgIfaces(in)
	out := make([]AWGInstance, 0, len(c.Interfaces))
	for i, it := range c.Interfaces {
		b, err := json.MarshalIndent(awgd.Config{Interfaces: []awgd.Iface{it}}, "", "  ")
		if err != nil {
			return nil, owners, err
		}
		out = append(out, AWGInstance{Iface: it.Name, Config: b, ReloadKey: keys[i]})
	}
	return out, owners, nil
}

func awgIfaces(in *xray.Input) (c awgd.Config, keys []string, owners map[string]int64) {
	c = awgd.Config{Interfaces: []awgd.Iface{}}
	owners = map[string]int64{}
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if ib.Protocol != inbounds.AmneziaWG || ib.Settings.AWG == nil {
			continue
		}
		a := ib.Settings.AWG
		it := awgd.Iface{
			Name: AWGIfaceName(ib.ID), ListenPort: ib.Port, PrivateKey: ib.Settings.PrivateKey,
			Address: ib.Settings.Address, MTU: ib.Settings.MTU,
			Params: awgd.Params{Jc: a.Jc, Jmin: a.Jmin, Jmax: a.Jmax, S1: a.S1, S2: a.S2, H1: a.H1, H2: a.H2, H3: a.H3, H4: a.H4},
			Peers:  []awgd.Peer{},
		}
		for _, u := range in.Users[ib.ID] {
			p, ok := in.Peers[ib.ID][u.ID]
			if !ok {
				continue
			}
			it.Peers = append(it.Peers, awgd.Peer{PublicKey: p.PublicKey, PresharedKey: p.PresharedKey, AllowedIP: p.Address})
			owners[it.Name+"/"+p.PublicKey] = u.ID
		}
		c.Interfaces = append(c.Interfaces, it)
		head := it
		head.Peers = nil
		hb, _ := json.Marshal(head)
		keys = append(keys, string(hb))
	}
	return c, keys, owners
}

// AWG runs the built-in AmneziaWG sidecar.
type AWG struct {
	*Supervisor
	opt    Options
	mu     sync.Mutex
	owners map[string]int64
	latest map[string]awgSample // iface/pubkey -> rx, tx
	cnt    counter
}

// NewAWG creates the AmneziaWG engine. self is the path of the veilon binary.
func NewAWG(opt Options, self string) *AWG {
	e := &AWG{opt: opt, owners: map[string]int64{}, latest: map[string]awgSample{}, cnt: counter{}}
	cfgDir := filepath.Join(opt.DataDir, "amneziawg")
	e.Supervisor = NewSupervisor(Spec{
		DB:           opt.DB,
		Name:         "amneziawg",
		Protocols:    []string{inbounds.AmneziaWG},
		Bin:          func() string { return self },
		Version:      func() string { return strings.TrimPrefix(awgd.Version, "amneziawg-go ") },
		ReloadSignal: syscall.SIGHUP,
		ConfigGlob:   filepath.Join(cfgDir, "vawg*.json"),
		Plan: func() ([]Instance, error) {
			in, err := xray.LoadInput(opt.DB, opt.DataDir, 0)
			if err != nil {
				return nil, err
			}
			list, owners, err := AWGConfigs(in)
			if err != nil {
				return nil, err
			}
			e.mu.Lock()
			e.owners = owners
			e.mu.Unlock()
			// The single config.json of older versions is not used anymore.
			_ = os.Remove(filepath.Join(cfgDir, "config.json"))
			out := make([]Instance, 0, len(list))
			for _, it := range list {
				p := filepath.Join(cfgDir, it.Iface+".json")
				out = append(out, Instance{Key: it.Iface, Config: it.Config, ConfigFile: p, Args: []string{"awg-run", p}, ReloadKey: it.ReloadKey})
			}
			return out, nil
		},
		LineHook: e.line,
		Collect:  e.collect,
		Ensure: func(context.Context) error {
			if _, err := os.Stat(self); err != nil {
				return fmt.Errorf("veilon binary not found at %s", self)
			}
			return nil
		},
	})
	return e
}

type awgSample struct {
	gen    uint64
	rx, tx int64
}

func (e *AWG) line(_ string, gen uint64, line string) bool {
	js, ok := strings.CutPrefix(line, awgd.StatsPrefix)
	if !ok {
		return false
	}
	var st map[string]map[string][2]int64
	if json.Unmarshal([]byte(js), &st) == nil {
		e.mu.Lock()
		for ifc, peers := range st {
			for pk, v := range peers {
				e.latest[ifc+"/"+pk] = awgSample{gen, v[0], v[1]}
			}
		}
		e.mu.Unlock()
	}
	return true
}

func (e *AWG) collect(_ context.Context, _ []Running) {
	deltas := map[int64][2]int64{}
	var su, sd int64
	e.mu.Lock()
	for k, v := range e.latest {
		// rx: received from the client (upload), tx: sent to it (download).
		du, dd := e.cnt.delta(k, v.gen, v.rx, v.tx)
		su += du
		sd += dd
		if uid, ok := e.owners[k]; ok {
			d := deltas[uid]
			deltas[uid] = [2]int64{d[0] + du, d[1] + dd}
		}
	}
	e.mu.Unlock()
	e.AddSystem(su, sd)
	if err := xray.ApplyUserTraffic(e.opt.DB, deltas, time.Now().UTC()); err != nil {
		slog.Warn("store amneziawg user traffic", "err", err)
	}
}
