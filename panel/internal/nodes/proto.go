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
}

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
	// Shapes is the speed-limit plan: users with a speed limit and their
	// live IPs. The node shapes the IPs connected to it the same way as
	// the main server (nil from masters older than v0.3.2 = unchanged).
	Shapes []guard.Shape `json:"shapes"`
}

// Hash is the config hash both sides agree on.
func Hash(cfg []byte) string {
	s := sha256.Sum256(cfg)
	return hex.EncodeToString(s[:])
}
