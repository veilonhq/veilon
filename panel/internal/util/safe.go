package util

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

// Safe runs fn and logs a panic instead of letting it crash the service.
// It reports whether fn returned normally.
func Safe(what string, fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("recovered from panic", "in", what, "panic", r, "stack", string(debug.Stack()))
			ok = false
		}
	}()
	fn()
	return true
}

// Supervise runs a long-lived loop until it returns normally or ctx ends.
// A panic in the loop is logged and the loop is started again after a
// short, growing pause, so one bad value can never stop a background
// service for good.
func Supervise(ctx context.Context, what string, run func(context.Context)) {
	pause := time.Second
	for {
		if Safe(what, func() { run(ctx) }) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		if pause < 30*time.Second {
			pause *= 2
		}
		slog.Warn("restarting after panic", "in", what)
	}
}
