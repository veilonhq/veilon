package nodes

// Master side of node agent updates: the panel asks a node to update its
// agent to the main server's own version, from the same release source as
// the panel's self-update (VEILON_UPDATE_API, if set), and follows the
// update through the node's reports.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/selfupdate"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// UpdateError says why a node update was refused. Code is stable for the
// panel: agent_unsupported, arch_unknown, not_release, up_to_date,
// downgrade, in_progress, rate_limited, offline, release_unavailable,
// node_refused.
type UpdateError struct {
	Code   string
	Msg    string
	Status int
}

func (e *UpdateError) Error() string { return e.Msg }

func updErr(status int, code, format string, a ...any) *UpdateError {
	return &UpdateError{Code: code, Status: status, Msg: fmt.Sprintf(format, a...)}
}

// UpdateGap is the shortest time between two update requests for one node.
const UpdateGap = 30 * time.Second

// UpdateDeadline: an update with no outcome after this long is failed.
const UpdateDeadline = 15 * time.Minute

// updates is the master's update bookkeeping (part of Manager).
type updates struct {
	mu       sync.Mutex
	state    map[int64]*UpdateStatus
	lastReq  map[int64]time.Time
	starting map[int64]bool
	rel      map[string]relEntry
	relMu    sync.Mutex
	// Xray-core installs (xrayupdate.go)
	xstate    map[int64]*UpdateStatus
	xlast     map[int64]time.Time
	xstarting map[int64]bool
}

type relEntry struct {
	rel *selfupdate.Release
	sum string // checksums.txt
	at  time.Time
}

func (m *Manager) upd() *updates {
	m.init()
	return &m.ups
}

func (u *updates) initMaps() {
	if u.state == nil {
		u.state = map[int64]*UpdateStatus{}
		u.lastReq = map[int64]time.Time{}
		u.starting = map[int64]bool{}
		u.rel = map[string]relEntry{}
	}
}

// UpdateState returns a copy of a node's update (nil when none ran since
// the panel started).
func (m *Manager) UpdateState(id int64) *UpdateStatus {
	u := m.upd()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.initMaps()
	if st := u.state[id]; st != nil {
		c := *st
		return &c
	}
	return nil
}

// Outdated reports whether a node's agent version is older than the main
// server's (both stable release versions).
func (m *Manager) Outdated(agentVersion string) bool {
	return selfupdate.Newer(m.Version, agentVersion)
}

func (m *Manager) releaseAPI() string {
	if m.ReleaseAPI != "" {
		return m.ReleaseAPI
	}
	return selfupdate.APIURL()
}

// release returns the main server's own release and its checksums.txt,
// cached for ten minutes (an "update all" asks once).
func (m *Manager) release(ctx context.Context, tag string) (*selfupdate.Release, string, error) {
	u := m.upd()
	u.relMu.Lock()
	defer u.relMu.Unlock()
	u.mu.Lock()
	u.initMaps()
	e, ok := u.rel[tag]
	u.mu.Unlock()
	if ok && time.Since(e.at) < 10*time.Minute {
		return e.rel, e.sum, nil
	}
	cl := m.ReleaseClient
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	rel, err := selfupdate.FetchTag(ctx, cl, m.releaseAPI(), tag)
	if err != nil {
		return nil, "", err
	}
	if rel == nil {
		return nil, "", fmt.Errorf("release %s is not published in the release source", tag)
	}
	sumURL := rel.Assets["checksums.txt"]
	if sumURL == "" {
		return nil, "", fmt.Errorf("release %s has no checksums.txt", tag)
	}
	var b strings.Builder
	if err := selfupdate.Download(ctx, cl, sumURL, &b); err != nil {
		return nil, "", fmt.Errorf("checksums.txt: %w", err)
	}
	u.mu.Lock()
	u.rel[tag] = relEntry{rel: rel, sum: b.String(), at: time.Now()}
	u.mu.Unlock()
	return rel, b.String(), nil
}

func adminName(a *db.Admin) string {
	if a == nil {
		return ""
	}
	return a.Username
}

// StartUpdate asks node n to update its agent to the main server's
// version. It checks the node live (version, architecture, capability)
// first and returns an *UpdateError when the update is refused.
func (m *Manager) StartUpdate(ctx context.Context, n *db.Node, admin *db.Admin, ip string) (*UpdateStatus, error) {
	u := m.upd()
	u.mu.Lock()
	u.initMaps()
	if st := u.state[n.ID]; (st != nil && !st.Final()) || u.starting[n.ID] {
		u.mu.Unlock()
		return nil, updErr(409, "in_progress", "an update of node %s is already running", n.Name)
	}
	if last, ok := u.lastReq[n.ID]; ok && time.Since(last) < UpdateGap {
		wait := (UpdateGap - time.Since(last)).Round(time.Second)
		u.mu.Unlock()
		return nil, updErr(429, "rate_limited", "node %s was asked to update moments ago; try again in %s", n.Name, wait)
	}
	u.starting[n.ID] = true
	u.lastReq[n.ID] = time.Now()
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.starting, n.ID)
		u.mu.Unlock()
	}()

	if _, ok := selfupdate.ParseVersion(m.Version); !ok {
		return nil, updErr(409, "not_release", "the main server runs %q, not a release version; nodes can only be updated to a release", m.Version)
	}
	hc, err := m.client(n)
	if err != nil {
		return nil, updErr(409, "offline", "node %s: %v", n.Name, err)
	}
	var info Info
	if err := m.call(ctx, hc, http.MethodGet, baseURL(n)+"/v1/info", nil, &info); err != nil {
		return nil, updErr(409, "offline", "node %s is not reachable: %v", n.Name, err)
	}
	if !info.HasCap(CapUpdate) {
		return nil, updErr(409, "agent_unsupported", "node %s runs agent %s, which cannot update itself; run the node's new install command once", n.Name, orDash(info.Version))
	}
	switch {
	case selfupdate.Newer(info.Version, m.Version):
		return nil, updErr(409, "downgrade", "node %s runs %s, newer than the main server's %s; refusing to downgrade", n.Name, info.Version, m.Version)
	case !selfupdate.Newer(m.Version, info.Version):
		if _, ok := selfupdate.ParseVersion(info.Version); !ok {
			return nil, updErr(409, "not_release", "node %s runs %q, not a release version", n.Name, info.Version)
		}
		return nil, updErr(409, "up_to_date", "node %s already runs %s", n.Name, info.Version)
	}
	if info.Update != nil && !info.Update.Final() && time.Since(info.Update.UpdatedAt) < staleUpdate {
		return nil, updErr(409, "in_progress", "node %s is already updating to %s", n.Name, info.Update.Target)
	}
	asset := ""
	switch info.Arch {
	case "amd64", "arm64":
		asset = "veilon-linux-" + info.Arch
	default:
		return nil, updErr(409, "arch_unknown", "node %s did not report a supported CPU architecture (%q)", n.Name, info.Arch)
	}
	rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	rel, sums, err := m.release(rctx, m.Version)
	cancel()
	if err != nil {
		return nil, updErr(502, "release_unavailable", "release %s: %v", m.Version, err)
	}
	binURL := rel.Assets[asset]
	want := selfupdate.ChecksumFor(sums, asset)
	if binURL == "" || want == "" {
		return nil, updErr(502, "release_unavailable", "release %s has no %s or no checksum for it", m.Version, asset)
	}
	var started UpdateStatus
	if err := m.call(ctx, hc, http.MethodPost, baseURL(n)+"/v1/update", UpdateRequest{Version: m.Version, URL: binURL, SHA256: want}, &started); err != nil {
		if strings.Contains(err.Error(), "already running") {
			return nil, updErr(409, "in_progress", "node %s: %v", n.Name, err)
		}
		return nil, updErr(502, "node_refused", "node %s refused the update: %v", n.Name, err)
	}
	now := time.Now().UTC()
	st := &UpdateStatus{Target: m.Version, From: info.Version, Phase: UpdDownloading, StartedAt: now, UpdatedAt: now, By: adminName(admin),
		agentStart: started.StartedAt}
	u.mu.Lock()
	u.state[n.ID] = st
	c := *st
	u.mu.Unlock()
	audit.Log(m.DB, admin, "node.update.requested", fmt.Sprintf("%s: %s → %s", n.Name, info.Version, m.Version), ip)
	m.Kick()
	return &c, nil
}

func orDash(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

// trackUpdate follows a running update with what the node reported this
// round (info nil = the node did not answer). It writes the outcome to the
// audit log once.
func (m *Manager) trackUpdate(n *db.Node, info *Info) {
	u := m.upd()
	u.mu.Lock()
	u.initMaps()
	st := u.state[n.ID]
	// A panel restarted mid-update picks the node's own report up again.
	if st == nil && info != nil && info.Update != nil && !info.Update.Final() && time.Since(info.Update.UpdatedAt) < staleUpdate {
		c := *info.Update
		st = &c
		u.state[n.ID] = st
	}
	if st == nil || st.Final() {
		u.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	var outcome string
	if info == nil && (st.Phase == UpdDownloading || st.Phase == UpdVerifying) {
		// The node took the update and stopped answering: it is restarting
		// (the download and the check run while it still answers).
		st.Phase = UpdRestarting
	}
	switch {
	case info != nil && info.Version == st.Target:
		st.Phase, st.Error = UpdDone, ""
		outcome = "ok"
	case info != nil && info.Update != nil && info.Update.Target == st.Target && (st.agentStart.IsZero() || info.Update.StartedAt.Equal(st.agentStart)):
		r := info.Update
		if r.Phase == UpdDone {
			// the helper is done but this agent is not the new version
			// (it was replaced again): leave it to the version check
			break
		}
		st.Phase, st.Error, st.RolledBack = r.Phase, r.Error, r.RolledBack
		if r.Phase == UpdFailed {
			outcome = "failed"
		}
	}
	if outcome == "" && now.Sub(st.StartedAt) > UpdateDeadline {
		st.Phase, st.Error = UpdFailed, fmt.Sprintf("no outcome from the node within %s", UpdateDeadline)
		outcome = "failed"
	}
	st.UpdatedAt = now
	if outcome != "" {
		st.FinishedAt = &now
	}
	c := *st
	u.mu.Unlock()
	if outcome == "" {
		return
	}
	var admin *db.Admin
	if c.By != "" {
		a := db.Admin{Username: c.By}
		m.DB.Where("username = ?", c.By).Limit(1).Find(&a)
		admin = &a
	}
	target := fmt.Sprintf("%s: %s → %s", n.Name, c.From, c.Target)
	if outcome == "failed" {
		reason := c.Error
		if len(reason) > 300 {
			reason = reason[:300] + "…"
		}
		target += ": " + reason
	}
	audit.Log(m.DB, admin, "node.update."+outcome, target, "")
}

// UpdateResult is one node's answer in "update all outdated".
type UpdateResult struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// UpdateOutdated starts an update on every node whose agent (as last
// seen) is older than the main server; the others are left alone. Each
// node is asked in parallel; the results are in node order.
func (m *Manager) UpdateOutdated(ctx context.Context, admin *db.Admin, ip string) ([]UpdateResult, error) {
	var ns []db.Node
	if err := m.DB.Order("id").Find(&ns).Error; err != nil {
		return nil, err
	}
	var mu sync.Mutex
	out := []UpdateResult{}
	var wg sync.WaitGroup
	for i := range ns {
		n := ns[i]
		st := m.Status(&n)
		if st.Version == "" || !m.Outdated(st.Version) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := UpdateResult{ID: n.ID, Name: n.Name}
			util.Safe("node update "+n.Name, func() {
				_, err := m.StartUpdate(ctx, &n, admin, ip)
				var ue *UpdateError
				switch {
				case err == nil:
					r.OK = true
				case errors.As(err, &ue):
					r.Code, r.Message = ue.Code, ue.Msg
				default:
					r.Code, r.Message = "error", err.Error()
				}
			})
			mu.Lock()
			out = append(out, r)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
