package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/cores"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/presence"
	"github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Node states shown in the panel.
const (
	StateOnline   = "online"   // reachable, Xray runs the current config
	StateDegraded = "degraded" // reachable, but Xray is down or rejected the config
	StateOffline  = "offline"  // not reachable
	StateWaiting  = "waiting"  // never reached yet (agent not installed)
	StateDisabled = "disabled" // switched off by an admin
)

// State is the live status of one node (kept in memory).
type State struct {
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	CheckedAt   *time.Time `json:"checked_at"`
	Version     string     `json:"version"`
	XrayVersion string     `json:"xray_version"`
	XrayRunning bool       `json:"xray_running"`
	Synced      bool       `json:"synced"`
	Skipped     []string   `json:"skipped"`
	StartedAt   *time.Time `json:"started_at"`
	Load1       float64    `json:"load1"`
	MemUsed     float64    `json:"mem_used"`
	CPUs        int        `json:"cpus"`
	LatencyMS   int64      `json:"latency_ms"`
	// Arch is the node's CPU architecture ("" from agents before v0.3.5).
	Arch string `json:"arch"`
	// CanUpdate: the agent updates itself from the panel (v0.3.5 and
	// later); older agents need one manual install.
	CanUpdate bool `json:"can_update"`
	// Update is the last agent update asked from this panel (nil = none).
	Update *UpdateStatus `json:"update"`
	// Caps are the capabilities the agent announced.
	Caps []string `json:"caps"`
	// NeedsUpdate lists the node's inbounds its agent is too old to run
	// (AmneziaWG / MTProto on agents before v0.3.8); they are skipped
	// until the agent is updated.
	NeedsUpdate []SkippedInbound `json:"needs_update"`
	// liveOnline: the agent lists the users with open connections
	// (CapOnline, v0.3.6 and later).
	liveOnline bool
}

// SkippedInbound is an inbound a node does not run.
type SkippedInbound struct {
	ID       int64  `json:"id"`
	Remark   string `json:"remark"`
	Protocol string `json:"protocol"`
}

// Manager is the master side: it keeps every node's config in sync, reads
// their traffic and tracks their health. Every node is handled in its own
// goroutine with hard timeouts, so a slow, broken or unreachable node never
// blocks the panel or the other nodes.
type Manager struct {
	DB      *gorm.DB
	DataDir string
	PKI     *PKI
	// Interval between rounds (default 10s); Timeout bounds one node's
	// round (default 25s); RequestTimeout bounds each request (default 8s).
	Interval       time.Duration
	Timeout        time.Duration
	RequestTimeout time.Duration
	// OnlineEvery is how often agents that list online users are asked
	// for them (default 5s), between the full rounds.
	OnlineEvery time.Duration
	// Presence receives the nodes' online users and traffic (nil =
	// presence.Default).
	Presence *presence.Tracker
	// ApplyTraffic stores per-user traffic and returns the counted bytes it
	// added (nil = xray.ApplyUserTrafficTx in one transaction with the node's
	// totals; the node's id keeps its history apart).
	ApplyTraffic func(gdb *gorm.DB, deltas map[int64][2]int64, now time.Time) (up, down int64, err error)
	// BuildConfig renders a node's Xray config (default xray.NodeConfig).
	BuildConfig func(gdb *gorm.DB, dataDir string, inboundIDs []int64, apiPort int) ([]byte, []string, error)
	// BuildEngines renders a node's AmneziaWG and MTProto inbounds
	// (default cores.BuildNodeSpec).
	BuildEngines func(gdb *gorm.DB, dataDir string, inboundIDs []int64) (*cores.NodeSpec, error)
	// OnIPs receives the client IPs a node reported (with the master-minus-
	// node clock difference), for IP limits across servers (nil = ignored).
	OnIPs func(ips []guard.SeenIP, skew time.Duration)
	// Bans lists the IPs dropped for the IP limit; every node gets them
	// (nil = no bans are sent).
	Bans func() []guard.Ban
	// Shapes is the speed-limit plan; every node gets it with the bans.
	Shapes func() []guard.Shape
	// Version is the main server's version: nodes are updated to it.
	Version string
	// ReleaseAPI is the release list nodes are updated from ("" = the
	// panel's self-update source, VEILON_UPDATE_API or GitHub).
	ReleaseAPI    string
	ReleaseClient *http.Client
	ups           updates

	once     sync.Once
	mu       sync.Mutex
	states   map[int64]*State
	inflight map[int64]bool
	clients  map[int64]*nodeClient
	rejected map[int64]rejection
	// engRejected: the last engines spec a node refused.
	engRejected map[int64]rejection
	bansSent    map[int64]bansState
	onlineIn    map[int64]bool      // GET /v1/online in flight
	noOnline    map[int64]time.Time // the node's Xray cannot list online users
	kick        chan struct{}
	trafMu      sync.Mutex
}

type bansState struct {
	sig         string
	at          time.Time
	unsupported bool // the agent is older than v0.3.2 (no /v1/bans)
}

type rejection struct {
	hash string
	at   time.Time
}

type nodeClient struct {
	key  string
	http *http.Client
}

func (m *Manager) init() {
	m.once.Do(func() {
		if m.Interval == 0 {
			m.Interval = 10 * time.Second
		}
		if m.Timeout == 0 {
			m.Timeout = 25 * time.Second
		}
		if m.RequestTimeout == 0 {
			m.RequestTimeout = 8 * time.Second
		}
		if m.OnlineEvery == 0 {
			m.OnlineEvery = 5 * time.Second
		}
		if m.Presence == nil {
			m.Presence = presence.Default
		}
		m.onlineIn = map[int64]bool{}
		m.noOnline = map[int64]time.Time{}
		if m.BuildConfig == nil {
			m.BuildConfig = xray.NodeConfig
		}
		if m.BuildEngines == nil {
			m.BuildEngines = cores.BuildNodeSpec
		}
		m.states = map[int64]*State{}
		m.inflight = map[int64]bool{}
		m.clients = map[int64]*nodeClient{}
		m.rejected = map[int64]rejection{}
		m.engRejected = map[int64]rejection{}
		m.bansSent = map[int64]bansState{}
		m.kick = make(chan struct{}, 1)
	})
}

// Kick asks for a round soon (after a config change). It never blocks.
func (m *Manager) Kick() {
	m.init()
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Run syncs the nodes until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.init()
	t := time.NewTicker(m.Interval)
	defer t.Stop()
	ot := time.NewTicker(m.OnlineEvery)
	defer ot.Stop()
	var debounce <-chan time.Time
	m.Round(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ot.C:
			m.OnlineRound(ctx)
		case <-m.kick:
			if debounce == nil {
				debounce = time.After(1500 * time.Millisecond)
			}
		case <-debounce:
			debounce = nil
			m.Round(ctx)
		case <-t.C:
			m.Round(ctx)
		}
	}
}

// Round starts a sync of every node that is not still busy with the last
// one. It returns at once; Wait-style callers (tests) use SyncNode.
func (m *Manager) Round(ctx context.Context) {
	m.init()
	var ns []db.Node
	if err := m.DB.Order("id").Find(&ns).Error; err != nil {
		slog.Warn("nodes: list failed", "err", err)
		return
	}
	alive := map[int64]bool{}
	for i := range ns {
		n := ns[i]
		alive[n.ID] = true
		m.mu.Lock()
		busy := m.inflight[n.ID]
		if !busy {
			m.inflight[n.ID] = true
		}
		m.mu.Unlock()
		if busy {
			continue
		}
		go func() {
			defer func() {
				m.mu.Lock()
				delete(m.inflight, n.ID)
				m.mu.Unlock()
			}()
			util.Safe("node "+n.Name, func() { m.SyncNode(ctx, &n) })
		}()
	}
	m.mu.Lock()
	for id := range m.states {
		if !alive[id] {
			delete(m.states, id)
			delete(m.clients, id)
			delete(m.rejected, id)
			delete(m.engRejected, id)
			delete(m.bansSent, id)
		}
	}
	m.mu.Unlock()
}

// Status returns a copy of a node's live state.
func (m *Manager) Status(n *db.Node) State {
	m.init()
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.states[n.ID]; ok {
		c := *st
		if !n.Enabled && c.Status != StateOffline {
			c.Status = StateDisabled
		}
		c.Update = m.UpdateState(n.ID)
		return c
	}
	if !n.Enabled {
		return State{Status: StateDisabled}
	}
	return State{Status: StateWaiting}
}

// Forget drops the cached client and state of a node (after its certificate
// changed or it was deleted).
func (m *Manager) Forget(id int64) {
	m.init()
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.clients[id]; c != nil {
		c.http.CloseIdleConnections()
	}
	delete(m.clients, id)
	delete(m.states, id)
	delete(m.rejected, id)
	delete(m.engRejected, id)
	delete(m.bansSent, id)
	delete(m.noOnline, id)
}

func (m *Manager) setState(id int64, st *State) {
	now := time.Now().UTC()
	st.CheckedAt = &now
	m.mu.Lock()
	m.states[id] = st
	m.mu.Unlock()
}

func (m *Manager) client(n *db.Node) (*http.Client, error) {
	key := fmt.Sprintf("%s|%d|%s", n.Address, n.APIPort, n.CertSHA256)
	m.mu.Lock()
	c := m.clients[n.ID]
	m.mu.Unlock()
	if c != nil && c.key == key {
		return c.http, nil
	}
	tc, err := m.PKI.ClientTLS(n.ID, n.CertSHA256)
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Timeout: m.RequestTimeout, Transport: &http.Transport{
		TLSClientConfig:     tc,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
		Proxy:               nil,
	}}
	m.mu.Lock()
	if old := m.clients[n.ID]; old != nil {
		old.http.CloseIdleConnections()
	}
	m.clients[n.ID] = &nodeClient{key: key, http: hc}
	m.mu.Unlock()
	return hc, nil
}

func baseURL(n *db.Node) string {
	return "https://" + net.JoinHostPort(n.Address, strconv.Itoa(n.APIPort))
}

// SyncNode runs one round for one node: health, config, traffic.
func (m *Manager) SyncNode(parent context.Context, n *db.Node) {
	m.init()
	ctx, cancel := context.WithTimeout(parent, m.Timeout)
	defer cancel()
	hc, err := m.client(n)
	if err != nil {
		m.setState(n.ID, &State{Status: StateOffline, Error: err.Error()})
		return
	}
	t0 := time.Now()
	var info Info
	if err := m.call(ctx, hc, http.MethodGet, baseURL(n)+"/v1/info", nil, &info); err != nil {
		prev := m.Status(n)
		status := StateOffline
		if n.LastSeenAt == nil {
			status = StateWaiting // never reached: most likely not installed yet
		}
		st := &State{Status: status, Error: err.Error(), Version: prev.Version, XrayVersion: prev.XrayVersion,
			Arch: prev.Arch, CanUpdate: prev.CanUpdate, Caps: prev.Caps, NeedsUpdate: prev.NeedsUpdate}
		m.setState(n.ID, st)
		m.trackUpdate(n, nil)
		return
	}
	m.trackUpdate(n, &info)
	st := &State{Version: info.Version, XrayVersion: info.XrayVersion, XrayRunning: info.XrayRunning, Load1: info.Load1,
		MemUsed: info.MemUsed, CPUs: info.CPUs, LatencyMS: time.Since(t0).Milliseconds(), Arch: info.Arch, CanUpdate: info.HasCap(CapUpdate),
		liveOnline: info.HasCap(CapOnline), Caps: info.Caps}
	if !info.StartedAt.IsZero() {
		s := info.StartedAt
		st.StartedAt = &s
	}
	if info.NodeID != 0 && info.NodeID != n.ID {
		st.Status, st.Error = StateDegraded, fmt.Sprintf("this address answers as node %d", info.NodeID)
		m.setState(n.ID, st)
		return
	}

	// config: a disabled node keeps running, but serves nothing
	var ids []int64
	if n.Enabled {
		m.DB.Model(&db.NodeInbound{}).Where("node_id = ?", n.ID).Order("inbound_id").Pluck("inbound_id", &ids)
	}
	apiPort := info.APIPort
	if apiPort == 0 {
		apiPort = DefaultAPIPort
	}
	var cfg []byte
	var skipped []string
	var berr error
	if !util.Safe("node config", func() { cfg, skipped, berr = m.BuildConfig(m.DB, m.DataDir, ids, apiPort) }) {
		berr = errors.New("internal error while rendering the node config")
	}
	if berr == nil {
		cfg, berr = compact(cfg)
	}
	st.Skipped = skipped
	pushErr := ""
	if berr != nil {
		pushErr = "config: " + berr.Error()
	} else {
		hash := Hash(cfg)
		st.Synced = info.ConfigHash == hash
		m.mu.Lock()
		rj, wasRejected := m.rejected[n.ID]
		m.mu.Unlock()
		retry := !wasRejected || rj.hash != hash || time.Since(rj.at) > 5*time.Minute
		if !st.Synced && wasRejected && !retry {
			pushErr = "the node rejected the config"
		}
		if !st.Synced && retry {
			var after Info
			err := m.call(ctx, hc, http.MethodPut, baseURL(n)+"/v1/config", ConfigPush{Hash: hash, Config: cfg}, &after)
			if err != nil {
				pushErr = err.Error()
				m.mu.Lock()
				m.rejected[n.ID] = rejection{hash: hash, at: time.Now()}
				m.mu.Unlock()
				slog.Warn("node config push failed", "node", n.Name, "err", err)
			} else {
				st.Synced = true
				st.XrayRunning, st.XrayVersion = after.XrayRunning, after.XrayVersion
				m.mu.Lock()
				delete(m.rejected, n.ID)
				m.mu.Unlock()
				slog.Info("node config pushed", "node", n.Name, "inbounds", len(ids))
			}
		}
	}

	// AmneziaWG and MTProto inbounds
	if eerr := m.syncEngines(ctx, hc, n, &info, ids, st); eerr != "" && pushErr == "" {
		pushErr = eerr
	}

	// traffic
	var sr StatsReply
	if err := m.call(ctx, hc, http.MethodGet, baseURL(n)+"/v1/stats?ack="+url.QueryEscape(n.StatsSeq), nil, &sr); err != nil {
		if pushErr == "" {
			pushErr = "traffic: " + err.Error()
		}
	} else if sr.Seq != "" && sr.Seq != n.StatsSeq {
		if err := m.storeTraffic(n, &sr); err != nil {
			slog.Warn("node traffic not stored", "node", n.Name, "err", err)
		} else if m.OnIPs != nil && len(sr.IPs) > 0 && n.Enabled {
			skew := time.Duration(0)
			if !sr.Now.IsZero() {
				skew = time.Now().UTC().Sub(sr.Now)
			}
			ips := sr.IPs
			util.Safe("node ips", func() { m.OnIPs(ips, skew) })
		}
	} else {
		now := time.Now().UTC()
		m.DB.Model(&db.Node{}).Where("id = ?", n.ID).Update("last_seen_at", now)
	}

	m.sendBans(ctx, hc, n)

	switch {
	case pushErr != "":
		st.Status, st.Error = StateDegraded, pushErr
	case !st.XrayRunning && info.XrayError != "":
		st.Status, st.Error = StateDegraded, info.XrayError
	case !st.XrayRunning:
		st.Status, st.Error = StateDegraded, "xray is not running"
	default:
		st.Status = StateOnline
	}
	m.setState(n.ID, st)
}

// storeTraffic applies a stats batch once: users' usage, the node's totals
// and the batch id (acknowledged in the next request).
func (m *Manager) storeTraffic(n *db.Node, sr *StatsReply) error {
	m.trafMu.Lock()
	defer m.trafMu.Unlock()
	now := time.Now().UTC()
	// The node's totals are the users' counted usage it carried (real bytes x
	// each user's multiplier), the same figure as its share in "traffic by
	// server" and in users' usage, not the raw inbound counters.
	node := func(tx *gorm.DB, up, down int64) error {
		return tx.Model(&db.Node{}).Where("id = ?", n.ID).Updates(map[string]any{
			"up": gorm.Expr("up + ?", up), "down": gorm.Expr("down + ?", down),
			"stats_seq": sr.Seq, "last_seen_at": now,
		}).Error
	}
	if m.ApplyTraffic != nil { // tests
		var up, down int64
		if len(sr.Users) > 0 {
			var err error
			if up, down, err = m.ApplyTraffic(m.DB, sr.Users, now); err != nil {
				return err
			}
		}
		n.StatsSeq = sr.Seq
		m.Presence.Traffic(Source(n.ID), sr.Users, now)
		m.Presence.Traffic(EngineSource(n.ID), sr.Engines, now)
		return node(m.DB, up, down)
	}
	// Users' counters, the node's totals and the batch id commit together:
	// a batch is counted exactly once even if the master stops halfway
	// (the node sends it again until its id is stored).
	var up, down int64
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if up, down, err = xray.ApplyUserTrafficTx(tx, sr.Users, now); err != nil {
			return err
		}
		return node(tx, up, down)
	})
	if err != nil {
		return err
	}
	n.StatsSeq = sr.Seq
	usage.For(n.ID).Add(up, down)
	m.Presence.Traffic(Source(n.ID), sr.Users, now)
	m.Presence.Traffic(EngineSource(n.ID), sr.Engines, now)
	return nil
}

// Source is a node's name in package presence.
func Source(id int64) string { return "node:" + strconv.FormatInt(id, 10) }

// EngineSource is the presence source of a node's AmneziaWG and MTProto
// traffic (traffic only: those engines cannot list open connections).
func EngineSource(id int64) string { return Source(id) + ":engines" }

// syncEngines pushes the node's AmneziaWG and MTProto inbounds when they
// changed and returns an error for the node's status ("" = fine). Inbounds
// the agent cannot run (no capability: agents before v0.3.8) are left out
// and listed in st.NeedsUpdate; an agent without any engine capability is
// not asked at all, so older agents keep working as before.
func (m *Manager) syncEngines(ctx context.Context, hc *http.Client, n *db.Node, info *Info, ids []int64, st *State) string {
	var spec *cores.NodeSpec
	var err error
	if !util.Safe("node engines", func() { spec, err = m.BuildEngines(m.DB, m.DataDir, ids) }) {
		err = errors.New("internal error while rendering the node's AmneziaWG / MTProto inbounds")
	}
	if err != nil {
		st.Synced = false
		return "engines: " + err.Error()
	}
	if spec == nil {
		spec = &cores.NodeSpec{}
	}
	canAWG, canMTG := info.HasCap(CapAWG), info.HasCap(CapMTProto)
	if !canAWG {
		for _, a := range spec.AWG {
			st.NeedsUpdate = append(st.NeedsUpdate, SkippedInbound{ID: a.InboundID, Remark: a.Remark, Protocol: inbounds.AmneziaWG})
		}
		spec.AWG = nil
	}
	if !canMTG {
		for _, x := range spec.MTG {
			st.NeedsUpdate = append(st.NeedsUpdate, SkippedInbound{ID: x.InboundID, Remark: x.Remark, Protocol: inbounds.MTProto})
		}
		spec.MTG = nil
	}
	if !canAWG && !canMTG {
		return "" // an older agent: it runs Xray only
	}
	raw := []byte("{}") // nothing to run (clears what an earlier push started)
	if !spec.Empty() {
		if raw, err = json.Marshal(spec); err != nil {
			st.Synced = false
			return "engines: " + err.Error()
		}
	}
	hash := Hash(raw)
	if info.EnginesHash == hash || (spec.Empty() && info.EnginesHash == "") {
		if len(info.EngineErrors) > 0 && !spec.Empty() {
			return strings.Join(info.EngineErrors, "; ")
		}
		return ""
	}
	m.mu.Lock()
	rj, wasRejected := m.engRejected[n.ID]
	m.mu.Unlock()
	if wasRejected && rj.hash == hash && time.Since(rj.at) < 5*time.Minute {
		st.Synced = false
		return "the node rejected its AmneziaWG / MTProto inbounds"
	}
	var after Info
	if err := m.call(ctx, hc, http.MethodPut, baseURL(n)+"/v1/engines", EnginesPush{Hash: hash, Spec: raw}, &after); err != nil {
		m.mu.Lock()
		m.engRejected[n.ID] = rejection{hash: hash, at: time.Now()}
		m.mu.Unlock()
		slog.Warn("node engines push failed", "node", n.Name, "err", err)
		st.Synced = false
		return "engines: " + err.Error()
	}
	m.mu.Lock()
	delete(m.engRejected, n.ID)
	m.mu.Unlock()
	slog.Info("node engines pushed", "node", n.Name, "amneziawg", len(spec.AWG), "mtproto", len(spec.MTG))
	return ""
}

// OnlineRound asks every reachable node whose agent lists online users for
// them (one request each, in parallel, never blocking the caller). Older
// agents keep the traffic rule.
func (m *Manager) OnlineRound(ctx context.Context) {
	m.init()
	var ns []db.Node
	if err := m.DB.Where("enabled = ?", true).Order("id").Find(&ns).Error; err != nil {
		return
	}
	for i := range ns {
		n := ns[i]
		m.mu.Lock()
		st := m.states[n.ID]
		skip := st == nil || !st.liveOnline || (st.Status != StateOnline && st.Status != StateDegraded) || m.onlineIn[n.ID]
		if t, ok := m.noOnline[n.ID]; ok && time.Since(t) < 5*time.Minute {
			skip = true
		}
		if !skip {
			m.onlineIn[n.ID] = true
		}
		m.mu.Unlock()
		if skip {
			continue
		}
		go func() {
			defer func() {
				m.mu.Lock()
				delete(m.onlineIn, n.ID)
				m.mu.Unlock()
			}()
			util.Safe("node online "+n.Name, func() { m.PollOnline(ctx, &n) })
		}()
	}
}

// PollOnline asks one node for its online users.
func (m *Manager) PollOnline(ctx context.Context, n *db.Node) {
	m.init()
	hc, err := m.client(n)
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var or OnlineReply
	if err := m.call(cctx, hc, http.MethodGet, baseURL(n)+"/v1/online", nil, &or); err != nil {
		if strings.Contains(err.Error(), ErrOnlineUnsupported.Error()) {
			m.mu.Lock()
			m.noOnline[n.ID] = time.Now()
			m.mu.Unlock()
			m.Presence.NotLive(Source(n.ID))
		}
		return
	}
	m.mu.Lock()
	delete(m.noOnline, n.ID)
	m.mu.Unlock()
	m.Presence.ReportLive(Source(n.ID), or.Users, time.Now().UTC())
	if m.OnIPs != nil && len(or.IPs) > 0 && n.Enabled {
		skew := time.Duration(0)
		if !or.Now.IsZero() {
			skew = time.Now().UTC().Sub(or.Now)
		}
		util.Safe("node ips", func() { m.OnIPs(or.IPs, skew) })
	}
}

// call does one JSON request; any non-2xx answer becomes an error.
func (m *Manager) call(ctx context.Context, hc *http.Client, method, u string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	rctx, cancel := context.WithTimeout(ctx, m.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, method, u, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return shortNetErr(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return errorf("%s", e.Error)
		}
		return fmt.Errorf("node answered HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// shortNetErr turns url.Error chains into one readable line.
func shortNetErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errors.New("timed out")
	}
	return errorf("%v", err)
}

// Reset pushes an empty config (no inbounds) to a node, best effort, so a
// node that is removed from the panel stops serving users.
func (m *Manager) Reset(ctx context.Context, n *db.Node) error {
	m.init()
	hc, err := m.client(n)
	if err != nil {
		return err
	}
	var info Info
	if err := m.call(ctx, hc, http.MethodGet, baseURL(n)+"/v1/info", nil, &info); err != nil {
		return err
	}
	apiPort := info.APIPort
	if apiPort == 0 {
		apiPort = DefaultAPIPort
	}
	cfg, _, err := m.BuildConfig(m.DB, m.DataDir, nil, apiPort)
	if err != nil {
		return err
	}
	if cfg, err = compact(cfg); err != nil {
		return err
	}
	if info.HasCap(CapAWG) || info.HasCap(CapMTProto) {
		empty := []byte("{}")
		if err := m.call(ctx, hc, http.MethodPut, baseURL(n)+"/v1/engines", EnginesPush{Hash: Hash(empty), Spec: empty}, nil); err != nil {
			slog.Warn("node engines reset failed", "node", n.Name, "err", err)
		}
	}
	return m.call(ctx, hc, http.MethodPut, baseURL(n)+"/v1/config", ConfigPush{Hash: Hash(cfg), Config: cfg}, nil)
}

// Caps returns the capabilities a node's agent announced; known is false
// while the node was never reached.
func (m *Manager) Caps(id int64) (caps []string, known bool) {
	m.init()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[id]
	if st == nil || st.Version == "" {
		return nil, false
	}
	return append([]string(nil), st.Caps...), true
}

// InboundIDs returns each node's inbound ids.
func InboundIDs(gdb *gorm.DB) map[int64][]int64 {
	var rows []db.NodeInbound
	gdb.Order("node_id, inbound_id").Find(&rows)
	out := map[int64][]int64{}
	for _, r := range rows {
		out[r.NodeID] = append(out[r.NodeID], r.InboundID)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i] < out[k][j] })
	}
	return out
}

// compact puts the config in the form it travels in (json.RawMessage is
// compacted when encoded), so both sides hash the same bytes.
func compact(cfg []byte) ([]byte, error) {
	var b bytes.Buffer
	if err := json.Compact(&b, cfg); err != nil {
		return nil, fmt.Errorf("node config is not valid JSON: %w", err)
	}
	return b.Bytes(), nil
}

// sendBans gives the node the IPs dropped for the IP limit when the list
// changed (and at least every minute, so a restarted node gets them again).
// It never fails the round: an old agent without the endpoint is noted and
// asked again ten minutes later.
func (m *Manager) sendBans(ctx context.Context, hc *http.Client, n *db.Node) {
	if m.Bans == nil || !n.Enabled {
		return
	}
	var bans []guard.Ban
	util.Safe("node bans", func() { bans = m.Bans() })
	if bans == nil {
		bans = []guard.Ban{}
	}
	var shapes []guard.Shape
	if m.Shapes != nil {
		util.Safe("node shapes", func() { shapes = m.Shapes() })
	}
	if shapes == nil {
		shapes = []guard.Shape{}
	}
	ips := make([]string, len(bans))
	for i, b := range bans {
		ips[i] = b.IP
	}
	sig := strings.Join(ips, ",")
	for _, s := range shapes {
		sig += fmt.Sprintf("|%d:%d:%d:%s", s.UserID, s.DownKbps, s.UpKbps, strings.Join(s.IPs, ","))
	}
	m.mu.Lock()
	prev := m.bansSent[n.ID]
	m.mu.Unlock()
	switch {
	case prev.unsupported && time.Since(prev.at) < 10*time.Minute:
		return
	case !prev.unsupported && prev.sig == sig && time.Since(prev.at) < time.Minute:
		return
	case !prev.unsupported && prev.at.IsZero() && len(bans) == 0 && len(shapes) == 0:
		m.mu.Lock()
		m.bansSent[n.ID] = bansState{sig: sig, at: time.Now()}
		m.mu.Unlock()
		return
	}
	err := m.call(ctx, hc, http.MethodPut, baseURL(n)+"/v1/bans", BansPush{Bans: bans, Shapes: shapes}, nil)
	st := bansState{sig: sig, at: time.Now()}
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "HTTP 405") {
			if !prev.unsupported {
				slog.Warn("node does not take IP-limit bans (update the node to v0.3.2 or later)", "node", n.Name)
			}
			st.unsupported = true
		} else {
			st.sig = "" // retry next round
			slog.Debug("node bans not sent", "node", n.Name, "err", err)
		}
	}
	m.mu.Lock()
	m.bansSent[n.ID] = st
	m.mu.Unlock()
}
