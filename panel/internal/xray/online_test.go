package xray

import (
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Traffic is counted on every poll, but last_online_at is no longer written
// per poll: package presence stores it on changes (at most once a minute
// while online).
func TestApplyUserTrafficDoesNotStampOnline(t *testing.T) {
	gdb, err := db.Open("sqlite", filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	u := db.User{ID: 1, Username: "a", Status: db.StatusActive, UUID: "u1", Password: "p", SubToken: "t1", CreatedAt: now, UpdatedAt: now}
	if err := gdb.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyUserTraffic(gdb, map[int64][2]int64{1: {100, 900}}, now); err != nil {
		t.Fatal(err)
	}
	var a db.User
	gdb.First(&a, 1)
	if a.Up != 100 || a.Down != 900 || a.LastOnlineAt != nil {
		t.Fatalf("user a: %+v", a)
	}
}

func TestDecodeOnlineUsers(t *testing.T) {
	var b []byte
	for _, s := range []string{"user>>>3.alice>>>online", "user>>>12.b.o.b>>>online", "user>>>junk>>>online", "inbound>>>x>>>online"} {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	ids, err := DecodeOnlineUsers(b)
	if err != nil || len(ids) != 2 || ids[0] != 3 || ids[1] != 12 {
		t.Fatalf("%v %v", ids, err)
	}
	if ids, err := DecodeOnlineUsers(nil); err != nil || len(ids) != 0 {
		t.Fatalf("empty: %v %v", ids, err)
	}
}

func TestTracksConnections(t *testing.T) {
	for v, want := range map[string]bool{"26.10.31": true, "v26.3.27": true, "26.11.9": true, "27.1.1": true,
		"26.2.6": false, "25.12.8": false, "": false, "dev": false} {
		if TracksConnections(v) != want {
			t.Fatalf("%q: want %v", v, want)
		}
	}
}
