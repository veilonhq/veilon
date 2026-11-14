package db

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Upgrading a schema-9 database (v0.3.8) adds the telegram_notices table and
// changes no existing row: users, inbounds and their links are identical.
func TestUpgradeFromSchema9AddsTelegramNotices(t *testing.T) {
	gdb := rawSQLite(t, filepath.Join(t.TempDir(), "u9.db"))
	saved := migrations
	migrations = saved[:9]
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrations = saved
	now := time.Now().UTC()
	gdb.Exec(`INSERT INTO users (username, status, uuid, password, sub_token, created_at, updated_at, data_limit, up, down, ip_limit)
		VALUES ('hosein','active','u-1','p-1','tok-1',?,?,5000,11,22,2), ('node-test','disabled','u-2','p-2','tok-2',?,?,0,0,7,0)`, now, now, now, now)
	gdb.Exec(`INSERT INTO inbounds (remark, protocol, port, network, security, created_at, updated_at, serve_main) VALUES ('VL', 'vless', 443, 'tcp', 'reality', ?, ?, TRUE)`, now, now)
	gdb.Exec(`INSERT INTO inbounds (remark, protocol, port, network, security, created_at, updated_at, serve_main) VALUES ('AWG', 'amneziawg', 8443, 'udp', 'none', ?, ?, FALSE)`, now, now)
	gdb.Exec(`INSERT INTO user_inbounds (user_id, inbound_id) VALUES (1, 1), (1, 2), (2, 1)`)
	snap := func() string {
		var us, ibs, ui []map[string]any
		gdb.Raw("SELECT * FROM users ORDER BY id").Scan(&us)
		for _, u := range us {
			// Columns added by later migrations (schema 12, 13).
			delete(u, "proxy_username")
			delete(u, "proxy_password")
			delete(u, "firewall")
		}
		gdb.Raw("SELECT * FROM inbounds ORDER BY id").Scan(&ibs)
		gdb.Raw("SELECT * FROM user_inbounds ORDER BY user_id, inbound_id").Scan(&ui)
		return fmt.Sprint(us, ibs, ui)
	}
	before := snap()
	if len(before) < 50 {
		t.Fatalf("setup rows missing: %s", before)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(gdb, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	if v := SchemaVersion(gdb); v != len(migrations) || v < 10 {
		t.Fatalf("schema %d", v)
	}
	if after := snap(); after != before {
		t.Fatalf("data changed by the upgrade:\n%s\n%s", before, after)
	}
	n := TelegramNotice{Kind: "traffic90", SubjectID: 1, Mark: "2026-11", CreatedAt: now}
	if err := gdb.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	dup := TelegramNotice{Kind: "traffic90", SubjectID: 1, Mark: "2026-11", CreatedAt: now}
	if err := gdb.Create(&dup).Error; err == nil {
		t.Fatal("duplicate notice accepted")
	}
}
