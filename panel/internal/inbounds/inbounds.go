// Package inbounds manages Xray inbound definitions.
package inbounds

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// TLS holds TLS parameters of an inbound.
type TLS struct {
	CertID        *int64   `json:"cert_id"`
	SNI           string   `json:"sni"`
	ALPN          []string `json:"alpn"`
	Fingerprint   string   `json:"fingerprint"`
	AllowInsecure bool     `json:"allow_insecure"`
}

// Reality holds REALITY parameters of an inbound.
type Reality struct {
	Dest        string   `json:"dest"`
	ServerNames []string `json:"server_names"`
	PrivateKey  string   `json:"private_key"`
	PublicKey   string   `json:"public_key"`
	ShortIDs    []string `json:"short_ids"`
	Fingerprint string   `json:"fingerprint"`
	SpiderX     string   `json:"spider_x"`
}

// Transport holds transport parameters.
type Transport struct {
	Path        string `json:"path"`
	Host        string `json:"host"`
	ServiceName string `json:"service_name"`
	Mode        string `json:"mode"`
}

// View is the API / domain representation of an inbound.
type View struct {
	ID        int64     `json:"id"`
	Remark    string    `json:"remark"`
	Enabled   bool      `json:"enabled"`
	Protocol  string    `json:"protocol"`
	Port      int       `json:"port"`
	Listen    string    `json:"listen"`
	Network   string    `json:"network"`
	Security  string    `json:"security"`
	TLS       *TLS      `json:"tls"`
	Reality   *Reality  `json:"reality"`
	Transport Transport `json:"transport"`
	Flow      string    `json:"flow"`
	// VLESSEnc is the VLESS Encryption setting (vless only, else null).
	VLESSEnc *VLESSEnc `json:"vless_encryption"`
	// Advanced holds optional security/transport settings (null = none,
	// the config older panels wrote).
	Advanced        *Advanced `json:"advanced"`
	SSMethod        string    `json:"ss_method"`
	SSServerKey     string    `json:"-"`
	Sniffing        bool      `json:"sniffing"`
	AddressOverride string    `json:"address_override"`
	Settings        Settings  `json:"settings"`
	UserCount       int64     `json:"user_count"`
	// HostCount is the number of hosts (link overrides) of the inbound.
	HostCount int64 `json:"host_count"`
	// NodeIDs are the nodes that run the inbound ([] = main server only).
	NodeIDs []int64 `json:"node_ids"`
	// ServeMain: the main server runs the inbound as well as its nodes.
	// New placements set it false when nodes are chosen; inbounds that were
	// assigned to nodes before this field existed keep it true.
	ServeMain bool `json:"serve_main"`
	// OnMain (read-only) reports whether the main server runs the inbound:
	// no nodes, or ServeMain.
	OnMain    bool      `json:"on_main"`
	CreatedAt time.Time `json:"created_at"`
	// XrayNeeds (read-only) lists the inbound's features that need a
	// minimum Xray-core version; XrayTooOld (read-only) the servers known
	// to run an older one, where the inbound is left out.
	XrayNeeds  []XrayNeed   `json:"xray_needs,omitempty"`
	XrayTooOld []XrayTooOld `json:"xray_too_old,omitempty"`
}

// XrayTooOld is a server (node_id 0 = the main server) whose Xray-core is
// too old for an inbound.
type XrayTooOld struct {
	NodeID  int64      `json:"node_id"`
	Version string     `json:"version"`
	Needs   []XrayNeed `json:"needs"`
}

// fillXray sets the read-only Xray version fields of v.
func (s *Service) fillXray(v *View) {
	v.XrayNeeds = XrayNeeds(v)
	if len(v.XrayNeeds) == 0 || s.XrayVersion == nil {
		return
	}
	servers := v.NodeIDs
	if v.OnMain {
		servers = append([]int64{0}, servers...)
	}
	for _, id := range servers {
		ver := s.XrayVersion(id)
		if miss := XrayMissing(v, ver); len(miss) > 0 {
			v.XrayTooOld = append(v.XrayTooOld, XrayTooOld{NodeID: id, Version: ver, Needs: miss})
		}
	}
}

// Tag is the Xray inbound tag for this inbound.
func (v *View) Tag() string { return fmt.Sprintf("inbound-%d", v.ID) }

// Allowed values.
var (
	Protocols  = []string{VLESS, VMess, Trojan, Shadowsocks, Hysteria, TUIC, WireGuard, AmneziaWG, HTTP, Mixed, Socks, Tunnel, TUN, MTProto, Masque}
	Networks   = []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp", "kcp"}
	Securities = []string{"none", "tls", "reality"}
	SSMethods  = []string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"}
	XHTTPModes = []string{"auto", "packet-up", "stream-up", "stream-one"}
)

// ValidationError is returned for bad input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ConflictError reports that another inbound (or the panel) already uses the
// requested port or name. Field is "port" or "remark"; Other is the remark of
// the inbound that holds it ("" when the port is reserved: the panel's own
// ports on the main server, a node's agent port). For a port, Server is
// "main" or "node" (then NodeID and NodeName name the node).
type ConflictError struct {
	Field    string
	Other    string
	Msg      string
	Port     int
	Server   string
	NodeID   int64
	NodeName string
	// Moving names the inbound that would move back to the main server
	// (its last node is removed) and cannot, because of this port.
	MovingID int64
	Moving   string
}

func (e *ConflictError) Error() string { return e.Msg }

// NodeAgentError: a chosen node's agent is too old to run this protocol
// (it has to be updated first).
type NodeAgentError struct {
	NodeID   int64
	NodeName string
	Protocol string
	Msg      string
}

func (e *NodeAgentError) Error() string { return e.Msg }

func verr(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

// RealityFingerprints are the client fingerprints that work with Reality
// (checked with Xray 26.10.31: the others, and random choices among them,
// do not complete the post-quantum key share Reality servers use).
var RealityFingerprints = []string{"chrome", "firefox", "safari"}

func in(s string, list []string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IsSS2022 reports whether method is a Shadowsocks 2022 cipher.
func IsSS2022(method string) bool { return strings.HasPrefix(method, "2022-") }

// SS2022KeyLen returns the PSK length in bytes for a 2022 method.
func SS2022KeyLen(method string) int {
	if strings.Contains(method, "aes-128") {
		return 16
	}
	return 32
}

// SS2022UserKey derives a per-user PSK of the right length from the user's password.
func SS2022UserKey(method, password string) string {
	h := sha256Sum([]byte(password))
	return base64.StdEncoding.EncodeToString(h[:SS2022KeyLen(method)])
}

// FromModel converts a DB row.
func FromModel(m *db.Inbound) View {
	v := View{
		ID: m.ID, Remark: m.Remark, Enabled: m.Enabled, Protocol: m.Protocol, Port: m.Port,
		Listen: m.Listen, Network: m.Network, Security: m.Security, Flow: m.Flow,
		SSMethod: m.SSMethod, SSServerKey: m.SSServerKey, Sniffing: m.Sniffing,
		AddressOverride: m.AddressOverride, CreatedAt: m.CreatedAt, ServeMain: m.ServeMain, NodeIDs: []int64{},
	}
	if m.TLS != "" {
		var t TLS
		if json.Unmarshal([]byte(m.TLS), &t) == nil {
			v.TLS = &t
		}
	}
	if m.Reality != "" {
		var r Reality
		if json.Unmarshal([]byte(m.Reality), &r) == nil {
			v.Reality = &r
		}
	}
	if m.Transport != "" {
		_ = json.Unmarshal([]byte(m.Transport), &v.Transport)
	}
	if m.Settings != "" {
		var st storedSettings
		if json.Unmarshal([]byte(m.Settings), &st) == nil {
			v.Settings, v.VLESSEnc, v.Advanced = st.Settings, st.VLESSEnc, st.Advanced
		}
	}
	if v.Protocol == VLESS && v.VLESSEnc == nil {
		v.VLESSEnc = DefaultVLESSEnc()
	}
	if v.Protocol != VLESS {
		v.VLESSEnc = nil
	}
	fixNil(&v.Settings)
	if v.TLS != nil && v.TLS.ALPN == nil {
		v.TLS.ALPN = []string{}
	}
	return v
}

func toModel(v *View, m *db.Inbound) {
	m.Remark, m.Enabled, m.Protocol, m.Port = v.Remark, v.Enabled, v.Protocol, v.Port
	m.Listen, m.Network, m.Security, m.Flow = v.Listen, v.Network, v.Security, v.Flow
	m.SSMethod, m.SSServerKey, m.Sniffing, m.AddressOverride = v.SSMethod, v.SSServerKey, v.Sniffing, v.AddressOverride
	m.ServeMain = v.ServeMain || len(v.NodeIDs) == 0
	m.TLS, m.Reality = "", ""
	if v.TLS != nil {
		b, _ := json.Marshal(v.TLS)
		m.TLS = string(b)
	}
	if v.Reality != nil {
		b, _ := json.Marshal(v.Reality)
		m.Reality = string(b)
	}
	b, _ := json.Marshal(v.Transport)
	m.Transport = string(b)
	st := storedSettings{Settings: v.Settings, Advanced: v.Advanced}
	if v.Protocol == VLESS {
		st.VLESSEnc = v.VLESSEnc
	}
	b, _ = json.Marshal(st)
	m.Settings = string(b)
}

// storedSettings is the settings column: the protocol settings plus, for
// VLESS, the encryption setting (kept here so no schema change is needed;
// older panels ignore the key).
type storedSettings struct {
	Settings
	VLESSEnc *VLESSEnc `json:"vless_encryption,omitempty"`
	Advanced *Advanced `json:"advanced,omitempty"`
}

// fixNil makes list fields render as [] instead of null.
func fixNil(s *Settings) {
	if s.DNS == nil {
		s.DNS = []string{}
	}
	if s.Gateway == nil {
		s.Gateway = []string{}
	}
	if s.Routes == nil {
		s.Routes = []string{}
	}
	if s.Pool == nil {
		s.Pool = []string{}
	}
}

// Normalize validates v and fills defaults. It does not check cross-row
// constraints (ports, certificates).
func Normalize(v *View) error {
	v.Remark = strings.TrimSpace(v.Remark)
	if v.Remark == "" || len(v.Remark) > 128 {
		return verr("remark is required (max 128 chars)")
	}
	if !in(v.Protocol, Protocols) {
		return verr("protocol must be one of %s", strings.Join(Protocols, ", "))
	}
	if v.Network == "" || !IsV2(v.Protocol) {
		v.Network = "tcp"
	}
	if !in(v.Network, Networks) {
		return verr("network must be one of %s", strings.Join(Networks, ", "))
	}
	if v.Security == "" {
		v.Security = "none"
	}
	if !in(v.Security, Securities) {
		return verr("security must be one of %s", strings.Join(Securities, ", "))
	}
	if v.Protocol != TUN && (v.Port < 1 || v.Port > 65535) {
		return verr("port must be 1-65535")
	}
	v.Listen = strings.TrimSpace(v.Listen)
	if v.Listen != "" && net.ParseIP(v.Listen) == nil {
		return verr("listen must be an IP address or empty")
	}
	v.AddressOverride = strings.TrimSpace(v.AddressOverride)
	if !IsV2(v.Protocol) {
		if v.Security == "reality" {
			return verr("reality is supported for vless and trojan only")
		}
		v.VLESSEnc = nil
		err := normalizeProtocol(v)
		fixNil(&v.Settings)
		if err != nil {
			return err
		}
		return normalizeAdvanced(v)
	}
	v.Settings = Settings{}
	fixNil(&v.Settings)

	// VLESS Encryption.
	if v.Protocol == VLESS {
		if v.VLESSEnc == nil {
			v.VLESSEnc = DefaultVLESSEnc()
		}
		if err := normalizeVLESSEnc(v.VLESSEnc); err != nil {
			return err
		}
	} else {
		v.VLESSEnc = nil
	}

	// Protocol specifics.
	if v.Protocol == "shadowsocks" {
		if v.Security == "reality" {
			return verr("shadowsocks inbounds do not support reality")
		}
		if v.SSMethod == "" {
			v.SSMethod = "chacha20-ietf-poly1305"
		}
		if !in(v.SSMethod, SSMethods) {
			return verr("ss_method must be one of %s", strings.Join(SSMethods, ", "))
		}
		if IsSS2022(v.SSMethod) {
			if b, err := base64.StdEncoding.DecodeString(v.SSServerKey); err != nil || len(b) != SS2022KeyLen(v.SSMethod) {
				k := make([]byte, SS2022KeyLen(v.SSMethod))
				_, _ = rand.Read(k)
				v.SSServerKey = base64.StdEncoding.EncodeToString(k)
			}
		} else {
			v.SSServerKey = ""
		}
	} else {
		v.SSMethod, v.SSServerKey = "", ""
	}

	// Security.
	switch v.Security {
	case "none":
		v.TLS, v.Reality = nil, nil
	case "tls":
		v.Reality = nil
		if v.TLS == nil || v.TLS.CertID == nil {
			return verr("tls.cert_id is required for security tls")
		}
		if v.TLS.ALPN == nil {
			v.TLS.ALPN = []string{}
		}
		for _, a := range v.TLS.ALPN {
			if !in(a, []string{"h2", "http/1.1", "h3"}) {
				return verr("invalid alpn %q", a)
			}
		}
		v.TLS.SNI = strings.TrimSpace(v.TLS.SNI)
	case "reality":
		v.TLS = nil
		if v.Protocol != "vless" && v.Protocol != "trojan" {
			return verr("reality is supported for vless and trojan only")
		}
		if v.Network != "tcp" && v.Network != "grpc" && v.Network != "xhttp" {
			return verr("reality supports network tcp, grpc or xhttp")
		}
		r := v.Reality
		if r == nil {
			return verr("reality settings are required")
		}
		r.Dest = strings.TrimSpace(r.Dest)
		if r.Dest == "" {
			return verr("reality.dest is required (e.g. www.example.com:443)")
		}
		if _, _, err := net.SplitHostPort(r.Dest); err != nil {
			r.Dest = net.JoinHostPort(r.Dest, "443")
		}
		var names []string
		for _, n := range r.ServerNames {
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			h, _, _ := net.SplitHostPort(r.Dest)
			if net.ParseIP(h) != nil {
				return verr("reality.server_names is required when dest is an IP")
			}
			names = []string{h}
		}
		r.ServerNames = names
		if strings.TrimSpace(r.PrivateKey) == "" {
			k, err := GenerateRealityKeys()
			if err != nil {
				return err
			}
			r.PrivateKey = k.PrivateKey
		}
		pub, err := PublicKeyFromPrivate(r.PrivateKey)
		if err != nil {
			return verr("reality.private_key: %v", err)
		}
		r.PublicKey = pub
		var sids []string
		for _, s := range r.ShortIDs {
			s = strings.ToLower(strings.TrimSpace(s))
			if len(s) > 16 || len(s)%2 != 0 {
				return verr("reality short id %q must be even-length hex up to 16 chars", s)
			}
			if _, err := hex.DecodeString(s); err != nil {
				return verr("reality short id %q is not hex", s)
			}
			sids = append(sids, s)
		}
		if len(sids) == 0 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			sids = []string{hex.EncodeToString(b)}
		}
		r.ShortIDs = sids
		if r.Fingerprint == "" {
			r.Fingerprint = "chrome"
		}
		if !in(r.Fingerprint, RealityFingerprints) {
			return verr("reality.fingerprint must be one of %s", strings.Join(RealityFingerprints, ", "))
		}
		if r.SpiderX == "" {
			r.SpiderX = "/"
		}
	}

	// Flow.
	if v.Flow != "" {
		if v.Flow != "xtls-rprx-vision" {
			return verr("flow must be empty or xtls-rprx-vision")
		}
		// VLESS Encryption carries Vision on every transport (tested with
		// Xray 26.10.31: tcp, ws, grpc, httpupgrade, xhttp).
		if v.Protocol != "vless" || !v.VLESSEnc.On() && (v.Network != "tcp" || v.Security == "none") {
			return verr("xtls-rprx-vision requires vless with vless encryption, or vless over tcp with tls or reality")
		}
	}

	// Transport.
	t := &v.Transport
	t.Path, t.Host, t.ServiceName, t.Mode = strings.TrimSpace(t.Path), strings.TrimSpace(t.Host), strings.TrimSpace(t.ServiceName), strings.TrimSpace(t.Mode)
	switch v.Network {
	case "tcp", "kcp":
		*t = Transport{}
	case "ws", "httpupgrade", "xhttp":
		if t.Path == "" {
			t.Path = "/"
		}
		if !strings.HasPrefix(t.Path, "/") {
			t.Path = "/" + t.Path
		}
		t.ServiceName = ""
		if v.Network == "xhttp" {
			if t.Mode == "" {
				t.Mode = "auto"
			}
			if !in(t.Mode, XHTTPModes) {
				return verr("transport.mode must be one of %s", strings.Join(XHTTPModes, ", "))
			}
		} else {
			t.Mode = ""
		}
	case "grpc":
		if t.ServiceName == "" {
			t.ServiceName = "grpc"
		}
		t.Path, t.Host = "", ""
		if t.Mode != "" && t.Mode != "gun" && t.Mode != "multi" {
			return verr("grpc transport.mode must be gun or multi")
		}
	}
	return normalizeAdvanced(v)
}

// Service provides CRUD for inbounds.
type Service struct {
	DB *gorm.DB
	// ReservedPorts returns ports that inbounds may not use (panel, xray api).
	ReservedPorts func() []int
	// NodeCaps returns what a node's agent announced (known is false when
	// the node was never reached). nil = not checked.
	NodeCaps func(nodeID int64) (caps []string, known bool)
	// XrayVersion returns the Xray-core version of the main server
	// (nodeID 0) or of a node ("" = unknown). nil = not checked.
	XrayVersion func(nodeID int64) string
	// OnChange is called after any mutation (triggers an Xray reload).
	OnChange func()
	// SelfSigned creates a self-signed certificate for domain and returns
	// its id (used by QUIC protocols created without a certificate).
	SelfSigned func(domain string) (int64, error)
}

func (s *Service) changed() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

// ErrNotFound is returned when the inbound does not exist.
var ErrNotFound = errors.New("inbound not found")

// List returns all inbounds with user counts.
func (s *Service) List() ([]View, error) {
	var rows []db.Inbound
	if err := s.DB.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	counts := map[int64]int64{}
	type cnt struct {
		InboundID int64
		N         int64
	}
	var cs []cnt
	s.DB.Model(&db.UserInbound{}).Select("inbound_id, COUNT(*) AS n").Group("inbound_id").Scan(&cs)
	for _, c := range cs {
		counts[c.InboundID] = c.N
	}
	hosts := HostCounts(s.DB)
	placed := Placements(s.DB)
	out := make([]View, 0, len(rows))
	for i := range rows {
		v := FromModel(&rows[i])
		v.UserCount = counts[v.ID]
		v.HostCount = hosts[v.ID]
		if ids := placed[v.ID]; ids != nil {
			v.NodeIDs = ids
		}
		v.OnMain = v.ServeMain || len(v.NodeIDs) == 0
		s.fillXray(&v)
		out = append(out, v)
	}
	return out, nil
}

// Get returns one inbound.
func (s *Service) Get(id int64) (*View, error) {
	var m db.Inbound
	if err := s.DB.First(&m, id).Error; err != nil {
		if db.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	v := FromModel(&m)
	s.DB.Model(&db.UserInbound{}).Where("inbound_id = ?", id).Count(&v.UserCount)
	s.DB.Model(&db.Host{}).Where("inbound_id = ?", id).Count(&v.HostCount)
	if ids := Placements(s.DB)[id]; ids != nil {
		v.NodeIDs = ids
	}
	v.OnMain = v.ServeMain || len(v.NodeIDs) == 0
	s.fillXray(&v)
	return &v, nil
}

func listenOverlap(a, b string) bool {
	any := func(x string) bool { return x == "" || x == "0.0.0.0" || x == "::" }
	return any(a) || any(b) || a == b
}

// checkUnique enforces that no two inbounds share a name (case-insensitive,
// trimmed) and that no two inbounds on the same server share a port
// (whatever the protocol or transport; see ports.go), and that no inbound
// takes a port reserved on one of its servers. v.NodeIDs must already be
// checked (checkNodes). prev is the stored inbound on update: a value that
// did not change is not re-checked, so inbounds stored before a rule
// existed stay editable; with the same port only servers the inbound did
// not run on before are checked.
func (s *Service) checkUnique(v *View, prev *db.Inbound) error {
	nameChanged := prev == nil || !strings.EqualFold(strings.TrimSpace(prev.Remark), v.Remark)
	if nameChanged {
		var rows []db.Inbound
		if err := s.DB.Select("id", "remark").Where("id <> ?", v.ID).Find(&rows).Error; err != nil {
			return err
		}
		for _, o := range rows {
			if strings.EqualFold(strings.TrimSpace(o.Remark), v.Remark) {
				return &ConflictError{Field: "remark", Other: o.Remark,
					Msg: fmt.Sprintf("name %q is already used by another inbound", o.Remark)}
			}
		}
	}
	if v.Protocol == TUN {
		return nil
	}
	want := serversOf(v.ServeMain, v.NodeIDs)
	if prev != nil && prev.Port == v.Port && prev.Protocol != TUN {
		want = want.minus(serversOf(prev.ServeMain, Placements(s.DB)[prev.ID]))
	}
	return s.checkPort(v.ID, v.Port, want)
}

func (s *Service) checkCross(v *View) error {
	tcp, udp := Transports(v)
	if v.Protocol != TUN {
		var rows []db.Inbound
		s.DB.Where("port = ? AND id <> ?", v.Port, v.ID).Find(&rows)
		mine := serversOf(v.ServeMain, v.NodeIDs)
		placed := Placements(s.DB)
		for i := range rows {
			o := FromModel(&rows[i])
			// Only inbounds on one of the same servers can clash.
			if _, _, ok := mine.shared(serversOf(o.ServeMain, placed[o.ID])); !ok {
				continue
			}
			if !listenOverlap(o.Listen, v.Listen) {
				continue
			}
			ot, ou := Transports(&o)
			switch {
			case tcp && ot:
				return verr("port %d/tcp is already used by inbound %q", v.Port, o.Remark)
			case udp && ou:
				return verr("port %d/udp is already used by inbound %q", v.Port, o.Remark)
			}
		}
	} else {
		var rows []db.Inbound
		s.DB.Where("protocol = ? AND id <> ?", TUN, v.ID).Find(&rows)
		for i := range rows {
			o := FromModel(&rows[i])
			if o.Settings.TunName == v.Settings.TunName {
				return verr("TUN interface %q is already used by inbound %q", v.Settings.TunName, o.Remark)
			}
		}
	}
	if v.Security == "tls" && TLSRequired(v.Protocol) && v.TLS.CertID == nil {
		if s.SelfSigned == nil {
			return verr("tls.cert_id is required")
		}
		sni := v.TLS.SNI
		if sni == "" {
			sni = "www.bing.com"
		}
		// Reuse an existing self-signed certificate for the same name.
		var c db.Cert
		var id int64
		if s.DB.Where("source = ? AND domain = ?", "self", strings.ToLower(sni)).Order("id").Limit(1).Find(&c); c.ID != 0 {
			id = c.ID
		} else {
			var err error
			if id, err = s.SelfSigned(sni); err != nil {
				return err
			}
		}
		v.TLS.CertID = &id
		v.TLS.SNI = sni
		v.TLS.AllowInsecure = true
	}
	if v.Security == "tls" {
		var c db.Cert
		if err := s.DB.First(&c, *v.TLS.CertID).Error; err != nil {
			return verr("tls.cert_id %d does not exist", *v.TLS.CertID)
		}
		if v.TLS.SNI == "" {
			v.TLS.SNI = c.Domain
		}
		if c.Source == "self" {
			// Clients can only connect to a self-signed certificate with
			// verification off; links pin the certificate instead.
			v.TLS.AllowInsecure = true
		}
	}
	if v.ReverseOn() {
		var n int64
		s.DB.Model(&db.User{}).Where("LOWER(uuid) = ?", v.Advanced.Reverse.UUID).Count(&n)
		if n > 0 {
			return verr("reverse.uuid is a user's UUID; use another one")
		}
	}
	if IsWG(v.Protocol) {
		var rows []db.Inbound
		s.DB.Where("protocol IN ? AND id <> ?", []string{WireGuard, AmneziaWG}, v.ID).Find(&rows)
		mine, _ := netip.ParsePrefix(v.Settings.Address)
		for i := range rows {
			o := FromModel(&rows[i])
			if p, err := netip.ParsePrefix(o.Settings.Address); err == nil && p.Masked().Overlaps(mine.Masked()) {
				return verr("subnet %s overlaps inbound %q (%s)", v.Settings.Address, o.Remark, o.Settings.Address)
			}
		}
	}
	return nil
}

// checkEncXray refuses an inbound whose features (VLESS Encryption and the
// advanced settings, see XrayNeeds) need a newer Xray-core than the main
// server or a chosen node is known to run. Unknown versions (a node never
// reached, Xray not started yet) are let through; if such a server turns
// out to be too old, the config build leaves the inbound out there instead
// of breaking the server's other inbounds.
//
// On update (prev set), a need the stored inbound already had on the same
// server is not re-checked, so an inbound left out on an old node can still
// be edited (or moved off that node).
func (s *Service) checkEncXray(v *View, prev *View) error {
	if s.XrayVersion == nil || len(XrayNeeds(v)) == 0 {
		return nil
	}
	serversOf := func(x *View) []int64 {
		l := []int64{}
		if x.ServeMain || len(x.NodeIDs) == 0 {
			l = append(l, 0)
		}
		return append(l, x.NodeIDs...)
	}
	had := map[int64]bool{}
	if prev != nil {
		for _, id := range serversOf(prev) {
			had[id] = true
		}
	}
	for _, id := range serversOf(v) {
		ver := s.XrayVersion(id)
		miss := XrayMissing(v, ver)
		if had[id] && len(miss) > 0 {
			old := map[string]bool{}
			for _, n := range XrayMissing(prev, ver) {
				old[n.Feature] = true
			}
			kept := miss[:0]
			for _, n := range miss {
				if !old[n.Feature] {
					kept = append(kept, n)
				}
			}
			miss = kept
		}
		if len(miss) == 0 {
			continue
		}
		where := "the main server"
		if id != 0 {
			var n db.Node
			s.DB.Select("id", "name").First(&n, id)
			where = fmt.Sprintf("node %q", n.Name)
		}
		if len(miss) == 1 {
			return verr("%s needs Xray-core %s or newer; %s runs %s", miss[0].Feature, miss[0].Min, where, ver)
		}
		return verr("%s runs Xray-core %s, too old for: %s", where, ver, XrayMissingText(miss))
	}
	return nil
}

// Defaults returns a View pre-filled with defaults for create requests.
func Defaults() View {
	return View{Enabled: true, Sniffing: true, Network: "tcp", Security: "none", ServeMain: true, NodeIDs: []int64{}}
}

// Create validates and stores a new inbound.
func (s *Service) Create(v *View) (*View, error) {
	v.ID = 0
	if err := Normalize(v); err != nil {
		return nil, err
	}
	if err := s.checkNodes(v); err != nil {
		return nil, err
	}
	if err := s.checkEncXray(v, nil); err != nil {
		return nil, err
	}
	if err := s.checkUnique(v, nil); err != nil {
		return nil, err
	}
	if err := s.checkCross(v); err != nil {
		return nil, err
	}
	var m db.Inbound
	toModel(v, &m)
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		return setNodes(tx, m.ID, v.NodeIDs)
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	return s.Get(m.ID)
}

// Update replaces an inbound with the merged view v (v.ID must be set).
func (s *Service) Update(v *View) (*View, error) {
	var m db.Inbound
	if err := s.DB.First(&m, v.ID).Error; err != nil {
		if db.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := Normalize(v); err != nil {
		return nil, err
	}
	prev := m
	if err := s.checkNodes(v); err != nil {
		return nil, err
	}
	old, err := s.Get(v.ID)
	if err != nil {
		return nil, err
	}
	if err := s.checkEncXray(v, old); err != nil {
		return nil, err
	}
	if err := s.checkUnique(v, &prev); err != nil {
		return nil, err
	}
	if err := s.checkCross(v); err != nil {
		return nil, err
	}
	toModel(v, &m)
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		return setNodes(tx, m.ID, v.NodeIDs)
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	return s.Get(m.ID)
}

// Delete removes an inbound and its user links.
func (s *Service) Delete(id int64) error {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&db.Inbound{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&db.WGPeer{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&db.Host{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&db.NodeInbound{}).Error; err != nil {
			return err
		}
		return tx.Where("inbound_id = ?", id).Delete(&db.UserInbound{}).Error
	})
	if err == nil {
		s.changed()
	}
	return err
}
