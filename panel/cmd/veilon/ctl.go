package main

// Local control socket. The running service ("veilon serve") listens on a
// unix socket in its data directory (mode 0600, owned by the service user;
// root can always connect). The CLI and the management menu use it for
// work that must happen inside the service: ACME issuance (the service has
// CAP_NET_BIND_SERVICE for the port 80 listener and hot-reloads the panel
// and inbounds), panel listener restarts and geo file updates.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/certs"
)

const ctlSocketName = "veilon.sock"

func ctlSocketPath(dataDir string) string { return filepath.Join(dataDir, ctlSocketName) }

type ctlServer struct {
	certs     *certs.Service
	api       *api.Server
	updateGeo func(ctx context.Context) error
}

func ctlWrite(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func ctlErr(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	var ve *certs.ValidationError
	switch {
	case errors.As(err, &ve):
		code = http.StatusBadRequest
	case errors.Is(err, certs.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, certs.ErrInUse):
		code = http.StatusConflict
	}
	ctlWrite(w, code, map[string]string{"error": err.Error()})
}

func (c *ctlServer) handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /certs", func(w http.ResponseWriter, r *http.Request) {
		l, err := c.certs.List()
		if err != nil {
			ctlErr(w, err)
			return
		}
		ctlWrite(w, 200, l)
	})
	m.HandleFunc("GET /public-ip", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		ctlWrite(w, 200, certs.DetectPublicIP(ctx))
	})
	m.HandleFunc("POST /certs/acme", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			certs.ACMERequest
			UseForPanel bool `json:"use_for_panel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ctlErr(w, &certs.ValidationError{Msg: err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
		defer cancel()
		v, err := c.certs.IssueACME(ctx, req.ACMERequest)
		if err != nil {
			ctlErr(w, err)
			return
		}
		if req.UseForPanel {
			if err := c.api.UsePanelCert(v); err != nil {
				ctlErr(w, err)
				return
			}
			v.InUse = true
		}
		ctlWrite(w, 201, v)
	})
	m.HandleFunc("POST /certs/{id}/renew", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
		defer cancel()
		v, err := c.certs.Renew(ctx, id)
		if err != nil {
			ctlErr(w, err)
			return
		}
		ctlWrite(w, 200, v)
	})
	m.HandleFunc("POST /certs/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := c.certs.Revoke(ctx, id); err != nil {
			ctlErr(w, err)
			return
		}
		ctlWrite(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("DELETE /certs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := c.certs.Delete(id); err != nil {
			ctlErr(w, err)
			return
		}
		ctlWrite(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /panel/cert", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID int64 `json:"id"` // 0 = plain HTTP
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ctlErr(w, &certs.ValidationError{Msg: err.Error()})
			return
		}
		var err error
		if req.ID == 0 {
			err = c.api.ClearPanelCert()
		} else {
			var v *certs.View
			if v, err = c.certs.Get(req.ID); err == nil {
				err = c.api.UsePanelCert(v)
			}
		}
		if err != nil {
			ctlErr(w, err)
			return
		}
		ctlWrite(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("GET /panel", func(w http.ResponseWriter, r *http.Request) {
		st := c.api.Settings()
		ctlWrite(w, 200, map[string]any{"cert_id": st.PanelCertID, "port": st.PanelPort, "path": st.PanelPath, "domain": st.PanelDomain})
	})
	m.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		c.api.RestartListener()
		ctlWrite(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /geo/update", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		if err := c.updateGeo(ctx); err != nil {
			ctlErr(w, err)
			return
		}
		ctlWrite(w, 200, map[string]bool{"ok": true})
	})
	return m
}

// serveCtl listens on the control socket until ctx is done.
func serveCtl(ctx context.Context, path string, h http.Handler) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		slog.Warn("control socket unavailable", "path", path, "err", err)
		return
	}
	_ = os.Chmod(path, 0o600)
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
		_ = os.Remove(path)
	}()
	_ = srv.Serve(ln)
}

// ctlClient talks to the service's control socket.
type ctlClient struct{ path string }

var errServiceDown = errors.New("the Veilon service is not running (start it: systemctl start veilon)")

func (c ctlClient) do(method, path string, body, out any, timeout time.Duration) error {
	if _, err := os.Stat(c.path); err != nil {
		return errServiceDown
	}
	hc := &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", c.path)
		},
	}}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://veilon"+path, rd)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			return errServiceDown
		}
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("service returned HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
