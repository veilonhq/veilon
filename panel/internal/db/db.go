// Package db opens the database (PostgreSQL or SQLite), runs versioned
// migrations and provides settings helpers.
package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects to the database and runs migrations. Another process that
// opens the same database at the same moment (the service starting while a
// CLI command runs) can hold SQLite's lock briefly; such a busy database is
// retried for a while instead of failing the start.
func Open(driver, dsn string) (*gorm.DB, error) {
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		gdb, err := open(driver, dsn)
		if err == nil || !isBusy(err) {
			return gdb, err
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}
	return nil, lastErr
}

func isBusy(err error) bool {
	m := err.Error()
	return strings.Contains(m, "SQLITE_BUSY") || strings.Contains(m, "database is locked")
}

func open(driver, dsn string) (*gorm.DB, error) {
	cfg := &gorm.Config{
		Logger:  logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time { return time.Now().UTC() },
	}
	var (
		gdb *gorm.DB
		err error
	)
	switch driver {
	case "postgres":
		gdb, err = gorm.Open(postgres.Open(dsn), cfg)
	case "sqlite":
		if dir := filepath.Dir(dsn); dir != "" && !strings.HasPrefix(dsn, "file:") && dsn != ":memory:" {
			_ = os.MkdirAll(dir, 0o750)
		}
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn = dsn + sep + "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
		gdb, err = gorm.Open(sqlite.Open(dsn), cfg)
		if err == nil {
			sqlDB, _ := gdb.DB()
			sqlDB.SetMaxOpenConns(1)
		}
	default:
		return nil, fmt.Errorf("unsupported db driver %q", driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}
	if err := Migrate(gdb, driver); err != nil {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return gdb, nil
}

// IsNotFound reports whether err is a record-not-found error.
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
