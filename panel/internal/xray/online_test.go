package xray

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Only bytes received from the client mark a user online; downlink alone
// (writes towards a client that may already be gone) only counts as traffic.
func TestApplyUserTrafficOnlineFromUplink(t *testing.T) {
	gdb, err := db.Open("sqlite", filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, u := range []db.User{
		{ID: 1, Username: "a", Status: db.StatusActive, UUID: "u1", Password: "p", SubToken: "t1", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Username: "b", Status: db.StatusActive, UUID: "u2", Password: "p", SubToken: "t2", CreatedAt: now, UpdatedAt: now},
	} {
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	at := now.Add(-5 * time.Second).Truncate(time.Second)
	if err := ApplyUserTraffic(gdb, map[int64][2]int64{1: {100, 900}, 2: {0, 5000}}, at); err != nil {
		t.Fatal(err)
	}
	var a, b db.User
	gdb.First(&a, 1)
	gdb.First(&b, 2)
	if a.Up != 100 || a.Down != 900 || a.LastOnlineAt == nil || !a.LastOnlineAt.Equal(at) {
		t.Fatalf("user a: %+v", a)
	}
	if b.Down != 5000 || b.LastOnlineAt != nil {
		t.Fatalf("downlink-only marked user b online: %+v", b)
	}
}
