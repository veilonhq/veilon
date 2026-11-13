// Package userkey maps the user names Xray reports (statistics, online
// list, access log) back to panel user ids.
//
// Xray identifies a user by its "email", which the panel sets to
// "<id>.<username>". HTTP, Mixed and SOCKS inbounds have no email field:
// Xray reports the authenticated proxy user name instead. A user with a
// custom proxy username is therefore reported under that name, and this
// package keeps the table that maps such names to user ids. The main server
// fills it from the database whenever it renders the Xray config; a node
// agent fills it from the "veilon_aliases" key of the config the master
// pushes.
package userkey

import (
	"strconv"
	"strings"
	"sync/atomic"
)

var aliases atomic.Pointer[map[string]int64]

// Set replaces the custom proxy username table (name -> user id).
func Set(m map[string]int64) {
	c := make(map[string]int64, len(m))
	for k, v := range m {
		if k != "" && v > 0 {
			c[k] = v
		}
	}
	aliases.Store(&c)
}

// Snapshot returns a copy of the current table.
func Snapshot() map[string]int64 {
	out := map[string]int64{}
	if p := aliases.Load(); p != nil {
		for k, v := range *p {
			out[k] = v
		}
	}
	return out
}

// ID returns the user id behind an Xray user name: "<id>.<username>", or a
// custom proxy username from the table.
func ID(name string) (int64, bool) {
	if idStr, _, ok := strings.Cut(name, "."); ok {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil && id > 0 {
			return id, true
		}
	}
	if p := aliases.Load(); p != nil {
		if id, ok := (*p)[name]; ok {
			return id, true
		}
	}
	return 0, false
}

// LooksAuto reports whether a name has the automatic "<digits>.<...>" form,
// which a custom proxy username must not take (it would be read as that
// user id).
func LooksAuto(name string) bool {
	idStr, _, ok := strings.Cut(name, ".")
	if !ok || idStr == "" {
		return false
	}
	for _, c := range idStr {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
