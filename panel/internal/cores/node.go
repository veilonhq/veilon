package cores

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/awgd"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Engines on a node.
//
// A node runs its Xray inbounds with its own Xray, and its AmneziaWG and
// MTProto inbounds with the same engines as the main server: the built-in
// AmneziaWG sidecar ("veilon awg-run") and mtg-multi. The master renders a
// NodeSpec from its database (the inbounds and their users) and pushes it
// to the agent (PUT /v1/engines); the agent runs it with NodeEngines and
// adds the engines' per-user traffic and client IPs to the batches it
// already sends.

// NodeSpec is what a node runs besides Xray.
type NodeSpec struct {
	AWG []NodeAWG `json:"awg,omitempty"`
	MTG []NodeMTG `json:"mtg,omitempty"`
}

// NodeAWG is one AmneziaWG inbound on a node.
type NodeAWG struct {
	InboundID int64      `json:"inbound_id"`
	Remark    string     `json:"remark"`
	Iface     awgd.Iface `json:"iface"`
	// Owners maps each peer's public key to its user id.
	Owners map[string]int64 `json:"owners"`
}

// NodeMTG is one MTProto inbound on a node.
type NodeMTG struct {
	InboundID int64       `json:"inbound_id"`
	Remark    string      `json:"remark"`
	Listen    string      `json:"listen,omitempty"`
	Port      int         `json:"port"`
	Domain    string      `json:"domain"`
	Secrets   []MTGSecret `json:"secrets"`
}

// Empty reports whether the spec runs nothing.
func (s *NodeSpec) Empty() bool { return s == nil || (len(s.AWG) == 0 && len(s.MTG) == 0) }

// Ports lists the ports the spec's inbounds listen on.
func (s *NodeSpec) Ports() []int {
	if s == nil {
		return nil
	}
	var out []int
	for _, a := range s.AWG {
		out = append(out, a.Iface.ListenPort)
	}
	for _, m := range s.MTG {
		out = append(out, m.Port)
	}
	sort.Ints(out)
	return out
}

// Validate checks a spec received from the master.
func (s *NodeSpec) Validate() error {
	seen := map[int]bool{}
	for _, a := range s.AWG {
		if a.InboundID <= 0 || a.Iface.Name != AWGIfaceName(a.InboundID) {
			return fmt.Errorf("amneziawg inbound %d: bad interface %q", a.InboundID, a.Iface.Name)
		}
		if a.Iface.ListenPort < 1 || a.Iface.ListenPort > 65535 {
			return fmt.Errorf("amneziawg inbound %d: bad port %d", a.InboundID, a.Iface.ListenPort)
		}
		if _, err := awgd.UAPI(a.Iface, false); err != nil {
			return fmt.Errorf("amneziawg inbound %d: %w", a.InboundID, err)
		}
		if seen[a.Iface.ListenPort] {
			return fmt.Errorf("port %d is used twice", a.Iface.ListenPort)
		}
		seen[a.Iface.ListenPort] = true
	}
	for _, m := range s.MTG {
		if m.InboundID <= 0 || m.Port < 1 || m.Port > 65535 {
			return fmt.Errorf("mtproto inbound %d: bad port %d", m.InboundID, m.Port)
		}
		if m.Listen != "" && net.ParseIP(m.Listen) == nil {
			return fmt.Errorf("mtproto inbound %d: bad listen address %q", m.InboundID, m.Listen)
		}
		if len(m.Secrets) == 0 {
			return fmt.Errorf("mtproto inbound %d has no users", m.InboundID)
		}
		if seen[m.Port] {
			return fmt.Errorf("port %d is used twice", m.Port)
		}
		seen[m.Port] = true
	}
	return nil
}

// BuildNodeSpec renders the AmneziaWG and MTProto inbounds among
// inboundIDs (enabled ones, with their served users) for a node. Other
// inbounds are left out (Xray's config carries them). An MTProto inbound
// without users is left out too: mtg refuses to start without a secret.
func BuildNodeSpec(gdb *gorm.DB, dataDir string, inboundIDs []int64) (*NodeSpec, error) {
	spec := &NodeSpec{}
	if len(inboundIDs) == 0 {
		return spec, nil
	}
	in, err := xray.LoadInput(gdb, dataDir, 0)
	if err != nil {
		return nil, err
	}
	want := map[int64]bool{}
	for _, id := range inboundIDs {
		want[id] = true
	}
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if !want[ib.ID] {
			continue
		}
		switch ib.Protocol {
		case inbounds.AmneziaWG:
			if ib.Settings.AWG == nil {
				continue
			}
			it, owners := awgIface(ib, in)
			spec.AWG = append(spec.AWG, NodeAWG{InboundID: ib.ID, Remark: ib.Remark, Iface: it, Owners: owners})
		case inbounds.MTProto:
			sec := mtgSecrets(ib, in)
			if len(sec) == 0 {
				continue
			}
			spec.MTG = append(spec.MTG, NodeMTG{InboundID: ib.ID, Remark: ib.Remark, Listen: ib.Listen, Port: ib.Port,
				Domain: ib.Settings.FakeTLSDomain, Secrets: sec})
		}
	}
	return spec, nil
}

// NodeEngines runs a node's AmneziaWG and MTProto inbounds.
type NodeEngines struct {
	// Dir holds the engines' files (amneziawg/, mtg/).
	Dir string
	// Self is the veilon binary (it runs the AmneziaWG sidecar).
	Self string
	// MTGBin is the mtg-multi binary, downloaded there when missing and
	// AutoDownload is set; MTGVersion is the release to download.
	MTGBin       string
	MTGVersion   string
	AutoDownload bool
	// Traffic receives each read's per-user traffic ({up, down}) and the
	// engines' totals.
	Traffic func(users map[int64][2]int64, up, down int64)
	// SeenIP receives client IPs per user (AmneziaWG endpoints that sent
	// traffic, MTProto's live client addresses), for IP limits.
	SeenIP func(userID int64, ip string)

	once   sync.Once
	mu     sync.Mutex
	spec   NodeSpec
	awg    *Supervisor
	mtg    *Supervisor
	mtgEng *MTG
	latest map[string]awgSample // iface/pubkey -> counters
	eps    map[string]string    // iface/pubkey -> endpoint
	owners map[string]int64     // iface/pubkey -> user id
	cnt    counter
}

func (e *NodeEngines) init() {
	e.once.Do(func() {
		e.latest, e.eps, e.owners, e.cnt = map[string]awgSample{}, map[string]string{}, map[string]int64{}, counter{}
		awgDir := filepath.Join(e.Dir, "amneziawg")
		e.awg = NewSupervisor(Spec{
			Name:         "amneziawg",
			Protocols:    []string{inbounds.AmneziaWG},
			Bin:          func() string { return e.Self },
			Version:      func() string { return strings.TrimPrefix(awgd.Version, "amneziawg-go ") },
			ReloadSignal: syscall.SIGHUP,
			ConfigGlob:   filepath.Join(awgDir, "vawg*.json"),
			Plan:         e.awgPlan,
			LineHook:     e.awgLine,
			Collect:      e.awgCollect,
			Ensure: func(context.Context) error {
				if _, err := os.Stat(e.Self); err != nil {
					return fmt.Errorf("veilon binary not found at %s", e.Self)
				}
				return nil
			},
		})
		e.mtgEng = &MTG{opt: Options{BinPath: e.MTGBin, Version: e.MTGVersion, AutoDownload: e.AutoDownload}, ports: map[int64]int{}, cnt: counter{}}
		e.mtg = NewSupervisor(Spec{
			Name:         "mtg",
			Protocols:    []string{inbounds.MTProto},
			Bin:          func() string { return e.MTGBin },
			Ensure:       e.mtgEng.ensure,
			Version:      func() string { return strings.TrimPrefix(e.MTGVersion, "v") },
			ReloadSignal: syscall.SIGHUP,
			Plan:         e.mtgPlan,
			Collect:      e.mtgCollect,
			ConfigGlob:   filepath.Join(e.Dir, "mtg", "inbound-*.toml"),
		})
	})
}

// Set replaces what the engines run (applied after a short debounce).
func (e *NodeEngines) Set(spec NodeSpec) {
	e.init()
	e.mu.Lock()
	e.spec = spec
	e.mu.Unlock()
	e.awg.RequestReload()
	e.mtg.RequestReload()
}

// Run supervises the engines until ctx ends; their last traffic is read
// and the processes are stopped before it returns.
func (e *NodeEngines) Run(ctx context.Context) {
	e.init()
	var wg sync.WaitGroup
	for _, s := range []*Supervisor{e.awg, e.mtg} {
		wg.Add(1)
		go func(s *Supervisor) { defer wg.Done(); util.Supervise(ctx, "node engine "+s.Name(), s.Run) }(s)
	}
	wg.Wait()
}

// Statuses reports both engines.
func (e *NodeEngines) Statuses() []xray.Status {
	e.init()
	return []xray.Status{e.awg.Status(), e.mtg.Status()}
}

// Errors lists the engines' current errors ("engine: message").
func (e *NodeEngines) Errors() []string {
	var out []string
	for _, st := range e.Statuses() {
		if st.Error != "" && st.Enabled {
			out = append(out, st.Name+": "+st.Error)
		}
	}
	return out
}

func (e *NodeEngines) awgPlan() ([]Instance, error) {
	e.mu.Lock()
	list := append([]NodeAWG(nil), e.spec.AWG...)
	e.mu.Unlock()
	owners := map[string]int64{}
	out := make([]Instance, 0, len(list))
	for _, a := range list {
		b, err := json.MarshalIndent(awgd.Config{Interfaces: []awgd.Iface{a.Iface}}, "", "  ")
		if err != nil {
			return nil, err
		}
		for pk, uid := range a.Owners {
			owners[a.Iface.Name+"/"+pk] = uid
		}
		p := filepath.Join(e.Dir, "amneziawg", a.Iface.Name+".json")
		out = append(out, Instance{Key: a.Iface.Name, Config: b, ConfigFile: p, Args: []string{"awg-run", p},
			Env: []string{awgd.EndpointsEnv + "=1"}, ReloadKey: awgReloadKey(a.Iface)})
	}
	e.mu.Lock()
	e.owners = owners
	e.mu.Unlock()
	return out, nil
}

func (e *NodeEngines) awgLine(_ string, gen uint64, line string) bool {
	if js, ok := strings.CutPrefix(line, awgd.StatsPrefix); ok {
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
	if js, ok := strings.CutPrefix(line, awgd.PeersPrefix); ok {
		var st map[string]map[string]string
		if json.Unmarshal([]byte(js), &st) == nil {
			e.mu.Lock()
			for ifc, peers := range st {
				for pk, ep := range peers {
					e.eps[ifc+"/"+pk] = ep
				}
			}
			e.mu.Unlock()
		}
		return true
	}
	return false
}

func (e *NodeEngines) awgCollect(_ context.Context, _ []Running) {
	deltas := map[int64][2]int64{}
	var su, sd int64
	type hit struct {
		uid int64
		ip  string
	}
	var hits []hit
	e.mu.Lock()
	for k, v := range e.latest {
		// rx: received from the client (upload), tx: sent to it (download).
		du, dd := e.cnt.delta("awg/"+k, v.gen, v.rx, v.tx)
		su += du
		sd += dd
		uid, ok := e.owners[k]
		if !ok {
			continue
		}
		d := deltas[uid]
		deltas[uid] = [2]int64{d[0] + du, d[1] + dd}
		// A peer keeps its last endpoint after the client left: only one
		// that sent something since the last read is a live client IP.
		if du > 0 {
			if host, _, err := net.SplitHostPort(e.eps[k]); err == nil && host != "" {
				hits = append(hits, hit{uid, host})
			}
		}
	}
	e.mu.Unlock()
	e.emit(deltas, su, sd)
	if e.SeenIP != nil {
		for _, h := range hits {
			e.SeenIP(h.uid, h.ip)
		}
	}
}

func (e *NodeEngines) mtgPlan() ([]Instance, error) {
	e.mu.Lock()
	list := append([]NodeMTG(nil), e.spec.MTG...)
	e.mu.Unlock()
	out := make([]Instance, 0, len(list))
	for _, m := range list {
		port := e.mtgEng.apiPort(m.InboundID)
		path := filepath.Join(e.Dir, "mtg", fmt.Sprintf("inbound-%d.toml", m.InboundID))
		out = append(out, Instance{
			Key: fmt.Sprintf("inbound-%d", m.InboundID), Config: mtgTOML(m.InboundID, m.Remark, m.Listen, m.Port, port, m.Secrets), ConfigFile: path,
			Args:      []string{"run", path},
			ReloadKey: fmt.Sprintf("%s|%d|%d|%s", m.Listen, m.Port, port, m.Domain),
			Meta:      mtgMeta{InboundID: m.InboundID, APIPort: port},
		})
	}
	return out, nil
}

// mtgNodeStats is the part of mtg-multi's /stats a node reads.
type mtgNodeStats struct {
	Users map[string]struct {
		BytesIn   int64    `json:"bytes_in"`
		BytesOut  int64    `json:"bytes_out"`
		ActiveIPs []string `json:"active_ips"`
	} `json:"users"`
}

func (e *NodeEngines) mtgCollect(ctx context.Context, running []Running) {
	deltas := map[int64][2]int64{}
	var su, sd int64
	type hit struct {
		uid int64
		ip  string
	}
	var hits []hit
	for _, r := range running {
		m, ok := r.Meta.(mtgMeta)
		if !ok || m.APIPort == 0 {
			continue
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/stats", m.APIPort), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		var st mtgNodeStats
		err = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		if err != nil {
			continue
		}
		e.mu.Lock()
		for name, u := range st.Users {
			// bytes_in: received from the client (upload).
			du, dd := e.cnt.delta(fmt.Sprintf("mtg/%d/%s", m.InboundID, name), r.Gen, u.BytesIn, u.BytesOut)
			su += du
			sd += dd
			id, ok := xray.ParseEmail(name)
			if !ok {
				continue
			}
			d := deltas[id]
			deltas[id] = [2]int64{d[0] + du, d[1] + dd}
			for _, ip := range u.ActiveIPs {
				if net.ParseIP(ip) != nil {
					hits = append(hits, hit{id, ip})
				}
			}
		}
		e.mu.Unlock()
	}
	e.emit(deltas, su, sd)
	if e.SeenIP != nil {
		for _, h := range hits {
			e.SeenIP(h.uid, h.ip)
		}
	}
}

func (e *NodeEngines) emit(deltas map[int64][2]int64, up, down int64) {
	for id, d := range deltas {
		if d[0] == 0 && d[1] == 0 {
			delete(deltas, id)
		}
	}
	if e.Traffic != nil && (len(deltas) > 0 || up != 0 || down != 0) {
		e.Traffic(deltas, up, down)
	}
}
