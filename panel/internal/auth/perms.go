package auth

import (
	"encoding/json"
	"sort"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Permissions of sub-admins. Sudo admins have every permission and are the
// only ones who manage admins.
const (
	PermUsersView   = "users_view"
	PermUsersCreate = "users_create"
	PermUsersEdit   = "users_edit"
	PermUsersDelete = "users_delete"
	// PermAllUsers lets a sub-admin see and manage every user, not only
	// the users it created.
	PermAllUsers = "all_users"
	PermInbounds = "inbounds"
	PermSettings = "settings"
	PermCerts    = "certs"
	PermLogs     = "logs"
	PermGroups   = "groups"
)

// AllPerms lists every permission flag.
var AllPerms = []string{PermUsersView, PermUsersCreate, PermUsersEdit, PermUsersDelete, PermAllUsers,
	PermInbounds, PermSettings, PermCerts, PermLogs, PermGroups}

// DefaultSubPerms is what a new sub-admin gets when none are given: manage
// its own users.
var DefaultSubPerms = []string{PermUsersView, PermUsersCreate, PermUsersEdit, PermUsersDelete}

// Perms returns the effective permission set of an admin. Sub-admins
// created before permissions existed (empty column) keep full access.
func Perms(a *db.Admin) map[string]bool {
	out := map[string]bool{}
	if a.IsSudo || a.Permissions == "" {
		for _, p := range AllPerms {
			out[p] = true
		}
		return out
	}
	var m map[string]bool
	_ = json.Unmarshal([]byte(a.Permissions), &m)
	for _, p := range AllPerms {
		if m[p] {
			out[p] = true
		}
	}
	return out
}

// Has reports whether the admin holds a permission.
func Has(a *db.Admin, p string) bool { return Perms(a)[p] }

// PermList returns the granted permissions, sorted.
func PermList(a *db.Admin) []string {
	out := []string{}
	for p, ok := range Perms(a) {
		if ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// EncodePerms stores a permission list (unknown names are dropped). The
// result is never empty, so a sub-admin with no flags has no access.
func EncodePerms(list []string) string {
	m := map[string]bool{}
	valid := map[string]bool{}
	for _, p := range AllPerms {
		valid[p] = true
	}
	for _, p := range list {
		if valid[p] {
			m[p] = true
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// OwnerScope returns the admin id whose users this admin is limited to, or
// nil when it may see every user.
func OwnerScope(a *db.Admin) *int64 {
	if a.IsSudo || Has(a, PermAllUsers) {
		return nil
	}
	id := a.ID
	return &id
}
