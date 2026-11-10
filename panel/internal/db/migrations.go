package db

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// migration is one versioned schema step. SQL uses placeholders that are
// replaced per dialect: {{PK}} {{TS}}.
type migration struct {
	Version int
	Name    string
	SQL     []string
}

var migrations = []migration{
	{1, "initial schema", []string{
		`CREATE TABLE admins (
			id {{PK}},
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_sudo BOOLEAN NOT NULL DEFAULT FALSE,
			password_changed_at {{TS}} NULL,
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE TABLE users (
			id {{PK}},
			username VARCHAR(64) NOT NULL UNIQUE,
			status VARCHAR(16) NOT NULL,
			data_limit BIGINT NOT NULL DEFAULT 0,
			up BIGINT NOT NULL DEFAULT 0,
			down BIGINT NOT NULL DEFAULT 0,
			expire_at {{TS}} NULL,
			on_hold_days INTEGER NULL,
			reset_strategy VARCHAR(16) NOT NULL DEFAULT 'no_reset',
			last_reset_at {{TS}} NULL,
			note TEXT NOT NULL DEFAULT '',
			uuid VARCHAR(36) NOT NULL,
			password VARCHAR(128) NOT NULL,
			sub_token VARCHAR(64) NOT NULL UNIQUE,
			last_online_at {{TS}} NULL,
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE INDEX idx_users_status ON users(status)`,
		`CREATE TABLE inbounds (
			id {{PK}},
			remark VARCHAR(128) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			protocol VARCHAR(16) NOT NULL,
			port INTEGER NOT NULL,
			listen VARCHAR(64) NOT NULL DEFAULT '',
			network VARCHAR(16) NOT NULL,
			security VARCHAR(16) NOT NULL,
			tls TEXT NOT NULL DEFAULT '',
			reality TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '',
			flow VARCHAR(32) NOT NULL DEFAULT '',
			ss_method VARCHAR(64) NOT NULL DEFAULT '',
			ss_server_key VARCHAR(128) NOT NULL DEFAULT '',
			sniffing BOOLEAN NOT NULL DEFAULT TRUE,
			address_override VARCHAR(255) NOT NULL DEFAULT '',
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE TABLE user_inbounds (
			user_id BIGINT NOT NULL,
			inbound_id BIGINT NOT NULL,
			PRIMARY KEY (user_id, inbound_id))`,
		`CREATE INDEX idx_user_inbounds_inbound ON user_inbounds(inbound_id)`,
		`CREATE TABLE settings (
			key VARCHAR(64) PRIMARY KEY,
			value TEXT NOT NULL)`,
		`CREATE TABLE certs (
			id {{PK}},
			domain VARCHAR(255) NOT NULL,
			issuer VARCHAR(255) NOT NULL DEFAULT '',
			not_after {{TS}} NOT NULL,
			auto_renew BOOLEAN NOT NULL DEFAULT FALSE,
			source VARCHAR(16) NOT NULL,
			email VARCHAR(255) NOT NULL DEFAULT '',
			cert_pem TEXT NOT NULL,
			key_pem TEXT NOT NULL,
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE TABLE audit_logs (
			id {{PK}},
			admin_id BIGINT NOT NULL DEFAULT 0,
			admin_name VARCHAR(64) NOT NULL DEFAULT '',
			action VARCHAR(64) NOT NULL,
			target TEXT NOT NULL DEFAULT '',
			ip VARCHAR(64) NOT NULL DEFAULT '',
			created_at {{TS}} NOT NULL)`,
		`CREATE INDEX idx_audit_created ON audit_logs(created_at)`,
		`CREATE TABLE traffic_points (
			id {{PK}},
			t {{TS}} NOT NULL,
			up BIGINT NOT NULL DEFAULT 0,
			down BIGINT NOT NULL DEFAULT 0)`,
		`CREATE INDEX idx_traffic_points_t ON traffic_points(t)`,
	}},
	{2, "multi-protocol inbounds", []string{
		`ALTER TABLE inbounds ADD COLUMN settings TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE wg_peers (
			inbound_id BIGINT NOT NULL,
			user_id BIGINT NOT NULL,
			private_key VARCHAR(64) NOT NULL,
			public_key VARCHAR(64) NOT NULL,
			preshared_key VARCHAR(64) NOT NULL DEFAULT '',
			address VARCHAR(64) NOT NULL,
			created_at {{TS}} NOT NULL,
			PRIMARY KEY (inbound_id, user_id))`,
		`CREATE INDEX idx_wg_peers_user ON wg_peers(user_id)`,
	}},
	{3, "acme ip certificates and profiles", []string{
		`ALTER TABLE certs ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'domain'`,
		`ALTER TABLE certs ADD COLUMN profile VARCHAR(32) NOT NULL DEFAULT ''`,
		`ALTER TABLE certs ADD COLUMN http_port INTEGER NOT NULL DEFAULT 80`,
		`ALTER TABLE certs ADD COLUMN not_before {{TS}} NULL`,
		`ALTER TABLE certs ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE certs ADD COLUMN last_attempt_at {{TS}} NULL`,
	}},
	// Persian is the default UI language. Earlier builds seeded "en" on first
	// start whatever the admin wanted; move that seed to "fa" once. An admin
	// can still pick English in Settings afterwards.
	{4, "persian default language", []string{
		`UPDATE settings SET value = 'fa' WHERE key = 'language' AND value = 'en'`,
	}},
	// Per-user limits, connection log (IPs and devices), groups and
	// sub-admin permissions.
	{5, "user limits, groups and admin permissions", []string{
		`ALTER TABLE users ADD COLUMN ip_limit INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN device_limit INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN require_hwid BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN speed_down_kbps BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN speed_up_kbps BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN traffic_multiplier {{FLOAT}} NOT NULL DEFAULT 1`,
		`ALTER TABLE users ADD COLUMN daily_limit BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN daily_used BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN daily_reset_at {{TS}} NULL`,
		`ALTER TABLE users ADD COLUMN block_torrent BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN window_from VARCHAR(5) NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN window_to VARCHAR(5) NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN window_days INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN suspend_reason VARCHAR(16) NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN group_id BIGINT NULL`,
		`ALTER TABLE users ADD COLUMN admin_id BIGINT NULL`,
		`CREATE INDEX idx_users_group ON users(group_id)`,
		`CREATE INDEX idx_users_admin ON users(admin_id)`,
		`CREATE TABLE user_groups (
			id {{PK}},
			name VARCHAR(64) NOT NULL UNIQUE,
			color VARCHAR(16) NOT NULL DEFAULT '',
			ad_block BOOLEAN NOT NULL DEFAULT FALSE,
			inbound_ids TEXT NOT NULL DEFAULT '',
			defaults TEXT NOT NULL DEFAULT '',
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE TABLE user_ips (
			id {{PK}},
			user_id BIGINT NOT NULL,
			ip VARCHAR(64) NOT NULL,
			country VARCHAR(8) NOT NULL DEFAULT '',
			first_seen {{TS}} NOT NULL,
			last_seen {{TS}} NOT NULL,
			hits BIGINT NOT NULL DEFAULT 0,
			blocked BOOLEAN NOT NULL DEFAULT FALSE)`,
		`CREATE UNIQUE INDEX idx_user_ips_user_ip ON user_ips(user_id, ip)`,
		`CREATE TABLE user_devices (
			id {{PK}},
			user_id BIGINT NOT NULL,
			hwid VARCHAR(128) NOT NULL,
			os VARCHAR(64) NOT NULL DEFAULT '',
			os_version VARCHAR(64) NOT NULL DEFAULT '',
			model VARCHAR(128) NOT NULL DEFAULT '',
			user_agent VARCHAR(255) NOT NULL DEFAULT '',
			ip VARCHAR(64) NOT NULL DEFAULT '',
			first_seen {{TS}} NOT NULL,
			last_seen {{TS}} NOT NULL,
			hits BIGINT NOT NULL DEFAULT 0,
			blocked BOOLEAN NOT NULL DEFAULT FALSE)`,
		`CREATE UNIQUE INDEX idx_user_devices_user_hwid ON user_devices(user_id, hwid)`,
		`ALTER TABLE admins ADD COLUMN permissions TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE admins ADD COLUMN user_quota INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE admins ADD COLUMN traffic_quota BIGINT NOT NULL DEFAULT 0`,
	}},
	// Hosts: per-inbound address / SNI / port / host / path / fingerprint /
	// ALPN overrides for share and subscription links. Inbounds without
	// hosts keep producing exactly the links they did before.
	{6, "inbound hosts", []string{
		`CREATE TABLE inbound_hosts (
			id {{PK}},
			inbound_id BIGINT NOT NULL,
			remark VARCHAR(64) NOT NULL DEFAULT '',
			address VARCHAR(255) NOT NULL DEFAULT '',
			port INTEGER NOT NULL DEFAULT 0,
			sni VARCHAR(255) NOT NULL DEFAULT '',
			host VARCHAR(255) NOT NULL DEFAULT '',
			path VARCHAR(255) NOT NULL DEFAULT '',
			fingerprint VARCHAR(32) NOT NULL DEFAULT '',
			alpn VARCHAR(64) NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			sort INTEGER NOT NULL DEFAULT 0,
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE INDEX idx_inbound_hosts_inbound ON inbound_hosts(inbound_id)`,
	}},
	// Nodes: remote servers running "veilon node" that serve chosen
	// inbounds. New tables only; existing data is untouched.
	{7, "nodes", []string{
		`CREATE TABLE nodes (
			id {{PK}},
			name VARCHAR(64) NOT NULL UNIQUE,
			address VARCHAR(255) NOT NULL,
			api_port INTEGER NOT NULL DEFAULT 62050,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			cert_sha256 VARCHAR(64) NOT NULL DEFAULT '',
			up BIGINT NOT NULL DEFAULT 0,
			down BIGINT NOT NULL DEFAULT 0,
			stats_seq VARCHAR(64) NOT NULL DEFAULT '',
			last_seen_at {{TS}} NULL,
			created_at {{TS}} NOT NULL,
			updated_at {{TS}} NOT NULL)`,
		`CREATE TABLE node_inbounds (
			node_id BIGINT NOT NULL,
			inbound_id BIGINT NOT NULL,
			PRIMARY KEY (node_id, inbound_id))`,
		`CREATE INDEX idx_node_inbounds_inbound ON node_inbounds(inbound_id)`,
	}},
	// Where an inbound runs: serve_main says whether the main server's own
	// cores serve it too. Additive only: every existing inbound gets TRUE,
	// so inbounds already assigned to nodes keep running on the main server
	// exactly as before. Inbounds without nodes always run on the main
	// server, whatever the flag says.
	{8, "inbound placement", []string{
		`ALTER TABLE inbounds ADD COLUMN serve_main BOOLEAN NOT NULL DEFAULT TRUE`,
	}},
	// Traffic history per server: node_id 0 is the main server, any other
	// value the node that carried the traffic. Additive: existing points
	// (written before the split) stay as they are and count as the main
	// server's.
	{9, "traffic history per server", []string{
		`ALTER TABLE traffic_points ADD COLUMN node_id BIGINT NOT NULL DEFAULT 0`,
	}},
}

type schemaMigration struct {
	Version   int `gorm:"primaryKey;autoIncrement:false"`
	Name      string
	AppliedAt time.Time
}

func (schemaMigration) TableName() string { return "schema_migrations" }

// migrateLockKey is the Postgres advisory lock id that serialises concurrent
// starts (service + CLI, or a restart racing the previous process).
const migrateLockKey int64 = 0x7665696c6f6e // "veilon"

func dialect(sql, driver string) string {
	pk, ts, fl := "BIGSERIAL PRIMARY KEY", "TIMESTAMPTZ", "DOUBLE PRECISION"
	if driver == "sqlite" {
		pk, ts, fl = "INTEGER PRIMARY KEY AUTOINCREMENT", "DATETIME", "REAL"
	}
	return strings.NewReplacer("{{PK}}", pk, "{{TS}}", ts, "{{FLOAT}}", fl).Replace(sql)
}

var (
	reCreateTable = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)
	reCreateIndex = regexp.MustCompile(`(?is)^\s*CREATE\s+(UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?`)
	reAddColumn   = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+(\w+)\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+(.+)$`)
)

// execIdempotent runs one schema statement so that re-running it against a
// schema that already has the object is a no-op: CREATE TABLE/INDEX get
// IF NOT EXISTS, and ADD COLUMN is skipped when the column is already there
// (SQLite has no ADD COLUMN IF NOT EXISTS, so existence is checked first).
func execIdempotent(tx *gorm.DB, stmt, driver string) error {
	stmt = dialect(stmt, driver)
	switch {
	case reAddColumn.MatchString(stmt):
		m := reAddColumn.FindStringSubmatch(stmt)
		table, col, def := m[1], m[2], m[3]
		has, err := hasColumn(tx, driver, table, col)
		if err != nil {
			return err
		}
		if has {
			return nil
		}
		return tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, def)).Error
	case reCreateTable.MatchString(stmt):
		stmt = reCreateTable.ReplaceAllString(stmt, "CREATE TABLE IF NOT EXISTS ")
	case reCreateIndex.MatchString(stmt):
		stmt = reCreateIndex.ReplaceAllString(stmt, "CREATE ${1}INDEX IF NOT EXISTS ")
	}
	return tx.Exec(stmt).Error
}

func hasColumn(tx *gorm.DB, driver, table, col string) (bool, error) {
	var n int64
	var err error
	if driver == "sqlite" {
		err = tx.Raw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, col).Scan(&n).Error
	} else {
		err = tx.Raw(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, col).Scan(&n).Error
	}
	return n > 0, err
}

// Migrate applies all pending migrations. Each migration runs in its own
// transaction together with the schema_migrations row that records it, so a
// migration is either fully applied and recorded or not at all. Statements
// are idempotent, which also repairs databases where a step's DDL landed
// without its version row. On Postgres a session advisory lock serialises
// concurrent starts; on SQLite the first statement of each transaction takes
// the write lock, and the version is re-checked under it.
func Migrate(gdb *gorm.DB, driver string) (err error) {
	if driver == "postgres" {
		sqlDB, derr := gdb.DB()
		if derr != nil {
			return derr
		}
		ctx := context.Background()
		conn, cerr := sqlDB.Conn(ctx)
		if cerr != nil {
			return cerr
		}
		defer conn.Close()
		if _, lerr := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); lerr != nil {
			return fmt.Errorf("migration lock: %w", lerr)
		}
		defer func() {
			_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrateLockKey)
		}()
	}
	ts := "TIMESTAMPTZ"
	if driver == "sqlite" {
		ts = "DATETIME"
	}
	if err := gdb.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name VARCHAR(128) NOT NULL, applied_at ` + ts + ` NOT NULL)`).Error; err != nil {
		return err
	}
	var applied []schemaMigration
	if err := gdb.Find(&applied).Error; err != nil {
		return err
	}
	done := map[int]bool{}
	for _, a := range applied {
		done[a.Version] = true
	}
	for _, m := range migrations {
		if done[m.Version] {
			continue
		}
		if err := applyMigration(gdb, driver, m); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(gdb *gorm.DB, driver string, m migration) error {
	return gdb.Transaction(func(tx *gorm.DB) error {
		if driver == "sqlite" {
			// A write as the first statement upgrades the transaction to a
			// write transaction (waiting on busy_timeout if another process
			// holds it), so the re-check below cannot race.
			if err := tx.Exec("UPDATE schema_migrations SET name = name WHERE version = ?", m.Version).Error; err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
			}
		}
		var n int64
		if err := tx.Raw("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", m.Version).Scan(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil // applied by a concurrent start
		}
		for _, s := range m.SQL {
			if err := execIdempotent(tx, s, driver); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
			}
		}
		return tx.Create(&schemaMigration{Version: m.Version, Name: m.Name, AppliedAt: time.Now().UTC()}).Error
	})
}

// SchemaVersion returns the latest applied migration version.
func SchemaVersion(gdb *gorm.DB) int {
	var v int
	gdb.Raw("SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&v)
	return v
}
