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
	ID              int64     `json:"id"`
	Remark          string    `json:"remark"`
	Enabled         bool      `json:"enabled"`
	Protocol        string    `json:"protocol"`
	Port            int       `json:"port"`
	Listen          string    `json:"listen"`
	Network         string    `json:"network"`
	Security        string    `json:"security"`
	TLS             *TLS      `json:"tls"`
	Reality         *Reality  `json:"reality"`
	Transport       Transport `json:"transport"`
	Flow            string    `json:"flow"`
	SSMethod        string    `json:"ss_method"`
	SSServerKey     string    `json:"-"`
	Sniffing        bool      `json:"sniffing"`
	AddressOverride string    `json:"address_override"`
	Settings        Settings  `json:"settings"`
	UserCount       int64     `json:"user_count"`
	// HostCount is the number of hosts (link overrides) of the inbound.
	HostCount int64     `json:"host_count"`
	CreatedAt time.Time `json:"created_at"`
}

// Tag is the Xray inbound tag for this inbound.
func (v *View) Tag() string { return fmt.Sprintf("inbound-%d", v.ID) }

// Allowed values.
var (
	Protocols  = []string{VLESS, VMess, Trojan, Shadowsocks, Hysteria, TUIC, WireGuard, AmneziaWG, HTTP, Mixed, Tunnel, TUN, MTProto, Masque}
	Networks   = []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"}
	Securities = []string{"none", "tls", "reality"}
	SSMethods  = []string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"}
	XHTTPModes = []string{"auto", "packet-up", "stream-up", "stream-one"}
)

// ValidationError is returned for bad input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ConflictError reports that another inbound (or the panel) already uses the
// requested port or name. Field is "port" or "remark"; Other is the remark of
// the inbound that holds it ("" when the panel itself reserves the port).
type ConflictError struct {
	Field string
	Other string
	Msg   string
}

func (e *ConflictError) Error() string { return e.Msg }

func verr(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

// RealityFingerprints are the client fingerprints that work with Reality.
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
		AddressOverride: m.AddressOverride, CreatedAt: m.CreatedAt,
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
		_ = json.Unmarshal([]byte(m.Settings), &v.Settings)
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
	b, _ = json.Marshal(v.Settings)
	m.Settings = string(b)
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
		err := normalizeProtocol(v)
		fixNil(&v.Settings)
		return err
	}
	v.Settings = Settings{}
	fixNil(&v.Settings)

	// Protocol specifics.
	if v.Protocol == "shadowsocks" {
		if v.Security != "none" {
			return verr("shadowsocks inbounds do not support tls/reality")
		}
		if v.Network != "tcp" {
			return verr("shadowsocks inbounds support network tcp only")
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
		if v.Protocol != "vless" || v.Network != "tcp" || v.Security == "none" {
			return verr("xtls-rprx-vision requires vless over tcp with tls or reality")
		}
	}

	// Transport.
	t := &v.Transport
	t.Path, t.Host, t.ServiceName, t.Mode = strings.TrimSpace(t.Path), strings.TrimSpace(t.Host), strings.TrimSpace(t.ServiceName), strings.TrimSpace(t.Mode)
	switch v.Network {
	case "tcp":
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
	return nil
}

// Service provides CRUD for inbounds.
type Service struct {
	DB *gorm.DB
	// ReservedPorts returns ports that inbounds may not use (panel, xray api).
	ReservedPorts func() []int
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
	out := make([]View, 0, len(rows))
	for i := range rows {
		v := FromModel(&rows[i])
		v.UserCount = counts[v.ID]
		v.HostCount = hosts[v.ID]
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
	return &v, nil
}

func listenOverlap(a, b string) bool {
	any := func(x string) bool { return x == "" || x == "0.0.0.0" || x == "::" }
	return any(a) || any(b) || a == b
}

// checkUnique enforces that no two inbounds share a port (whatever the
// protocol or transport) or a name (case-insensitive, trimmed), and that no
// inbound takes a port the panel itself uses. prev is the stored inbound on
// update: a value that did not change is not re-checked, so inbounds stored
// before this rule existed stay editable.
func (s *Service) checkUnique(v *View, prev *db.Inbound) error {
	var rows []db.Inbound
	if err := s.DB.Select("id", "remark", "port", "protocol").Where("id <> ?", v.ID).Find(&rows).Error; err != nil {
		return err
	}
	nameChanged := prev == nil || !strings.EqualFold(strings.TrimSpace(prev.Remark), v.Remark)
	if nameChanged {
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
	if prev != nil && prev.Port == v.Port && prev.Protocol != TUN {
		return nil
	}
	if s.ReservedPorts != nil {
		for _, p := range s.ReservedPorts() {
			if p == v.Port {
				return &ConflictError{Field: "port",
					Msg: fmt.Sprintf("port %d is used by the panel itself", v.Port)}
			}
		}
	}
	for _, o := range rows {
		if o.Protocol != TUN && o.Port == v.Port {
			return &ConflictError{Field: "port", Other: o.Remark,
				Msg: fmt.Sprintf("port %d is already used by inbound %q", v.Port, o.Remark)}
		}
	}
	return nil
}

func (s *Service) checkCross(v *View) error {
	tcp, udp := Transports(v)
	if v.Protocol != TUN {
		var rows []db.Inbound
		s.DB.Where("port = ? AND id <> ?", v.Port, v.ID).Find(&rows)
		for i := range rows {
			o := FromModel(&rows[i])
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

// Defaults returns a View pre-filled with defaults for create requests.
func Defaults() View {
	return View{Enabled: true, Sniffing: true, Network: "tcp", Security: "none"}
}

// Create validates and stores a new inbound.
func (s *Service) Create(v *View) (*View, error) {
	v.ID = 0
	if err := Normalize(v); err != nil {
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
	if err := s.DB.Create(&m).Error; err != nil {
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
	if err := s.checkUnique(v, &prev); err != nil {
		return nil, err
	}
	if err := s.checkCross(v); err != nil {
		return nil, err
	}
	toModel(v, &m)
	if err := s.DB.Save(&m).Error; err != nil {
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
