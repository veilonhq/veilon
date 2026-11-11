package sub

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/users"
)

// SubURL builds the public subscription URL for a token. Host preference:
// sub_domain, panel_domain, server_address, then the request host. A host
// that already contains a port is used verbatim; otherwise the panel port is
// appended unless it is the scheme default.
func SubURL(s db.Settings, token, reqHost string) string {
	scheme := "http"
	if s.PanelCertID != nil {
		scheme = "https"
	}
	host := ""
	for _, h := range []string{s.SubDomain, s.PanelDomain, s.ServerAddress} {
		if h != "" {
			host = h
			break
		}
	}
	if host == "" {
		host = reqHost
	} else if _, _, err := net.SplitHostPort(host); err != nil {
		if !((scheme == "https" && s.PanelPort == 443) || (scheme == "http" && s.PanelPort == 80)) && s.PanelPort > 0 {
			host = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(s.PanelPort))
		}
	}
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s://%s/%s/%s", scheme, host, strings.Trim(s.SubPath, "/"), token)
}

// Service serves subscriptions.
type Service struct {
	DB *gorm.DB
	// UI holds the web build with the subscription page (nil: the build
	// embedded in the binary).
	UI fs.FS
}

// Resolve loads the user for a token and their enabled inbounds.
func (s *Service) Resolve(token string) (*db.User, []inbounds.View, error) {
	var u db.User
	if err := s.DB.Where("sub_token = ?", token).First(&u).Error; err != nil {
		return nil, nil, err
	}
	var rows []db.Inbound
	err := s.DB.Where("enabled = ? AND id IN (?)", true,
		s.DB.Model(&db.UserInbound{}).Select("inbound_id").Where("user_id = ?", u.ID)).Order("id").Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	ibs := make([]inbounds.View, 0, len(rows))
	for i := range rows {
		ibs = append(ibs, inbounds.FromModel(&rows[i]))
	}
	return &u, ibs, nil
}

// Extras loads certificate pins and the user's WireGuard peers.
func (s *Service) Extras(u *db.User, ibs []inbounds.View) Extras {
	ids := make([]int64, 0, len(ibs))
	for i := range ibs {
		ids = append(ids, ibs[i].ID)
	}
	ex := Extras{Pins: CertPinsFull(s.DB), Peers: map[int64]db.WGPeer{}, Hosts: inbounds.EnabledHosts(s.DB, ids), Nodes: inbounds.NodeTargets(s.DB, ids), NodeOnly: inbounds.NodeOnly(s.DB), Placed: inbounds.Placed(s.DB)}
	for i := range ibs {
		if !inbounds.IsWG(ibs[i].Protocol) {
			continue
		}
		if m, err := inbounds.EnsurePeers(s.DB, &ibs[i], []int64{u.ID}); err == nil {
			if p, ok := m[u.ID]; ok {
				ex.Peers[ibs[i].ID] = p
			}
		}
	}
	return ex
}

// CertPinsFull returns certificate and public-key pins by cert id.
func CertPinsFull(gdb *gorm.DB) map[int64]certs.Pins {
	var rows []db.Cert
	gdb.Select("id", "cert_pem").Find(&rows)
	out := make(map[int64]certs.Pins, len(rows))
	for _, c := range rows {
		if p, ok := certs.PinsOf(c.CertPEM); ok {
			out[c.ID] = p
		}
	}
	return out
}

// CertPins returns leaf certificate SHA-256 fingerprints (hex) by cert id.
func CertPins(gdb *gorm.DB) map[int64]string {
	var rows []db.Cert
	gdb.Select("id", "cert_pem").Find(&rows)
	out := make(map[int64]string, len(rows))
	for _, c := range rows {
		if blk, _ := pem.Decode([]byte(c.CertPEM)); blk != nil {
			sum := sha256.Sum256(blk.Bytes)
			out[c.ID] = hex.EncodeToString(sum[:])
		}
	}
	return out
}

// Info is the JSON returned by /<sub_path>/{token}/info.
type Info struct {
	Username string     `json:"username"`
	Status   string     `json:"status"`
	Used     int64      `json:"used"`
	Limit    int64      `json:"limit"`
	ExpireAt *time.Time `json:"expire_at"`
	Links    []string   `json:"links"`
}

// Serve writes the subscription for token. When info is true it writes the
// JSON summary instead.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, token string, info bool) {
	u, ibs, err := s.Resolve(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	format := r.URL.Query().Get("format")
	if !info && format == FormatLive {
		serveLive(w, u)
		return
	}
	st := db.LoadSettings(s.DB)
	ps := BuildProxiesX(u, ibs, st, r.Host, s.Extras(u, ibs))
	if !info {
		// A browser gets the subscription page; clients (and ?format=raw)
		// get the subscription exactly as before.
		switch {
		case format == FormatStatus:
			s.serveStatus(w, r, u, ps, st)
			return
		case format == FormatHTML || format == "" && IsBrowser(r):
			if s.servePage(w, r, u, ps, st) {
				return
			}
			format = ""
		case format == FormatRaw:
			format = ""
		}
	}
	if !info {
		// Device (HWID) limit: a refused device gets a valid but empty
		// subscription, so its client drops the configs.
		dec := users.CheckDevice(s.DB, u, DeviceFromRequest(r), time.Now().UTC())
		if !dec.Allowed {
			ps = nil
			w.Header().Set("X-Veilon-Refused", dec.Reason)
		}
	}
	if info {
		links := []string{}
		for i := range ps {
			if l := URI(&ps[i]); l != "" {
				links = append(links, l)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(Info{Username: u.Username, Status: u.Status, Used: u.Used(), Limit: u.DataLimit, ExpireAt: u.ExpireAt, Links: links})
		return
	}
	if format == "" {
		format = DetectFormat(r.UserAgent())
	}
	var (
		body  []byte
		ctype string
		ext   string
	)
	switch format {
	case "singbox", "sing-box":
		body, err = Singbox(ps)
		ctype, ext = "application/json; charset=utf-8", ".json"
	case "clash", "clash-meta", "mihomo":
		body, err = Clash(ps)
		ctype, ext = "text/yaml; charset=utf-8", ".yaml"
	case "json", "xray":
		body, err = XrayJSON(ps)
		ctype, ext = "application/json; charset=utf-8", ".json"
	default:
		body = []byte(V2Ray(ps))
		ctype, ext = "text/plain; charset=utf-8", ".txt"
	}
	if err != nil {
		http.Error(w, "subscription render failed", http.StatusInternalServerError)
		return
	}
	SetHeaders(w, u, st)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s"`, safeFilename(u.Username), ext))
	_, _ = w.Write(body)
}

// DeviceFromRequest reads the device headers sent by subscription clients.
func DeviceFromRequest(r *http.Request) users.Device {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	h := r.Header
	return users.Device{
		HWID: h.Get("X-Hwid"), OS: h.Get("X-Device-Os"), OSVersion: h.Get("X-Ver-Os"),
		Model: h.Get("X-Device-Model"), UserAgent: r.UserAgent(), IP: ip,
	}
}

// ServeConf writes one WireGuard / AmneziaWG config file of the user as a
// download (application/octet-stream, NAME.conf), so mobile browsers keep
// the .conf extension.
func (s *Service) ServeConf(w http.ResponseWriter, r *http.Request, token string, inboundID int64) {
	u, ibs, err := s.Resolve(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dec := users.CheckDevice(s.DB, u, DeviceFromRequest(r), time.Now().UTC()); !dec.Allowed {
		http.Error(w, "device refused: "+dec.Reason, http.StatusForbidden)
		return
	}
	st := db.LoadSettings(s.DB)
	ps := BuildProxiesX(u, ibs, st, r.Host, s.Extras(u, ibs))
	for i := range ps {
		if ps[i].InboundID != inboundID {
			continue
		}
		name, body, ok := ConfFile(&ps[i])
		if !ok {
			break
		}
		WriteConf(w, name, body)
		return
	}
	http.NotFound(w, r)
}

// WriteConf sends a config file as an attachment named *.conf.
func WriteConf(w http.ResponseWriter, name, body string) {
	name = strings.TrimSuffix(safeFilename(name), ".txt")
	if !strings.HasSuffix(name, ".conf") {
		name += ".conf"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(body))
}

// SetHeaders writes the standard subscription metadata headers.
func SetHeaders(w http.ResponseWriter, u *db.User, st db.Settings) {
	var exp int64
	if u.ExpireAt != nil {
		exp = u.ExpireAt.Unix()
	}
	w.Header().Set("subscription-userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", u.Up, u.Down, u.DataLimit, exp))
	title := st.SubTitle
	if title == "" {
		title = "Veilon"
	}
	w.Header().Set("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte(title)))
	w.Header().Set("profile-update-interval", strconv.Itoa(st.SubUpdateHours))
	w.Header().Set("Cache-Control", "no-store")
}

func safeFilename(s string) string {
	b := strings.Builder{}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
