package db

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Upgrading a schema-13 database (v0.3.13) adds the description column
// with an empty value for every user, changes no existing
// value and can run again.
func TestUpgradeFromSchema13AddsDescription(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "u13.db"))
	saved := migrations
	migrations = saved[:13]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	if columnCount(t, gdb, "users", "description") != 0 {
		t.Fatal("column exists before schema 14")
	}
	now := time.Now().UTC()
	gdb.Exec(`INSERT INTO users (username, status, uuid, password, sub_token, created_at, updated_at, data_limit, up, down, proxy_username)
		VALUES ('hosein','active','u-1','p-1','tok-1',?,?,5000,11,22,'hx'), ('bob','disabled','u-2','p-2','tok-2',?,?,0,0,7,'')`, now, now, now, now)
	snap := func() string {
		var us []map[string]any
		gdb.Raw("SELECT id, username, status, uuid, password, sub_token, data_limit, up, down, proxy_username, proxy_password FROM users ORDER BY id").Scan(&us)
		return fmt.Sprint(us)
	}
	before := snap()
	for i := 0; i < 2; i++ {
		if err := Migrate(gdb, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	if v := SchemaVersion(gdb); v != len(migrations) || v < 14 {
		t.Fatalf("schema %d", v)
	}
	if after := snap(); after != before {
		t.Fatalf("data changed by the upgrade:\n%s\n%s", before, after)
	}
	var us []User
	gdb.Order("id").Find(&us)
	if len(us) != 2 || us[0].Description != "" || us[1].Description != "" {
		t.Fatalf("description not empty after upgrade: %+v", us)
	}
	if err := gdb.Model(&User{}).Where("id = 1").Update("description", "x").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("DELETE FROM schema_migrations WHERE version = 14").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatalf("schema 14 not idempotent: %v", err)
	}
	var u User
	gdb.First(&u, 1)
	if u.Description != "x" {
		t.Fatalf("value lost on re-run: %q", u.Description)
	}
}
