package nodes

// Master side of Xray-core installs on nodes: the panel asks a node's agent
// to install a release tag (the version picker offers the same releases as
// the main server's Xray card) and follows it until the node reports the
// outcome. After a successful install the node reports its new version in
// the next round, so inbounds that were left out for an old Xray-core come
// back on their own.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// XrayUpdateGap is the shortest time between two Xray install requests for
// one node.
const XrayUpdateGap = 10 * time.Second

// XrayUpdateDeadline: an install with no outcome after this long is failed.
const XrayUpdateDeadline = 12 * time.Minute

// XrayFollowEvery is how often the master asks a node about a running
// install (between the normal rounds).
var XrayFollowEvery = 2 * time.Second

func (u *updates) initXray() {
	if u.xstate == nil {
		u.xstate = map[int64]*UpdateStatus{}
		u.xlast = map[int64]time.Time{}
		u.xstarting = map[int64]bool{}
	}
}

// XrayUpdateState returns a copy of a node's Xray install (nil when none
// ran since the panel started).
func (m *Manager) XrayUpdateState(id int64) *UpdateStatus {
	u := m.upd()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.initXray()
	if st := u.xstate[id]; st != nil {
		c := *st
		return &c
	}
	return nil
}

// NormalizeXrayTag adds the "v" of a release tag ("26.10.31" -> "v26.10.31").
func NormalizeXrayTag(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// StartXrayUpdate asks node n to install Xray-core release version. It
// checks the node live first and returns an *UpdateError when refused
// (codes: bad_version, agent_unsupported, offline, up_to_date, in_progress,
// rate_limited, node_refused).
func (m *Manager) StartXrayUpdate(ctx context.Context, n *db.Node, version string, admin *db.Admin, ip string) (*UpdateStatus, error) {
	version = NormalizeXrayTag(version)
	if !xray.ValidTag(version) {
		return nil, updErr(400, "bad_version", "%q is not an Xray-core release tag (such as v26.10.31)", version)
	}
	u := m.upd()
	u.mu.Lock()
	u.initXray()
	if st := u.xstate[n.ID]; (st != nil && !st.Final()) || u.xstarting[n.ID] {
		u.mu.Unlock()
		return nil, updErr(409, "in_progress", "an Xray-core install on node %s is already running", n.Name)
	}
	if last, ok := u.xlast[n.ID]; ok && time.Since(last) < XrayUpdateGap {
		wait := (XrayUpdateGap - time.Since(last)).Round(time.Second)
		u.mu.Unlock()
		return nil, updErr(429, "rate_limited", "node %s was asked moments ago; try again in %s", n.Name, wait)
	}
	u.xstarting[n.ID] = true
	u.xlast[n.ID] = time.Now()
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.xstarting, n.ID)
		u.mu.Unlock()
	}()

	hc, err := m.client(n)
	if err != nil {
		return nil, updErr(409, "offline", "node %s: %v", n.Name, err)
	}
	var info Info
	if err := m.call(ctx, hc, http.MethodGet, baseURL(n)+"/v1/info", nil, &info); err != nil {
		return nil, updErr(409, "offline", "node %s is not reachable: %v", n.Name, err)
	}
	if !info.HasCap(CapXrayInstall) {
		return nil, updErr(409, "agent_unsupported", "node %s runs agent %s, which cannot install Xray-core; update the node's agent first", n.Name, orDash(info.Version))
	}
	if info.XrayVersion != "" && strings.TrimPrefix(version, "v") == info.XrayVersion {
		return nil, updErr(409, "up_to_date", "node %s already runs Xray-core %s", n.Name, info.XrayVersion)
	}
	if r := info.XrayUpdate; r != nil && !r.Final() {
		return nil, updErr(409, "in_progress", "node %s is already installing Xray-core %s", n.Name, r.Target)
	}
	var started UpdateStatus
	if err := m.call(ctx, hc, http.MethodPost, baseURL(n)+"/v1/xray", XrayInstallRequest{Version: version}, &started); err != nil {
		if strings.Contains(err.Error(), "already") {
			return nil, updErr(409, "in_progress", "node %s: %v", n.Name, err)
		}
		return nil, updErr(502, "node_refused", "node %s refused the install: %v", n.Name, err)
	}
	now := time.Now().UTC()
	st := &UpdateStatus{Target: version, From: info.XrayVersion, Phase: UpdDownloading, StartedAt: now, UpdatedAt: now,
		By: adminName(admin), agentStart: started.StartedAt}
	u.mu.Lock()
	u.xstate[n.ID] = st
	c := *st
	u.mu.Unlock()
	audit.Log(m.DB, admin, "node.xray.requested", fmt.Sprintf("%s: %s → %s", n.Name, orDash(info.XrayVersion), version), ip)
	node := *n
	go util.Safe("node xray follow "+n.Name, func() { m.followXray(&node) })
	return &c, nil
}

// followXray asks the node about its install every XrayFollowEvery until
// it ends, so the panel shows each step, then asks for a round (the new
// version re-renders the node's config).
func (m *Manager) followXray(n *db.Node) {
	end := time.Now().Add(XrayUpdateDeadline + time.Minute)
	for time.Now().Before(end) {
		time.Sleep(XrayFollowEvery)
		st := m.XrayUpdateState(n.ID)
		if st == nil || st.Final() {
			break
		}
		hc, err := m.client(n)
		if err != nil {
			m.trackXray(n, nil)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), m.RequestTimeout)
		var rep *UpdateStatus
		err = m.call(ctx, hc, http.MethodGet, baseURL(n)+"/v1/xray", nil, &rep)
		cancel()
		if err != nil {
			m.trackXray(n, nil)
			continue
		}
		if rep != nil && rep.Final() && m.syncNow(n) {
			// The round saw the outcome together with the node's new
			// version, so the panel never shows "done" next to the old one.
			continue
		}
		m.trackXray(n, &Info{XrayUpdate: rep})
	}
	m.Kick()
}

// syncNow runs a round for node n at once unless one is already running
// for it (then it returns false).
func (m *Manager) syncNow(n *db.Node) bool {
	m.mu.Lock()
	busy := m.inflight[n.ID]
	if !busy {
		m.inflight[n.ID] = true
	}
	m.mu.Unlock()
	if busy {
		return false
	}
	defer func() {
		m.mu.Lock()
		delete(m.inflight, n.ID)
		m.mu.Unlock()
	}()
	var cur db.Node
	if err := m.DB.First(&cur, n.ID).Error; err != nil {
		return false
	}
	util.Safe("node "+cur.Name, func() { m.SyncNode(context.Background(), &cur) })
	st := m.XrayUpdateState(n.ID)
	return st != nil && st.Final()
}

// trackXray follows a running Xray install with what the node reported
// (info nil = the node did not answer) and writes the outcome to the audit
// log once.
func (m *Manager) trackXray(n *db.Node, info *Info) {
	u := m.upd()
	u.mu.Lock()
	u.initXray()
	st := u.xstate[n.ID]
	// A panel restarted mid-install picks the node's own report up again.
	if st == nil && info != nil && info.XrayUpdate != nil && !info.XrayUpdate.Final() && time.Since(info.XrayUpdate.UpdatedAt) < XrayUpdateDeadline {
		c := *info.XrayUpdate
		c.agentStart = c.StartedAt
		st = &c
		u.xstate[n.ID] = st
	}
	if st == nil || st.Final() {
		u.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	outcome := ""
	if info != nil {
		if r := info.XrayUpdate; r != nil && r.Target == st.Target && (st.agentStart.IsZero() || r.StartedAt.Equal(st.agentStart)) {
			st.Phase, st.Error, st.RolledBack = r.Phase, r.Error, r.RolledBack
			switch r.Phase {
			case UpdDone:
				outcome = "ok"
			case UpdFailed:
				outcome = "failed"
			}
		}
	}
	if outcome == "" && now.Sub(st.StartedAt) > XrayUpdateDeadline {
		st.Phase, st.Error = UpdFailed, fmt.Sprintf("no outcome from the node within %s", XrayUpdateDeadline)
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
	target := fmt.Sprintf("%s: %s → %s", n.Name, orDash(c.From), c.Target)
	if outcome == "failed" {
		reason := c.Error
		if len(reason) > 300 {
			reason = reason[:300] + "…"
		}
		if c.RolledBack {
			reason += " (rolled back)"
		}
		target += ": " + reason
	}
	audit.Log(m.DB, admin, "node.xray."+outcome, target, "")
	m.Kick()
}
