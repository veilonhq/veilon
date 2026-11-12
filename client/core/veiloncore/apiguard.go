package veiloncore

import (
	"errors"
	"fmt"
)

// 1.0.6: no panic may cross the gomobile boundary. gomobile does not
// recover panics in exported functions: a panic on the JNI call path is
// fatal for the whole Android process (the app "suddenly closes"). Every
// function the app calls defers apiGuard, which logs + saves the panic
// (crash file, log page) and lets the call return an error value instead.
//
// Panics inside goroutines started by the bundled libraries (sing-box,
// Xray, gVisor, amneziawg-go) are not on this path and still cannot be
// recovered from outside; their report goes to go-crash.txt (crash.go).

// apiGuard must be deferred directly: defer apiGuard("Name", func(msg string) { ... }).
// onPanic (may be nil) sets the function's named results.
func apiGuard(name string, onPanic func(msg string)) {
	if r := recover(); r != nil {
		recordPanic("api "+name, r, false)
		if onPanic != nil {
			onPanic(fmt.Sprint(r))
		}
	}
}

// panicErr is the error an exported function returns after a recovered panic.
func panicErr(name, msg string) error {
	return errors.New("internal error in " + name + ": " + msg)
}

// panicJSON is the {"error": ...} JSON returned after a recovered panic.
func panicJSON(name, msg string) string {
	return encodeJSON(map[string]string{"error": panicErr(name, msg).Error()})
}

// safeClose closes one component; a panic there is recovered and reported
// as an error, so one failing component cannot skip the others.
func safeClose(what string, f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			recordPanic("close "+what, r, false)
			err = fmt.Errorf("close %s: panic: %v", what, r)
		}
	}()
	return f()
}
