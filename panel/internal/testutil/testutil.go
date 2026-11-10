// Package testutil provides helpers for tests.
package testutil

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// DB opens a migrated SQLite database in a temp dir.
func DB(t *testing.T) *gorm.DB {
	t.Helper()
	return DBAt(t, filepath.Join(t.TempDir(), "test.db"))
}

// DBAt opens a migrated SQLite database at path.
func DBAt(t *testing.T, path string) *gorm.DB {
	t.Helper()
	gdb, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if s, err := gdb.DB(); err == nil {
			s.Close()
		}
	})
	return gdb
}
