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
	gdb, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
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
