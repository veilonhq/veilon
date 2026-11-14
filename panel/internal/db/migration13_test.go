package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Upgrading a schema-12 database (v0.3.12) adds the per-user firewall
// column with ” for every user (nothing blocked), changes no existing
// value and can run again.
func TestUpgradeFromSchema12AddsFirewall(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "u12.db"))
	saved := migrations
	migrations = saved[:12]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	if columnCount(t, gdb, "users", "firewall") != 0 {
		t.Fatal("column exists before schema 13")
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
	if v := SchemaVersion(gdb); v != len(migrations) || v < 13 {
		t.Fatalf("schema %d", v)
	}
	if after := snap(); after != before {
		t.Fatalf("data changed by the upgrade:\n%s\n%s", before, after)
	}
	var us []User
	gdb.Order("id").Find(&us)
	if len(us) != 2 || us[0].Firewall != "" || us[1].Firewall != "" {
		t.Fatalf("firewall not empty after upgrade: %+v", us)
	}
	if err := gdb.Model(&User{}).Where("id = 1").Update("firewall", "example.com\n1.2.3.4").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("DELETE FROM schema_migrations WHERE version = 13").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatalf("schema 13 not idempotent: %v", err)
	}
	var u User
	gdb.First(&u, 1)
	if u.Firewall != "example.com\n1.2.3.4" {
		t.Fatalf("value lost on re-run: %q", u.Firewall)
	}
}

func TestSchema13Dialects(t *testing.T) {
	m := migrations[12]
	if m.Version != 13 || LatestSchema() < 13 {
		t.Fatalf("migration 13 at index 12 is %d (latest %d)", m.Version, LatestSchema())
	}
	for _, d := range []string{"sqlite", "postgres"} {
		for _, st := range m.SQL {
			if s := dialect(st, d); strings.Contains(s, "{{") {
				t.Fatalf("%s: unrendered placeholder: %s", d, s)
			}
		}
	}
	if !reAddColumn.MatchString(m.SQL[0]) {
		t.Fatal("schema 13 statement is not handled idempotently")
	}
}
