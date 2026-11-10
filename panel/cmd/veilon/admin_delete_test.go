package main

import (
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestAdminDelete(t *testing.T) {
	g := testutil.DB(t)
	root, _ := auth.CreateAdmin(g, "admin", "password123", true)
	sub, _ := auth.CreateAdmin(g, "reseller", "password123", false)
	if _, err := auth.CreateAdmin(g, "helper", "password123", false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	g.Create(&db.User{Username: "u1", Status: db.StatusActive, UUID: "x-1", Password: "p", SubToken: "t1", AdminID: &sub.ID, CreatedAt: now, UpdatedAt: now})

	if err := cmdAdminDelete(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("no username: %v", err)
	}
	if err := adminDelete(g, "nobody"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown: %v", err)
	}
	if err := adminDelete(g, "admin"); err == nil || !strings.Contains(err.Error(), "last full") {
		t.Fatalf("last sudo deleted: %v", err)
	}
	if err := adminDelete(g, "reseller"); err == nil || !strings.Contains(err.Error(), "owns 1 users") {
		t.Fatalf("owner deleted: %v", err)
	}
	if err := adminDelete(g, "helper"); err != nil {
		t.Fatal(err)
	}
	// with a second full admin, the first one may go
	if _, err := auth.CreateAdmin(g, "admin2", "password123", true); err != nil {
		t.Fatal(err)
	}
	if err := adminDelete(g, "admin"); err != nil {
		t.Fatal(err)
	}
	var names []string
	g.Model(&db.Admin{}).Order("id").Pluck("username", &names)
	if strings.Join(names, ",") != "reseller,admin2" {
		t.Fatalf("admins left: %v", names)
	}
	var n int64
	g.Model(&db.User{}).Count(&n)
	if n != 1 {
		t.Fatal("users touched")
	}
	var logged int64
	g.Model(&db.AuditLog{}).Where("action = ?", "admin.delete").Count(&logged)
	if logged != 2 {
		t.Fatalf("audit rows %d", logged)
	}
	_ = root
}
