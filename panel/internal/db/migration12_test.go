package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Upgrading a schema-11 database (v0.3.10) adds the custom proxy login
// columns with ” for every user, changes no existing value, can run again,
// and keeps custom proxy usernames unique while any number of users keep
// the automatic login (”).
func TestUpgradeFromSchema11AddsProxyLogin(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "u11.db"))
	saved := migrations
	migrations = saved[:11]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	if columnCount(t, gdb, "users", "proxy_username") != 0 {
		t.Fatal("column exists before schema 12")
	}
	now := time.Now().UTC()
	gdb.Exec(`INSERT INTO users (username, status, uuid, password, sub_token, created_at, updated_at, data_limit, up, down)
		VALUES ('hosein','active','u-1','p-1','tok-1',?,?,5000,11,22), ('bob','active','u-2','p-2','tok-2',?,?,0,0,7)`, now, now, now, now)
	snap := func() string {
		var us []map[string]any
		gdb.Raw("SELECT id, username, status, uuid, password, sub_token, data_limit, up, down FROM users ORDER BY id").Scan(&us)
		return fmt.Sprint(us)
	}
	before := snap()
	for i := 0; i < 2; i++ {
		if err := Migrate(gdb, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	if v := SchemaVersion(gdb); v != len(migrations) || v < 12 {
		t.Fatalf("schema %d", v)
	}
	if after := snap(); after != before {
		t.Fatalf("data changed by the upgrade:\n%s\n%s", before, after)
	}
	var us []User
	gdb.Order("id").Find(&us)
	if len(us) != 2 || us[0].ProxyUsername != "" || us[1].ProxyPassword != "" {
		t.Fatalf("new columns not empty: %+v", us)
	}
	// Both users keep '' (the partial index ignores them); a custom name is
	// unique.
	if err := gdb.Model(&User{}).Where("id = 1").Update("proxy_username", "alice_px").Error; err != nil {
		t.Fatal(err)
	}
	err := gdb.Model(&User{}).Where("id = 2").Update("proxy_username", "alice_px").Error
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate proxy username accepted: %v", err)
	}
	if err := gdb.Model(&User{}).Where("id = 2").Update("proxy_username", "bob_px").Error; err != nil {
		t.Fatal(err)
	}
	// Re-running the whole chain on the upgraded schema is a no-op.
	if err := gdb.Exec("DELETE FROM schema_migrations WHERE version = 12").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatalf("schema 12 not idempotent: %v", err)
	}
	var u User
	gdb.First(&u, 1)
	if u.ProxyUsername != "alice_px" {
		t.Fatalf("value lost on re-run: %q", u.ProxyUsername)
	}
}

// The schema-12 statements render to valid SQL for both drivers (the
// partial unique index works on SQLite and PostgreSQL alike).
func TestSchema12Dialects(t *testing.T) {
	m := migrations[11]
	if m.Version != 12 {
		t.Fatalf("migration 12 at index 11 is %d", m.Version)
	}
	for _, d := range []string{"sqlite", "postgres"} {
		for _, st := range m.SQL {
			s := dialect(st, d)
			if strings.Contains(s, "{{") {
				t.Fatalf("%s: unrendered placeholder: %s", d, s)
			}
		}
	}
	if !reCreateIndex.MatchString(m.SQL[2]) || !reAddColumn.MatchString(m.SQL[0]) {
		t.Fatal("schema 12 statements are not handled idempotently")
	}
}
