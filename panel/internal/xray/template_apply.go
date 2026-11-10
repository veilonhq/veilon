package xray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// ErrNoBinary means the template could not be tested because Xray-core is
// not installed yet.
var ErrNoBinary = errors.New("xray binary is not installed")

// TemplateResult is the outcome of testing or applying a template.
type TemplateResult struct {
	OK bool `json:"ok"`
	// Tested is false when no Xray binary was available to test with.
	Tested bool `json:"tested"`
	// Applied is true when the template was saved and the core reloaded.
	Applied bool `json:"applied"`
	// RolledBack is true when Xray did not stay up and the previous
	// template was restored.
	RolledBack bool `json:"rolled_back"`
	// Error is Xray's own complaint (last lines of "xray run -test").
	Error string `json:"error,omitempty"`
	// Warnings lists things left out of the config (inbounds, rules).
	Warnings []string `json:"warnings"`
}

// LoadTemplate returns the saved template (or the default one).
func (m *Manager) LoadTemplate() *Template {
	if m.opt.DB != nil {
		if raw := db.GetSetting(m.opt.DB, KeyTemplate); raw != "" {
			if t, err := ParseTemplate([]byte(raw)); err == nil {
				return t
			}
		}
	}
	return DefaultTemplate()
}

// renderWith renders the full config from the database with t in place of
// the saved template.
func (m *Manager) renderWith(t *Template) ([]byte, []string, error) {
	in, err := LoadInput(m.opt.DB, m.opt.DataDir, m.opt.APIPort)
	if err != nil {
		return nil, nil, err
	}
	in.BridgePort = m.opt.BridgePort
	in.Template = t
	b, err := Build(in)
	return b, in.Skipped, err
}

// testRendered runs "xray run -test" on cfg with the installed binary.
func (m *Manager) testRendered(ctx context.Context, cfg []byte) error {
	if _, err := os.Stat(m.opt.BinPath); err != nil {
		return ErrNoBinary
	}
	dir := filepath.Dir(m.opt.BinPath)
	f, err := os.CreateTemp(dir, ".config-test-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(cfg); err != nil {
		f.Close()
		return err
	}
	f.Close()
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, m.opt.BinPath, "run", "-test", "-c", f.Name())
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := lastLines(string(out), 4)
		if i := strings.Index(string(out), "Failed to start:"); i >= 0 {
			msg = strings.TrimSpace(strings.TrimPrefix(string(out[i:]), "Failed to start:"))
			msg = strings.TrimPrefix(msg, "main: failed to load config files: ")
			if j := strings.Index(msg, "] > "); j >= 0 && strings.HasPrefix(msg, "[") {
				msg = msg[j+4:]
			}
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// TestTemplate validates t and checks the resulting config with Xray
// without saving anything.
func (m *Manager) TestTemplate(ctx context.Context, t *Template) TemplateResult {
	res := TemplateResult{Warnings: []string{}}
	cfg, warn, err := m.renderWith(t)
	if warn != nil {
		res.Warnings = warn
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	switch err := m.testRendered(ctx, cfg); {
	case errors.Is(err, ErrNoBinary):
		res.OK = true
	case err != nil:
		res.Tested = true
		res.Error = err.Error()
	default:
		res.Tested, res.OK = true, true
	}
	return res
}

// ApplyTemplate tests t, saves it and reloads Xray. If Xray does not stay
// up with the new config, the previous template is restored and Xray is
// reloaded with it, so a bad template never leaves the core down.
func (m *Manager) ApplyTemplate(ctx context.Context, t *Template) (TemplateResult, error) {
	if !m.beginJob("config", "") {
		return TemplateResult{}, ErrBusy
	}
	var jobErr error
	rolled := false
	defer func() { m.endJob(jobErr, rolled) }()
	res := m.TestTemplate(ctx, t)
	if !res.OK {
		jobErr = errors.New(res.Error)
		return res, nil
	}
	prev := db.GetSetting(m.opt.DB, KeyTemplate)
	if err := db.SetSetting(m.opt.DB, KeyTemplate, string(MarshalTemplate(t))); err != nil {
		jobErr = err
		return res, err
	}
	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	var startedAt time.Time
	if err := m.exec(ctx, func() {
		m.apply(true)
		m.mu.Lock()
		startedAt = m.startedAt
		m.mu.Unlock()
	}); err != nil {
		// No supervisor (tests, or the core loop is not running): the saved
		// template is picked up by the next start.
		if errors.Is(err, ErrNotSupervised) {
			res.Applied = true
			return res, nil
		}
		jobErr = err
		return res, err
	}
	if stopped || !res.Tested {
		res.Applied = true
		return res, nil
	}
	select {
	case <-time.After(verifyWindow):
	case <-ctx.Done():
	}
	m.mu.Lock()
	ok := m.running && m.startedAt.Equal(startedAt)
	lastErr := m.lastErr
	m.mu.Unlock()
	if ok {
		res.Applied = true
		m.logs.add("[veilon] xray config template applied")
		return res, nil
	}
	m.logs.add("[veilon] xray did not stay up with the new template; restoring the previous one")
	_ = db.SetSetting(m.opt.DB, KeyTemplate, prev)
	_ = m.exec(context.Background(), func() { m.apply(true) })
	if lastErr == "" {
		lastErr = "xray did not stay up"
	}
	rolled = true
	res.OK, res.RolledBack, res.Error = false, true, lastErr
	jobErr = fmt.Errorf("rolled back: %s", lastErr)
	return res, nil
}
