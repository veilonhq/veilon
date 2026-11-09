package cores

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/veilonhq/veilon/panel/internal/logbuf"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// Engine is a supervised proxy engine.
type Engine interface {
	Name() string
	Status() xray.Status
	Restart()
	RequestReload()
	Logs(n int) []string
	GenerateConfig() ([]byte, error)
	Run(ctx context.Context)
	TakeSystemDelta() (up, down int64)
}

// Set fans operations out to all engines. The first engine is the primary
// one (Xray-core).
type Set struct {
	Engines []Engine
}

// Run runs every engine until ctx is cancelled.
func (s *Set) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, e := range s.Engines {
		wg.Add(1)
		go func(e Engine) { defer wg.Done(); util.Supervise(ctx, "engine "+e.Name(), e.Run) }(e)
	}
	wg.Wait()
}

// RequestReload asks every engine to re-render its config.
func (s *Set) RequestReload() {
	for _, e := range s.Engines {
		e.RequestReload()
	}
}

// Restart restarts every engine.
func (s *Set) Restart() {
	for _, e := range s.Engines {
		e.Restart()
	}
}

// Status returns the primary engine status.
func (s *Set) Status() xray.Status { return s.Engines[0].Status() }

// Statuses returns the status of every engine.
func (s *Set) Statuses() []xray.Status {
	out := make([]xray.Status, 0, len(s.Engines))
	for _, e := range s.Engines {
		out = append(out, e.Status())
	}
	return out
}

func (s *Set) find(name string) (Engine, error) {
	if name == "" {
		return s.Engines[0], nil
	}
	for _, e := range s.Engines {
		if e.Name() == name {
			return e, nil
		}
	}
	return nil, fmt.Errorf("unknown core %q", name)
}

// Logs returns recent log lines of one engine ("" = primary).
func (s *Set) Logs(name string, n int) ([]string, error) {
	e, err := s.find(name)
	if err != nil {
		return nil, err
	}
	return e.Logs(n), nil
}

// LogEntries returns recent log lines of one engine ("" = primary) with
// their times and levels.
func (s *Set) LogEntries(name string, n int) ([]logbuf.Entry, error) {
	e, err := s.find(name)
	if err != nil {
		return nil, err
	}
	if le, ok := e.(interface{ LogEntries(int) []logbuf.Entry }); ok {
		return le.LogEntries(n), nil
	}
	return logbuf.FromLines(e.Name(), e.Logs(n), time.Local), nil
}

// Names lists the engines.
func (s *Set) Names() []string {
	out := make([]string, len(s.Engines))
	for i, e := range s.Engines {
		out[i] = e.Name()
	}
	return out
}

// GenerateConfig renders one engine's config ("" = primary).
func (s *Set) GenerateConfig(name string) ([]byte, error) {
	e, err := s.find(name)
	if err != nil {
		return nil, err
	}
	return e.GenerateConfig()
}

// TakeSystemDelta sums system traffic of every engine.
func (s *Set) TakeSystemDelta() (up, down int64) {
	for _, e := range s.Engines {
		u, d := e.TakeSystemDelta()
		up += u
		down += d
	}
	return
}

// XrayController returns the Xray-core engine's maintenance controls (nil
// if the primary engine is not Xray-core).
func (s *Set) XrayController() xray.Controller {
	if len(s.Engines) == 0 {
		return nil
	}
	if m, ok := s.Engines[0].(*xray.Manager); ok {
		return m
	}
	return nil
}

// Power scopes for SetPower.
const (
	ScopeXray = "xray" // only the primary engine (Xray-core)
	ScopeAll  = "all"  // every engine
)

// ErrBadScope is returned for an unknown power scope.
var ErrBadScope = fmt.Errorf("scope must be %q or %q", ScopeXray, ScopeAll)

// powered is an engine that can be switched on and off.
type powered interface {
	SetPower(ctx context.Context, on bool) error
}

// SetPower switches engines on or off and returns the names it changed.
// ScopeXray touches only the primary engine; ScopeAll touches every engine,
// skipping the ones already in the wanted state (so starting "all" brings
// back exactly what was stopped). Each choice is stored by the engine.
func (s *Set) SetPower(ctx context.Context, scope string, on bool) ([]string, error) {
	var targets []Engine
	switch scope {
	case ScopeXray:
		if len(s.Engines) > 0 {
			targets = s.Engines[:1]
		}
	case ScopeAll:
		targets = s.Engines
	default:
		return nil, ErrBadScope
	}
	changed := []string{}
	var errs []string
	for _, e := range targets {
		p, ok := e.(powered)
		if !ok || e.Status().Stopped == !on {
			continue
		}
		if err := p.SetPower(ctx, on); err != nil {
			errs = append(errs, e.Name()+": "+err.Error())
			continue
		}
		changed = append(changed, e.Name())
	}
	if len(errs) > 0 {
		return changed, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return changed, nil
}

// StoppedNames lists the engines an admin switched off.
func (s *Set) StoppedNames() []string {
	out := []string{}
	for _, e := range s.Engines {
		if e.Status().Stopped {
			out = append(out, e.Name())
		}
	}
	return out
}
