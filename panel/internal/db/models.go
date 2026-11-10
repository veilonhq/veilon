package db

import "time"

// Admin is a panel administrator.
type Admin struct {
	ID           int64  `gorm:"primaryKey" json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	IsSudo       bool   `json:"is_sudo"`
	// Permissions is a JSON object of permission flags (sub-admins only).
	Permissions string `json:"-"`
	// UserQuota caps how many users a sub-admin may own (0 = unlimited).
	UserQuota int `json:"user_quota"`
	// TrafficQuota caps the total data limit a sub-admin may hand out
	// across its users, in bytes (0 = unlimited).
	TrafficQuota      int64      `json:"traffic_quota"`
	PasswordChangedAt *time.Time `json:"-"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// User statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusLimited  = "limited"
	StatusExpired  = "expired"
	StatusOnHold   = "on_hold"
)

// Reset strategies.
const (
	ResetNone  = "no_reset"
	ResetDay   = "day"
	ResetWeek  = "week"
	ResetMonth = "month"
)

// User is a proxy user (client).
type User struct {
	ID            int64 `gorm:"primaryKey"`
	Username      string
	Status        string
	DataLimit     int64
	Up            int64
	Down          int64
	ExpireAt      *time.Time
	OnHoldDays    *int
	ResetStrategy string
	LastResetAt   *time.Time
	Note          string
	UUID          string `gorm:"column:uuid"`
	Password      string
	SubToken      string
	LastOnlineAt  *time.Time

	// IPLimit is the number of client IPs allowed at the same time (0 = any).
	IPLimit int `gorm:"column:ip_limit"`
	// DeviceLimit is the number of subscription devices (HWIDs) allowed.
	DeviceLimit int
	// RequireHWID refuses subscriptions from clients that send no HWID.
	RequireHWID bool `gorm:"column:require_hwid"`
	// Speed limits in kilobits per second (0 = unlimited).
	SpeedDownKbps int64
	SpeedUpKbps   int64
	// TrafficMultiplier scales counted usage (1 = real usage).
	TrafficMultiplier float64
	// DailyLimit caps counted usage per panel-time day (0 = none).
	DailyLimit   int64
	DailyUsed    int64
	DailyResetAt *time.Time
	BlockTorrent bool
	// Allowed time window in panel time ("HH:MM"; empty = always) and
	// weekdays as a bitmask (bit 0 = Sunday; 0 = every day).
	WindowFrom string
	WindowTo   string
	WindowDays int
	// SuspendReason is set while a served user is kept out of the cores
	// temporarily: "window" (outside the allowed time window) or "daily"
	// (daily cap reached). Empty when not suspended.
	SuspendReason string
	GroupID       *int64
	// AdminID is the sub-admin that owns this user (nil = sudo-owned).
	AdminID   *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Suspend reasons.
const (
	SuspendWindow = "window"
	SuspendDaily  = "daily"
)

// Group is a named set of users with defaults for new members.
type Group struct {
	ID      int64 `gorm:"primaryKey"`
	Name    string
	Color   string
	AdBlock bool
	// InboundIDs is a JSON array of default inbound ids for new members.
	InboundIDs string `gorm:"column:inbound_ids"`
	// Defaults is a JSON object of default limits for new members.
	Defaults  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the table name.
func (Group) TableName() string { return "user_groups" }

// UserIP is one client IP seen for a user in the core access log.
type UserIP struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	IP        string `gorm:"column:ip"`
	Country   string
	FirstSeen time.Time
	LastSeen  time.Time
	Hits      int64
	Blocked   bool
}

// TableName pins the table name.
func (UserIP) TableName() string { return "user_ips" }

// UserDevice is one device (HWID) that fetched a user's subscription.
type UserDevice struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64
	HWID      string `gorm:"column:hwid"`
	OS        string `gorm:"column:os"`
	OSVersion string `gorm:"column:os_version"`
	Model     string
	UserAgent string
	IP        string `gorm:"column:ip"`
	FirstSeen time.Time
	LastSeen  time.Time
	Hits      int64
	Blocked   bool
}

// Multiplier returns the effective traffic multiplier (>0).
func (u *User) Multiplier() float64 {
	if u.TrafficMultiplier <= 0 {
		return 1
	}
	return u.TrafficMultiplier
}

// Used returns total consumed bytes.
func (u *User) Used() int64 { return u.Up + u.Down }

// UserInbound links users to inbounds.
type UserInbound struct {
	UserID    int64 `gorm:"primaryKey;autoIncrement:false"`
	InboundID int64 `gorm:"primaryKey;autoIncrement:false"`
}

// Inbound is an Xray inbound definition. TLS, Reality and Transport are JSON
// blobs stored as text so the schema stays portable.
type Inbound struct {
	ID              int64 `gorm:"primaryKey"`
	Remark          string
	Enabled         bool
	Protocol        string
	Port            int
	Listen          string
	Network         string
	Security        string
	TLS             string `gorm:"column:tls"`
	Reality         string
	Transport       string
	Flow            string
	SSMethod        string `gorm:"column:ss_method"`
	SSServerKey     string `gorm:"column:ss_server_key"`
	Sniffing        bool
	AddressOverride string
	// ServeMain: the main server serves the inbound too. Only read when
	// the inbound is assigned to nodes (no nodes = main server only).
	ServeMain bool `gorm:"column:serve_main"`
	// Settings holds protocol-specific options (JSON) for the protocols that
	// do not use the shared stream fields (hysteria, wireguard, tuic...).
	Settings  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Host overrides how clients reach an inbound: one share/subscription link
// per enabled host, in Sort order. Empty fields keep the inbound's value.
type Host struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	InboundID   int64     `json:"inbound_id"`
	Remark      string    `json:"remark"`
	Address     string    `json:"address"`
	Port        int       `json:"port"`
	SNI         string    `gorm:"column:sni" json:"sni"`
	Host        string    `json:"host"`
	Path        string    `json:"path"`
	Fingerprint string    `json:"fingerprint"`
	ALPN        string    `gorm:"column:alpn" json:"alpn"`
	Enabled     bool      `json:"enabled"`
	Sort        int       `json:"sort"`
	CreatedAt   time.Time `json:"-"`
	UpdatedAt   time.Time `json:"-"`
}

// TableName pins the table name.
func (Host) TableName() string { return "inbound_hosts" }

// Node is a remote server running "veilon node". The master pushes the
// Xray config for the node's inbounds to it over mutual TLS and collects its
// traffic. Address is what clients connect to (it appears in subscription
// links); the agent listens on Address:APIPort.
type Node struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	Name       string     `json:"name"`
	Address    string     `json:"address"`
	APIPort    int        `gorm:"column:api_port" json:"api_port"`
	Enabled    bool       `json:"enabled"`
	CertSHA256 string     `gorm:"column:cert_sha256" json:"-"`
	Up         int64      `json:"up"`
	Down       int64      `json:"down"`
	StatsSeq   string     `json:"-"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"-"`
}

// NodeInbound assigns an inbound to a node.
type NodeInbound struct {
	NodeID    int64 `gorm:"primaryKey"`
	InboundID int64 `gorm:"primaryKey"`
}

// WGPeer is the auto-generated WireGuard / AmneziaWG peer of a user on one
// inbound: its key pair, preshared key and tunnel address.
type WGPeer struct {
	InboundID    int64 `gorm:"primaryKey;autoIncrement:false"`
	UserID       int64 `gorm:"primaryKey;autoIncrement:false"`
	PrivateKey   string
	PublicKey    string
	PresharedKey string
	Address      string
	CreatedAt    time.Time
}

// TableName pins the table name.
func (WGPeer) TableName() string { return "wg_peers" }

// Setting is a key/value runtime setting.
type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// Cert is a TLS certificate.
type Cert struct {
	ID        int64 `gorm:"primaryKey"`
	Domain    string
	Issuer    string
	NotAfter  time.Time
	AutoRenew bool
	Source    string // acme | upload | self
	Email     string
	// Kind is "domain" or "ip"; Domain holds the identifiers, comma separated.
	Kind string
	// Profile is the ACME certificate profile ("" = CA default, "shortlived").
	Profile string
	// HTTPPort is the local port of the HTTP-01 challenge listener (default 80).
	HTTPPort  int `gorm:"column:http_port"`
	NotBefore *time.Time
	// LastError is the error of the last failed renewal ("" after success).
	LastError     string
	LastAttemptAt *time.Time
	CertPEM       string `gorm:"column:cert_pem"`
	KeyPEM        string `gorm:"column:key_pem"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AuditLog records admin actions.
type AuditLog struct {
	ID        int64 `gorm:"primaryKey"`
	AdminID   int64
	AdminName string
	Action    string
	Target    string
	IP        string `gorm:"column:ip"`
	CreatedAt time.Time
}

// TrafficPoint is system-wide traffic during one sampling interval.
type TrafficPoint struct {
	ID   int64 `gorm:"primaryKey"`
	T    time.Time
	Up   int64
	Down int64
}

// TableName pins the table name.
func (Node) TableName() string { return "nodes" }

// TableName pins the table name.
func (NodeInbound) TableName() string { return "node_inbounds" }
