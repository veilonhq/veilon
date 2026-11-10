package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func rawSQLite(t *testing.T, p string) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(p+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	return gdb
}

func columnCount(t *testing.T, gdb *gorm.DB, table, col string) int64 {
	t.Helper()
	var n int64
	gdb.Raw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, col).Scan(&n)
	return n
}

// The upgrade failure seen in the field: migration 2's column already exists
// but its version row is missing. Migrate must repair it instead of failing.
func TestMigrateRepairsPartiallyAppliedStep(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "p.db"))
	saved := migrations
	migrations = saved[:1]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	if err := gdb.Exec(`ALTER TABLE inbounds ADD COLUMN settings TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(dialect(`CREATE TABLE wg_peers (inbound_id BIGINT NOT NULL, user_id BIGINT NOT NULL, private_key VARCHAR(64) NOT NULL, public_key VARCHAR(64) NOT NULL, preshared_key VARCHAR(64) NOT NULL DEFAULT '', address VARCHAR(64) NOT NULL, created_at {{TS}} NOT NULL, PRIMARY KEY (inbound_id, user_id))`, "sqlite")).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatalf("migrate over partial schema: %v", err)
	}
	if v := SchemaVersion(gdb); v != len(migrations) {
		t.Fatalf("schema version %d, want %d", v, len(migrations))
	}
	if columnCount(t, gdb, "inbounds", "settings") != 1 || columnCount(t, gdb, "certs", "kind") != 1 {
		t.Fatal("columns missing after repair")
	}
}

// A failing migration must leave neither its DDL nor its version row behind.
func TestMigrateFailureRollsBack(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "r.db"))
	saved := migrations
	defer func() { migrations = saved }()
	migrations = append(append([]migration{}, saved...), migration{99, "broken", []string{
		`ALTER TABLE users ADD COLUMN half_done TEXT NOT NULL DEFAULT ''`,
		`THIS IS NOT SQL`,
	}})
	err := Migrate(gdb, "sqlite")
	if err == nil || !strings.Contains(err.Error(), "migration 99") {
		t.Fatalf("want migration 99 error, got %v", err)
	}
	if v := SchemaVersion(gdb); v != len(saved) {
		t.Fatalf("schema version %d, want %d", v, len(saved))
	}
	if columnCount(t, gdb, "users", "half_done") != 0 {
		t.Fatal("DDL of the failed migration was not rolled back")
	}
	migrations = saved
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
}

// Two processes starting at once (service restart racing a CLI call).
func TestMigrateConcurrentSQLite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.db")
	a, b := rawSQLite(t, p), rawSQLite(t, p)
	runConcurrent(t, "sqlite", a, b)
}

// Same race on Postgres, guarded by the advisory lock. Runs when
// VEILON_TEST_PG_DSN points at a scratch database.
func TestMigrateConcurrentPostgres(t *testing.T) {
	dsn := os.Getenv("VEILON_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("VEILON_TEST_PG_DSN not set")
	}
	open := func() *gorm.DB {
		gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		return gdb
	}
	a, b := open(), open()
	for _, tbl := range []string{"schema_migrations", "admins", "users", "inbounds", "user_inbounds", "settings", "certs", "audit_logs", "traffic_points", "wg_peers"} {
		a.Exec("DROP TABLE IF EXISTS " + tbl)
	}
	runConcurrent(t, "postgres", a, b)
	// partial state: drop version 2's row, keep its column
	a.Exec("DELETE FROM schema_migrations WHERE version >= 2")
	if err := Migrate(a, "postgres"); err != nil {
		t.Fatalf("repair: %v", err)
	}
}

func runConcurrent(t *testing.T, driver string, a, b *gorm.DB) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, g := range []*gorm.DB{a, b} {
		wg.Add(1)
		go func(i int, g *gorm.DB) {
			defer wg.Done()
			time.Sleep(time.Duration(i) * time.Millisecond)
			errs[i] = Migrate(g, driver)
		}(i, g)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("migrate %d: %v", i, err)
		}
	}
	if v := SchemaVersion(a); v != len(migrations) {
		t.Fatalf("schema version %d", v)
	}
	var n int64
	a.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&n)
	if n != int64(len(migrations)) {
		t.Fatalf("%d version rows", n)
	}
}

func TestLanguageDefaultsToPersian(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "l.db"))
	saved := migrations
	migrations = saved[:3]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	_ = SetSetting(gdb, KeyLanguage, "en") // seeded by an older build
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if l := LoadSettings(gdb).Language; l != "fa" {
		t.Fatalf("language %q after upgrade", l)
	}
	_ = SetSetting(gdb, KeyLanguage, "en") // chosen by the admin afterwards
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if l := LoadSettings(gdb).Language; l != "en" {
		t.Fatalf("explicit choice overwritten: %q", l)
	}
	_ = gdb.Exec("DELETE FROM settings WHERE key = ?", KeyLanguage)
	if l := LoadSettings(gdb).Language; l != "fa" {
		t.Fatalf("missing language falls back to %q", l)
	}
}

// Two processes opening the same SQLite file at once (service + CLI) must both
// come up: a busy database is retried rather than failing the start.
func TestOpenConcurrentSQLiteProcesses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.db")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g, err := Open("sqlite", p)
			errs[i] = err
			if err == nil {
				sqlDB, _ := g.DB()
				sqlDB.Close()
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}
}

// Upgrading a schema-5 database (the v0.2.x releases) applies the new
// migrations without touching existing users, inbounds or settings, and a
// second start is a no-op.
func TestUpgradeFromSchema5KeepsData(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "u.db"))
	saved := migrations
	migrations = saved[:5]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	if v := SchemaVersion(gdb); v != 5 {
		t.Fatalf("setup: schema %d", v)
	}
	now := time.Now().UTC()
	gdb.Exec(`INSERT INTO users (username, status, uuid, password, sub_token, created_at, updated_at, data_limit, up, down, ip_limit)
		VALUES ('alice','active','u-1','p-1','tok-1',?,?,1000,11,22,3)`, now, now)
	gdb.Exec(`INSERT INTO inbounds (remark, protocol, port, network, security, created_at, updated_at) VALUES ('VL', 'vless', 443, 'tcp', 'reality', ?, ?)`, now, now)
	gdb.Exec(`INSERT INTO user_inbounds (user_id, inbound_id) VALUES (1, 1)`)
	gdb.Exec(`INSERT INTO settings (key, value) VALUES ('server_address', '1.2.3.4')`)
	snap := func() string {
		var rows []map[string]any
		gdb.Raw("SELECT username, status, uuid, password, sub_token, data_limit, up, down, ip_limit FROM users").Scan(&rows)
		var ibs []map[string]any
		gdb.Raw("SELECT remark, protocol, port, network, security, enabled FROM inbounds").Scan(&ibs)
		var links, st []map[string]any
		gdb.Raw("SELECT * FROM user_inbounds").Scan(&links)
		gdb.Raw("SELECT * FROM settings").Scan(&st)
		return fmt.Sprint(rows, ibs, links, st)
	}
	before := snap()
	for i := 0; i < 2; i++ {
		if err := Migrate(gdb, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	if v := SchemaVersion(gdb); v != len(migrations) {
		t.Fatalf("schema %d, want %d", v, len(migrations))
	}
	if after := snap(); after != before {
		t.Fatalf("data changed by the upgrade:\n%s\n%s", before, after)
	}
}
