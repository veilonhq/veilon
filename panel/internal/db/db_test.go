package db

import (
	"path/filepath"
	"testing"
)

func TestMigrateIdempotentAndSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	gdb, err := Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, "sqlite"); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if v := SchemaVersion(gdb); v != len(migrations) {
		t.Fatalf("schema version %d", v)
	}
	if v, _ := SetDefault(gdb, "k", "a"); v != "a" {
		t.Fatal(v)
	}
	if v, _ := SetDefault(gdb, "k", "b"); v != "a" {
		t.Fatal("SetDefault overwrote")
	}
	_ = SetSetting(gdb, "k", "c")
	if GetSetting(gdb, "k") != "c" {
		t.Fatal("SetSetting failed")
	}
	_ = AddCounter(gdb, "n", 5)
	_ = AddCounter(gdb, "n", 7)
	if GetSetting(gdb, "n") != "12" {
		t.Fatal(GetSetting(gdb, "n"))
	}
	s := LoadSettings(gdb)
	if s.SubPath != "sub" || s.SubUpdateHours != 12 || s.Language != "fa" {
		t.Fatalf("defaults %+v", s)
	}
	id := int64(3)
	s.PanelCertID, s.PanelPort, s.PanelPath = &id, 1234, "abc"
	if err := SaveSettings(gdb, s); err != nil {
		t.Fatal(err)
	}
	s2 := LoadSettings(gdb)
	if s2.PanelCertID == nil || *s2.PanelCertID != 3 || s2.PanelPort != 1234 || s2.PanelPath != "abc" {
		t.Fatalf("roundtrip %+v", s2)
	}
}
