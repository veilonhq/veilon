package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/backup"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestCreateCheckRestoreApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veilon.db")
	gdb := testutil.DBAt(t, path)
	if _, err := auth.CreateAdmin(gdb, "root", "password123", true); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&db.User{Username: "before", Status: db.StatusActive, SubToken: "tok-before"})
	var exits atomic.Int32
	s := &backup.Service{DB: gdb, Driver: "sqlite", DSN: path, DataDir: dir, Version: "v0.4.0", Exit: func() { exits.Add(1) }, ExitDelay: 10 * time.Millisecond}

	name, data, err := s.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(name) != ".db" || len(data) == 0 {
		t.Fatalf("backup %s %d bytes", name, len(data))
	}
	desc, err := s.Check(name, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(desc)
	if _, err := s.Check("x.db", []byte("hello")); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err := s.Check("x.pgdump", []byte("PGDMP....")); err == nil {
		t.Fatal("postgres dump accepted on sqlite")
	}

	// The database changes after the backup; the restore brings it back.
	gdb.Create(&db.User{Username: "after", Status: db.StatusActive, SubToken: "tok-after"})
	safety, err := s.Restore(name, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(safety); err != nil {
		t.Fatalf("safety copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backup.StageDir(dir), "pending.db")); err != nil {
		t.Fatalf("staged file: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if exits.Load() != 1 {
		t.Fatalf("exit called %d times", exits.Load())
	}
	// The safety copy has the newer state.
	if desc, err := s.Check("safety.db", mustRead(t, safety)); err != nil {
		t.Fatalf("safety copy invalid: %v (%s)", err, desc)
	}
	// Stop the panel's database, then apply as the next start does.
	sqlDB, _ := gdb.DB()
	sqlDB.Close()
	applied, err := backup.ApplyPending("sqlite", path, dir)
	if err != nil || !applied {
		t.Fatalf("apply %v %v", applied, err)
	}
	g2, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s, _ := g2.DB(); s.Close() }()
	var n int64
	g2.Model(&db.User{}).Where("username = ?", "after").Count(&n)
	if n != 0 {
		t.Fatal("restore not applied")
	}
	g2.Model(&db.User{}).Where("username = ?", "before").Count(&n)
	if n != 1 {
		t.Fatal("backup content missing")
	}
	if _, err := os.Stat(filepath.Join(backup.StageDir(dir), "pending.json")); !os.IsNotExist(err) {
		t.Fatal("pending marker left behind")
	}
	if r := (&backup.Service{DataDir: dir}).LastResult(); r == nil || !r.OK || r.Safety != safety {
		t.Fatalf("result %+v", r)
	}
	// Nothing staged: nothing to do.
	if applied, err := backup.ApplyPending("sqlite", path, dir); applied || err != nil {
		t.Fatalf("second apply %v %v", applied, err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
