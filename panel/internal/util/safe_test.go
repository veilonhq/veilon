package util

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafe(t *testing.T) {
	if Safe("x", func() { panic("boom") }) {
		t.Fatal("panic not reported")
	}
	if !Safe("x", func() {}) {
		t.Fatal("normal return reported as panic")
	}
}

func TestSuperviseRestarts(t *testing.T) {
	var n atomic.Int32
	done := make(chan struct{})
	go func() {
		Supervise(context.Background(), "loop", func(context.Context) {
			if n.Add(1) == 1 {
				panic("first run")
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop not restarted")
	}
	if n.Load() != 2 {
		t.Fatalf("runs %d", n.Load())
	}
}

func TestSuperviseStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Supervise(ctx, "loop", func(context.Context) { panic("always") })
}
