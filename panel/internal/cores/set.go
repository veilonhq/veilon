package cores

import (
	"context"
	"fmt"
	"sync"

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
