package sub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/webui"
)

// Query values of ?format= that belong to the browser page rather than to a
// subscription format.
const (
	// FormatRaw forces the subscription itself (the format then follows the
	// User-Agent, as for any client), even in a browser.
	FormatRaw = "raw"
	// FormatHTML forces the subscription page.
	FormatHTML = "html"
	// FormatStatus returns the page's data as JSON.
	FormatStatus = "status"
)

// clientMarkers are lower-case User-Agent fragments of VPN clients and
// command-line tools. A request carrying one is never sent the page, even
// when it claims to accept text/html.
var clientMarkers = []string{
	"v2rayng", "v2rayn", "v2raytun", "v2box", "hiddify", "streisand", "shadowrocket",
	"sing-box", "singbox", "sfa/", "sfi/", "sfm/", "sft/", "clash", "mihomo", "stash",
	"happ", "nekobox", "nekoray", "karing", "foxray", "quantumult", "surge", "loon",
	"curl", "wget", "okhttp", "go-http-client", "python", "httpie", "postman",
}

// IsBrowser reports whether the request comes from a web browser: it accepts
// text/html, its User-Agent looks like a browser ("Mozilla/…") and names no
// known VPN client or tool.
func IsBrowser(r *http.Request) bool {
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		return false
	}
	ua := strings.ToLower(r.UserAgent())
	if !strings.HasPrefix(ua, "mozilla/") {
		return false
	}
	for _, m := range clientMarkers {
		if strings.Contains(ua, m) {
			return false
		}
	}
	return true
}

// PageLink is one config of the subscription page.
type PageLink struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Network  string `json:"network,omitempty"`
	Security string `json:"security,omitempty"`
	URI      string `json:"uri,omitempty"`
	// ConfURL downloads a WireGuard / AmneziaWG config file.
	ConfURL string `json:"conf_url,omitempty"`
}

// PageData is what the subscription page shows: the user's own usage and
// configs, nothing about the panel or other users.
type PageData struct {
	Username     string     `json:"username"`
	Status       string     `json:"status"`
	Up           int64      `json:"up"`
	Down         int64      `json:"down"`
	Used         int64      `json:"used"`
	Limit        int64      `json:"limit"`
	ExpireAt     *time.Time `json:"expire_at"`
	OnHoldDays   *int       `json:"on_hold_days,omitempty"`
	LastOnlineAt *time.Time `json:"last_online_at"`
	SubURL       string     `json:"sub_url"`
	Title        string     `json:"title"`
	UpdateHours  int        `json:"update_hours"`
	Timezone     string     `json:"timezone"`
	Links        []PageLink `json:"links"`
}

func pageData(u *db.User, ps []Proxy, st db.Settings, host string) PageData {
	title := st.SubTitle
	if title == "" {
		title = "Veilon"
	}
	su := SubURL(st, u.SubToken, host)
	d := PageData{Username: u.Username, Status: u.Status, Up: u.Up, Down: u.Down, Used: u.Used(), Limit: u.DataLimit,
		ExpireAt: u.ExpireAt, OnHoldDays: u.OnHoldDays, LastOnlineAt: u.LastOnlineAt, SubURL: su, Title: title,
		UpdateHours: st.SubUpdateHours, Timezone: st.Timezone, Links: []PageLink{}}
	for i := range ps {
		l := PageLink{Name: ps[i].Name, Protocol: ps[i].Protocol, Network: ps[i].Network, Security: ps[i].Security, URI: URI(&ps[i])}
		if _, _, ok := ConfFile(&ps[i]); ok {
			l.ConfURL = fmt.Sprintf("%s/conf/%d", su, ps[i].InboundID)
		}
		if l.URI != "" || l.ConfURL != "" {
			d.Links = append(d.Links, l)
		}
	}
	return d
}

func pageHeaders(w http.ResponseWriter, ctype string) {
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Vary", "Accept, User-Agent")
}

// servePage writes the browser page, or returns false when the build has
// none (the caller then serves the subscription).
func (s *Service) servePage(w http.ResponseWriter, r *http.Request, u *db.User, ps []Proxy, st db.Settings) bool {
	base := "/" + strings.Trim(st.SubPath, "/") + "/"
	page, ok := webui.SubPage(s.UI, base, st.Language, pageData(u, ps, st, r.Host))
	if !ok {
		return false
	}
	pageHeaders(w, "text/html; charset=utf-8")
	_, _ = w.Write(page)
	return true
}

func (s *Service) serveStatus(w http.ResponseWriter, r *http.Request, u *db.User, ps []Proxy, st db.Settings) {
	pageHeaders(w, "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(pageData(u, ps, st, r.Host))
}

// ServeAsset serves the subscription page's static files (rel is
// "assets/…" or "favicon.svg").
func (s *Service) ServeAsset(w http.ResponseWriter, r *http.Request, rel string) {
	webui.ServeSubAsset(w, r, s.UI, rel)
}
