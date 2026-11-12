package nodes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/veilonhq/veilon/panel/internal/guard"
)

// DefaultPort is the port "veilon node" listens on.
const DefaultPort = 62050

// DefaultAPIPort is the loopback port of the node's Xray stats API.
const DefaultAPIPort = 62051

// Info is what a node reports about itself (GET /v1/info).
type Info struct {
	NodeID      int64     `json:"node_id"`
	Version     string    `json:"version"`
	XrayVersion string    `json:"xray_version"`
	XrayRunning bool      `json:"xray_running"`
	XrayError   string    `json:"xray_error,omitempty"`
	ConfigHash  string    `json:"config_hash"`
	APIPort     int       `json:"api_port"`
	StartedAt   time.Time `json:"started_at"`
	Load1       float64   `json:"load1"`
	MemUsed     float64   `json:"mem_used"` // 0..1
	CPUs        int       `json:"cpus"`
	// Arch is the node's CPU architecture (GOARCH: amd64, arm64), so the
	// master picks the right release asset. Empty from agents before v0.3.5.
	Arch string `json:"arch,omitempty"`
	// Caps lists what the agent supports beyond the base protocol
	// (CapUpdate: POST /v1/update). Empty from agents before v0.3.5.
	Caps []string `json:"caps,omitempty"`
	// Update is the agent's last self-update (nil when none ran).
	Update *UpdateStatus `json:"update,omitempty"`
}

// CapUpdate: the agent updates itself on request (POST /v1/update).
const CapUpdate = "update"

// CapOnline: the agent lists the users with open connections on its Xray
// (GET /v1/online). Agents before v0.3.6 do not: the master keeps the
// traffic rule for them.
const CapOnline = "online"

// OnlineReply is the answer to GET /v1/online: the users with at least one
// open connection on the node's Xray, and the client IPs first seen since
// the last stats batch (agents after v0.3.6; older ones send them only with
// the stats batch).
type OnlineReply struct {
	Users []int64        `json:"users"`
	Now   time.Time      `json:"now"`
	IPs   []guard.SeenIP `json:"ips,omitempty"`
}

// HasCap reports whether the agent announced capability c.
func (in *Info) HasCap(c string) bool {
	for _, x := range in.Caps {
		if x == c {
			return true
		}
	}
	return false
}

// UpdateRequest is the body of POST /v1/update: the release the node
// downloads (the asset for its architecture) and the asset's SHA-256 from
// the release's checksums.txt.
type UpdateRequest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// Phases of a node update, in order; done and failed are final.
const (
	UpdDownloading = "downloading"
	UpdVerifying   = "verifying"
	UpdRestarting  = "restarting"
	UpdDone        = "done"
	UpdFailed      = "failed"
)

// UpdateStatus is the state of one node update. The agent and its update
// helper write it on the node; the master keeps its own copy for the panel.
type UpdateStatus struct {
	Target string `json:"target"`
	From   string `json:"from"`
	Phase  string `json:"phase"`
	Error  string `json:"error,omitempty"`
	// RolledBack: the new version did not come back and the previous
	// binary was restored.
	RolledBack bool       `json:"rolled_back,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// By is the admin who asked for it (master side only).
	By string `json:"by,omitempty"`
	// agentStart is the node's StartedAt for this update (master side),
	// so an older report for the same target is not mistaken for it.
	agentStart time.Time
}

// Final reports whether the update has ended.
func (u *UpdateStatus) Final() bool { return u.Phase == UpdDone || u.Phase == UpdFailed }

// ConfigPush is the body of PUT /v1/config.
type ConfigPush struct {
	Hash   string          `json:"hash"`
	Config json.RawMessage `json:"config"`
}

// StatsReply is the answer to GET /v1/stats?ack=<seq>. The node keeps a
// batch until the master acknowledges its Seq in the next request, so a lost
// answer is sent again instead of lost, and a repeated one is recognised by
// its Seq instead of counted twice.
type StatsReply struct {
	Seq   string             `json:"seq"`
	Users map[int64][2]int64 `json:"users"`
	Up    int64              `json:"up"`
	Down  int64              `json:"down"`
	// IPs are the client IPs seen per user since the previous batch (from
	// the node's Xray access log), for IP limits across servers.
	IPs []guard.SeenIP `json:"ips,omitempty"`
	// Now is the node's clock when it answered (the master corrects the
	// IP times for clock differences with it).
	Now time.Time `json:"now"`
}

// BansPush is the body of PUT /v1/bans: the client IPs the master drops for
// the IP limit, which the node drops on its inbound ports too.
type BansPush struct {
	Bans []guard.Ban `json:"bans"`
	// Shapes is the speed-limit plan: users with a speed limit (and their
	// live IPs, used only by older agents that shape per IP). The node shapes the
	// connections of those users that its own access log shows, the same
	// way as the main server (nil from masters older than v0.3.2 =
	// unchanged).
	Shapes []guard.Shape `json:"shapes"`
}

// Hash is the config hash both sides agree on.
func Hash(cfg []byte) string {
	s := sha256.Sum256(cfg)
	return hex.EncodeToString(s[:])
}
