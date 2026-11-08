package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/audit"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
)

func (s *Server) listCerts(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	list, err := s.Certs.List()
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// acmeCert issues a Let's Encrypt (ACME) certificate for a domain or for
// the server's IP address(es). IP certificates use the "shortlived" profile.
func (s *Server) acmeCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		certs.ACMERequest
		UseForPanel bool `json:"use_for_panel"`
	}
	if !decode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	v, err := s.Certs.IssueACME(ctx, req.ACMERequest)
	if err != nil {
		if _, ok := err.(*certs.ValidationError); ok {
			writeServiceErr(w, err)
			return
		}
		writeErr(w, http.StatusBadGateway, "acme_failed", err.Error())
		return
	}
	audit.Log(s.DB, a, "cert.acme", fmt.Sprintf("cert:%d %s %s", v.ID, v.Type, strings.Join(v.Identifiers, ",")), s.clientIP(r))
	if req.UseForPanel {
		if err := s.UsePanelCert(v); err != nil {
			writeServiceErr(w, err)
			return
		}
		v.InUse = true
	}
	writeJSON(w, 201, v)
}

// UsePanelCert makes certificate v the panel's TLS certificate and restarts
// the panel listener (HTTPS from then on). For a domain certificate the
// panel domain is set to it; for an IP certificate the public server
// address is set to its first IP when none is configured.
func (s *Server) UsePanelCert(v *certs.View) error {
	if _, err := s.Certs.Certificate(v.ID); err != nil {
		return err
	}
	if err := db.SetSetting(s.DB, db.KeyPanelCertID, strconv.FormatInt(v.ID, 10)); err != nil {
		return err
	}
	switch v.Type {
	case "domain":
		if err := db.SetSetting(s.DB, db.KeyPanelDomain, v.Domain); err != nil {
			return err
		}
	case "ip":
		if db.GetSetting(s.DB, db.KeyServerAddress) == "" {
			if err := db.SetSetting(s.DB, db.KeyServerAddress, v.Domain); err != nil {
				return err
			}
		}
	}
	s.restartListenerSoon()
	return nil
}

// ClearPanelCert makes the panel serve plain HTTP again.
func (s *Server) ClearPanelCert() error {
	if err := db.SetSetting(s.DB, db.KeyPanelCertID, ""); err != nil {
		return err
	}
	s.restartListenerSoon()
	return nil
}

// restartListenerSoon reloads settings and restarts the panel listener after
// a short delay, so the current response can still be delivered.
func (s *Server) restartListenerSoon() {
	s.ReloadSettings()
	if s.OnListenerChange != nil {
		go util.Safe("listener restart", func() {
			time.Sleep(time.Second)
			s.OnListenerChange()
		})
	}
}

// RestartListener reloads settings (port, path, certificate) and restarts
// the panel listener.
func (s *Server) RestartListener() { s.restartListenerSoon() }

func (s *Server) publicIP(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, 200, certs.DetectPublicIP(ctx))
}

func (s *Server) getCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := s.Certs.Get(id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) renewCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	v, err := s.Certs.Renew(ctx, id)
	if err != nil {
		if _, ok := err.(*certs.ValidationError); ok || errors.Is(err, certs.ErrNotFound) {
			writeServiceErr(w, err)
			return
		}
		writeErr(w, http.StatusBadGateway, "acme_failed", err.Error())
		return
	}
	audit.Log(s.DB, a, "cert.renew", fmt.Sprintf("cert:%d", id), s.clientIP(r))
	writeJSON(w, 200, v)
}

func (s *Server) patchCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		AutoRenew *bool `json:"auto_renew"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.AutoRenew == nil {
		writeErr(w, http.StatusBadRequest, "validation_error", "nothing to change (auto_renew)")
		return
	}
	v, err := s.Certs.SetAutoRenew(id, *req.AutoRenew)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "cert.update", fmt.Sprintf("cert:%d auto_renew=%v", id, *req.AutoRenew), s.clientIP(r))
	writeJSON(w, 200, v)
}

func (s *Server) revokeCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.Certs.Revoke(ctx, id); err != nil {
		var ve *certs.ValidationError
		if errors.As(err, &ve) || errors.Is(err, certs.ErrNotFound) || errors.Is(err, certs.ErrInUse) {
			writeServiceErr(w, err)
			return
		}
		writeErr(w, http.StatusBadGateway, "acme_failed", err.Error())
		return
	}
	audit.Log(s.DB, a, "cert.revoke", fmt.Sprintf("cert:%d", id), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) uploadCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Domain  string `json:"domain"`
		CertPEM string `json:"cert_pem"`
		KeyPEM  string `json:"key_pem"`
	}
	if !decode(w, r, &req) {
		return
	}
	v, err := s.Certs.Upload(req.Domain, req.CertPEM, req.KeyPEM)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "cert.upload", fmt.Sprintf("cert:%d %s", v.ID, v.Domain), s.clientIP(r))
	writeJSON(w, 201, v)
}

func (s *Server) deleteCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Certs.Delete(id); err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "cert.delete", fmt.Sprintf("cert:%d", id), s.clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) selfSignedCert(w http.ResponseWriter, r *http.Request, a *db.Admin) {
	var req struct {
		Domain string `json:"domain"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, err := s.Certs.SelfSigned(req.Domain)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	var c db.Cert
	if err := s.DB.First(&c, id).Error; err != nil {
		writeServiceErr(w, err)
		return
	}
	audit.Log(s.DB, a, "cert.self_signed", fmt.Sprintf("cert:%d %s", id, c.Domain), s.clientIP(r))
	writeJSON(w, 201, certs.ToView(&c))
}
