package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
)

var pathRe = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	writeJSON(w, 200, db.LoadSettings(s.DB))
}

// ValidateSettings checks a settings object.
func ValidateSettings(st *db.Settings) error {
	st.PanelPath = strings.Trim(st.PanelPath, "/")
	st.SubPath = strings.Trim(st.SubPath, "/")
	if st.PanelPort < 1 || st.PanelPort > 65535 {
		return fmt.Errorf("panel_port must be 1-65535")
	}
	if !pathRe.MatchString(st.PanelPath) {
		return fmt.Errorf("panel_path must be 2-64 chars of letters, digits, _ or -")
	}
	if !pathRe.MatchString(st.SubPath) {
		return fmt.Errorf("sub_path must be 2-64 chars of letters, digits, _ or -")
	}
	if st.SubPath == st.PanelPath {
		return fmt.Errorf("sub_path must differ from panel_path")
	}
	if st.SubUpdateHours < 1 || st.SubUpdateHours > 168 {
		return fmt.Errorf("sub_update_hours must be 1-168")
	}
	if st.Language != "fa" && st.Language != "en" {
		return fmt.Errorf("language must be fa or en")
	}
	if st.Timezone == "" {
		st.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(st.Timezone); err != nil {
		return fmt.Errorf("unknown timezone %q", st.Timezone)
	}
	if st.PanelCertID != nil && *st.PanelCertID == 0 {
		st.PanelCertID = nil
	}
	for _, h := range []*string{&st.PanelDomain, &st.SubDomain, &st.ServerAddress} {
		*h = strings.TrimSpace(*h)
		if strings.ContainsAny(*h, " /") {
			return fmt.Errorf("domains/addresses must not contain spaces or slashes")
		}
	}
	return nil
}

func (s *Server) patchSettings(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	cur := db.LoadSettings(s.DB)
	old := cur
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "cannot read body")
		return
	}
	if err := json.Unmarshal(body, &cur); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := ValidateSettings(&cur); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if cur.PanelPort != old.PanelPort {
		var n int64
		s.DB.Model(&db.Inbound{}).Where("port = ?", cur.PanelPort).Count(&n)
		if n > 0 || db.GetSetting(s.DB, db.KeyXrayAPIPort) == fmt.Sprint(cur.PanelPort) || db.GetSetting(s.DB, db.KeySingboxAPIPort) == fmt.Sprint(cur.PanelPort) {
			writeErr(w, http.StatusBadRequest, "validation_error", "panel_port is used by an inbound or the core API")
			return
		}
	}
	if cur.PanelCertID != nil {
		if _, err := s.Certs.Certificate(*cur.PanelCertID); err != nil {
			writeErr(w, http.StatusBadRequest, "validation_error", "panel_cert_id does not exist or is invalid")
			return
		}
	}
	if err := db.SaveSettings(s.DB, cur); err != nil {
		writeServiceErr(w, err)
		return
	}
	s.ReloadSettings()
	audit.Log(s.DB, a, "settings.update", string(body), s.clientIP(r))
	writeJSON(w, 200, cur)
	listenerChanged := cur.PanelPort != old.PanelPort || !sameID(cur.PanelCertID, old.PanelCertID)
	if listenerChanged && s.OnListenerChange != nil {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go util.Safe("listener restart", func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			<-ctx.Done()
			s.OnListenerChange()
		})
	}
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
