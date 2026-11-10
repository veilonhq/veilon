package inbounds

import (
	"net"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// MaxHosts limits hosts per inbound.
const MaxHosts = 32

// HostInput is one host as the API receives it (order = list order).
type HostInput struct {
	Remark      string `json:"remark"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	SNI         string `json:"sni"`
	Host        string `json:"host"`
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
	ALPN        string `json:"alpn"`
	Enabled     *bool  `json:"enabled"`
}

var (
	hostnameRx   = regexp.MustCompile(`^(\*\.)?([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.?$`)
	fingerprints = map[string]bool{"": true, "chrome": true, "firefox": true, "safari": true, "ios": true, "android": true,
		"edge": true, "360": true, "qq": true, "random": true, "randomized": true}
	alpnValues = map[string]bool{"h3": true, "h2": true, "http/1.1": true}
)

func validHostname(s string) bool {
	if net.ParseIP(strings.Trim(s, "[]")) != nil {
		return true
	}
	return len(s) <= 253 && hostnameRx.MatchString(s)
}

// normalizeHost trims and checks a host; the returned error is a
// ValidationError naming the host's position.
func normalizeHost(i int, in HostInput) (db.Host, error) {
	h := db.Host{
		Remark: strings.TrimSpace(in.Remark), Address: strings.TrimSpace(in.Address), Port: in.Port,
		SNI: strings.TrimSpace(in.SNI), Host: strings.TrimSpace(in.Host), Path: strings.TrimSpace(in.Path),
		Fingerprint: strings.TrimSpace(in.Fingerprint), Enabled: in.Enabled == nil || *in.Enabled, Sort: i,
	}
	bad := func(f string, a ...any) (db.Host, error) {
		return db.Host{}, verr("host %d: "+f, append([]any{i + 1}, a...)...)
	}
	if len([]rune(h.Remark)) > 64 {
		return bad("name is longer than 64 characters")
	}
	if h.Address != "" && !validHostname(h.Address) {
		return bad("address %q is not a domain or IP", h.Address)
	}
	if h.Port < 0 || h.Port > 65535 {
		return bad("port must be 0 (same as the inbound) to 65535")
	}
	if h.SNI != "" && !validHostname(h.SNI) {
		return bad("SNI %q is not a domain", h.SNI)
	}
	if h.Host != "" && !validHostname(h.Host) {
		return bad("host header %q is not a domain", h.Host)
	}
	if h.Path != "" && (!strings.HasPrefix(h.Path, "/") || len(h.Path) > 255 || strings.ContainsAny(h.Path, " \t\n")) {
		return bad("path must start with / and have no spaces")
	}
	if !fingerprints[h.Fingerprint] {
		return bad("unknown fingerprint %q", h.Fingerprint)
	}
	var alpn []string
	for _, a := range strings.Split(in.ALPN, ",") {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		if !alpnValues[a] {
			return bad("ALPN %q is not one of h3, h2, http/1.1", a)
		}
		alpn = append(alpn, a)
	}
	h.ALPN = strings.Join(alpn, ",")
	return h, nil
}

// Hosts returns an inbound's hosts in order.
func (s *Service) Hosts(inboundID int64) ([]db.Host, error) {
	if _, err := s.Get(inboundID); err != nil {
		return nil, err
	}
	out := []db.Host{}
	err := s.DB.Where("inbound_id = ?", inboundID).Order("sort, id").Find(&out).Error
	return out, err
}

// SetHosts replaces an inbound's hosts with list (in that order).
func (s *Service) SetHosts(inboundID int64, list []HostInput) ([]db.Host, error) {
	if _, err := s.Get(inboundID); err != nil {
		return nil, err
	}
	if len(list) > MaxHosts {
		return nil, verr("an inbound can have at most %d hosts", MaxHosts)
	}
	rows := make([]db.Host, 0, len(list))
	for i, in := range list {
		h, err := normalizeHost(i, in)
		if err != nil {
			return nil, err
		}
		h.InboundID = inboundID
		rows = append(rows, h)
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("inbound_id = ?", inboundID).Delete(&db.Host{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Hosts(inboundID)
}

// EnabledHosts returns the enabled hosts of the given inbounds by inbound id.
func EnabledHosts(gdb *gorm.DB, inboundIDs []int64) map[int64][]db.Host {
	out := map[int64][]db.Host{}
	if len(inboundIDs) == 0 {
		return out
	}
	var rows []db.Host
	gdb.Where("enabled = ? AND inbound_id IN ?", true, inboundIDs).Order("inbound_id, sort, id").Find(&rows)
	for _, h := range rows {
		out[h.InboundID] = append(out[h.InboundID], h)
	}
	return out
}

// HostCounts returns how many hosts each inbound has.
func HostCounts(gdb *gorm.DB) map[int64]int64 {
	type cnt struct {
		InboundID int64
		N         int64
	}
	var cs []cnt
	gdb.Model(&db.Host{}).Select("inbound_id, COUNT(*) AS n").Group("inbound_id").Scan(&cs)
	out := map[int64]int64{}
	for _, c := range cs {
		out[c.InboundID] = c.N
	}
	return out
}
