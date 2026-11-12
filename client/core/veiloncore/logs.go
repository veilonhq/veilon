package veiloncore

import (
	"encoding/json"
	stdlog "log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/log"
	xlog "github.com/xtls/xray-core/common/log"
)

// LogLine is one captured log line.
type LogLine struct {
	Seq   int64  `json:"seq"`
	Time  int64  `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

const maxLogLines = 1500

var (
	logMu   sync.Mutex
	logRing []LogLine
	logSeq  int64
)

func levelName(l log.Level) string {
	switch l {
	case log.LevelPanic, log.LevelFatal, log.LevelError:
		return "error"
	case log.LevelWarn:
		return "warn"
	case log.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

func pushLogLevel(level, msg string) {
	msg = strings.TrimRight(msg, "\r\n")
	if msg == "" {
		return
	}
	logMu.Lock()
	logSeq++
	logRing = append(logRing, LogLine{Seq: logSeq, Time: time.Now().UnixMilli(), Level: level, Msg: msg})
	if len(logRing) > maxLogLines {
		logRing = append([]LogLine(nil), logRing[len(logRing)-maxLogLines:]...)
	}
	logMu.Unlock()
}

func pushLog(l log.Level, msg string) { pushLogLevel(levelName(l), msg) }

func logsAfter(seq int64) string {
	logMu.Lock()
	defer logMu.Unlock()
	out := []LogLine{}
	for _, l := range logRing {
		if l.Seq > seq {
			out = append(out, l)
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func clearLogs() {
	logMu.Lock()
	logRing = nil
	logMu.Unlock()
}

type lineWriter struct{ level string }

func (w lineWriter) Write(b []byte) (int, error) {
	for _, l := range strings.Split(string(b), "\n") {
		pushLogLevel(w.level, l)
	}
	return len(b), nil
}

// xrayLogHandler sends Xray's log to the app's log ring. It stays the
// process-wide Xray handler: every Xray instance's log app replaces the
// global handler on start (with a stdout writer, invisible on Android) and
// silences it on close (e.g. after a latency test), so startXray and
// xrayInstance.Close re-register it.
type xrayLogHandler struct{}

// xrayUserSeverity is the level chosen for the connected session.
var xrayUserSeverity atomic.Int32

func init() { xrayUserSeverity.Store(int32(xlog.Severity_Warning)) }

func setXrayLogLevel(level string) {
	sev := xlog.Severity_Warning
	switch level {
	case "debug", "trace":
		sev = xlog.Severity_Debug
	case "info":
		sev = xlog.Severity_Info
	case "error":
		sev = xlog.Severity_Error
	}
	xrayUserSeverity.Store(int32(sev))
}

// Severity makes Xray produce Info lines too: its per-connection dial
// failures ("failed to process outbound traffic > ...") are Info, and they
// are exactly what a user needs to send when apps get no internet.
func (xrayLogHandler) Severity() xlog.Severity {
	if s := xlog.Severity(xrayUserSeverity.Load()); s > xlog.Severity_Info {
		return s
	}
	return xlog.Severity_Info
}

func (xrayLogHandler) Handle(m xlog.Message) {
	gm, ok := m.(*xlog.GeneralMessage)
	if !ok {
		return // access / DNS logs
	}
	user := xlog.Severity(xrayUserSeverity.Load())
	lvl := "info"
	switch gm.Severity {
	case xlog.Severity_Error:
		lvl = "error"
	case xlog.Severity_Warning:
		lvl = "warn"
	case xlog.Severity_Debug:
		lvl = "debug"
	}
	s := m.String()
	if gm.Severity > user {
		// Below the chosen level: keep only connection failures.
		if gm.Severity != xlog.Severity_Info || !xrayIsFailure(s) {
			return
		}
		lvl = "warn"
	}
	pushLogLevel(lvl, "[xray] "+s)
}

func xrayIsFailure(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "failed") || strings.Contains(l, "rejected") || strings.Contains(l, "timeout")
}

func registerXrayLog() { xlog.RegisterHandler(xrayLogHandler{}) }

func init() {
	// Libraries that log through the standard logger end up in the app log.
	stdlog.SetFlags(0)
	stdlog.SetOutput(lineWriter{level: "info"})
	registerXrayLog()
}
