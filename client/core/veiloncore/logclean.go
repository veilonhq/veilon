package veiloncore

import (
	"regexp"
	"strings"

	"github.com/sagernet/sing-box/log"
)

// ansiRe matches ANSI SGR colour sequences ("\x1b[31m", "\x1b[38;5;84m").
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	return ansiRe.ReplaceAllString(s, "")
}

// benignCloseErrors are connection-teardown errors sing-box logs at ERROR
// level ("connection: connection download closed: ... endpoint not
// connected") although they only mean that one side of a proxied
// connection went away (app closed it, server reset it, tunnel restarted).
var benignCloseErrors = []string{"endpoint not connected", "use of closed", "connection reset", "broken pipe"}

// cleanCoreLog strips colours and demotes teardown noise to debug.
func cleanCoreLog(level log.Level, msg string) (log.Level, string) {
	msg = stripANSI(msg)
	if level <= log.LevelError && strings.Contains(msg, "connection") {
		for _, e := range benignCloseErrors {
			if strings.Contains(msg, e) && (strings.Contains(msg, "closed") || strings.Contains(msg, "close ")) {
				return log.LevelDebug, msg
			}
		}
	}
	return level, msg
}
