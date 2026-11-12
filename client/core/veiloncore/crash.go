package veiloncore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Crash robustness.
//
//   - recoverGo is deferred at the top of every goroutine the core starts
//     itself (SOCKS sidecar, DNS tunnels, SSH transport, scanner, tun
//     diagnostics). A panic there becomes an error log line, is persisted
//     to the crash file, and ends the running session (the app's health
//     check sees IsRunning()==false and handles it like any other core
//     failure) instead of killing the app.
//   - Panics in goroutines of the bundled libraries (sing-box, Xray, gVisor,
//     amneziawg-go) cannot be recovered from outside; debug.SetCrashOutput
//     makes the Go runtime also write their fatal report to
//     <dataDir>/go-crash.txt. The next InitCrashLog / connect moves it to
//     the crash file and puts it on the log page.
//   - The app writes uncaught Kotlin exceptions to the same crash file
//     (VeilonApp / Store.recordCrash) — same format, same file.
//
// Crash file: <dataDir>/last-crash.txt (newest report last, capped).

const (
	crashFileName   = "last-crash.txt"
	goCrashFileName = "go-crash.txt"
	maxCrashFile    = 64 << 10
)

var (
	crashMu      sync.Mutex
	crashDir     string
	crashOutFile *os.File
)

// InitCrashLog sets the directory for crash reports (the app's core data
// dir, the same as Settings.dataDir), routes fatal Go runtime reports there
// and moves the report of a previous fatal crash (if any) into the crash
// file and onto the log page. Safe to call more than once.
func InitCrashLog(dataDir string) {
	defer func() { _ = recover() }()
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return
	}
	crashMu.Lock()
	defer crashMu.Unlock()
	if crashDir == dataDir && crashOutFile != nil {
		return
	}
	_ = os.MkdirAll(dataDir, 0o700)
	crashDir = dataDir
	goCrash := filepath.Join(dataDir, goCrashFileName)
	// A previous process died with a fatal Go error: keep its report.
	if b, err := os.ReadFile(goCrash); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		appendCrashLocked("go (previous run, fatal)", string(b))
		pushLogLevel("error", "previous run crashed (Go):\n"+firstLines(string(b), 40))
	}
	f, err := os.OpenFile(goCrash, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		f.Close()
		return
	}
	if crashOutFile != nil {
		crashOutFile.Close()
	}
	crashOutFile = f // SetCrashOutput dup'ed it; keep ours open anyway
}

// LastCrash returns the crash file (Go panics recovered by the core, fatal
// Go reports of previous runs, uncaught Kotlin exceptions), or "".
func LastCrash() (out string) {
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()
	crashMu.Lock()
	defer crashMu.Unlock()
	if crashDir == "" {
		return ""
	}
	b, _ := os.ReadFile(filepath.Join(crashDir, crashFileName))
	return string(b)
}

// ClearLastCrash deletes the crash file.
func ClearLastCrash() {
	defer func() { _ = recover() }()
	crashMu.Lock()
	defer crashMu.Unlock()
	if crashDir != "" {
		_ = os.Remove(filepath.Join(crashDir, crashFileName))
	}
}

// RecordCrash appends a report (e.g. an uncaught Kotlin exception) to the
// crash file and the log page.
func RecordCrash(source, report string) {
	defer func() { _ = recover() }()
	pushLogLevel("error", "crash ("+source+"):\n"+firstLines(report, 40))
	crashMu.Lock()
	defer crashMu.Unlock()
	appendCrashLocked(source, report)
}

func appendCrashLocked(source, report string) {
	if crashDir == "" {
		return
	}
	p := filepath.Join(crashDir, crashFileName)
	old, _ := os.ReadFile(p)
	entry := fmt.Sprintf("==== %s  %s  core %s\n%s\n", time.Now().Format(time.RFC3339), source, Version, strings.TrimRight(report, "\n"))
	all := append(old, entry...)
	if len(all) > maxCrashFile {
		all = all[len(all)-maxCrashFile:]
	}
	_ = os.WriteFile(p, all, 0o600)
}

func firstLines(s string, n int) string {
	l := strings.SplitN(s, "\n", n+1)
	if len(l) > n {
		l = l[:n]
	}
	return strings.Join(l, "\n")
}

// onRecoveredPanic is called after a recovered panic is logged; the
// default ends the running session (tests replace it).
var onRecoveredPanic = func(where string, r any) {
	go func() {
		defer func() { _ = recover() }()
		_ = stopGlobal()
	}()
}

// recoverGo must be deferred directly at the top of a goroutine the core
// owns: defer recoverGo("what").
func recoverGo(where string) {
	if r := recover(); r != nil {
		handlePanic(where, r, true)
	}
}

// recoverGoKeep is recoverGo for work that is not part of the VPN session
// (DNS scanner, latency probes): the panic is logged and saved, the
// session keeps running.
func recoverGoKeep(where string) {
	if r := recover(); r != nil {
		handlePanic(where, r, false)
	}
}

func handlePanic(where string, r any, stopSession bool) {
	recordPanic(where, r, stopSession)
	if stopSession {
		onRecoveredPanic(where, r)
	}
}

// recordPanic logs a recovered panic and saves it to the crash file
// (no session side effects; safe to call from the close path).
func recordPanic(where string, r any, stopSession bool) {
	report := fmt.Sprintf("panic in %s: %v\n%s", where, r, debug.Stack())
	what := "details saved for the crash report"
	if stopSession {
		what = "session stopped; " + what
	}
	pushLogLevel("error", fmt.Sprintf("core: recovered panic in %s: %v (%s)", where, r, what))
	crashMu.Lock()
	appendCrashLocked("go (recovered)", report)
	crashMu.Unlock()
}
