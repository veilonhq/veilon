package users

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestServiceCRUD(t *testing.T) {
	gdb := testutil.DB(t)
	ib := db.Inbound{Remark: "x", Enabled: true, Protocol: "vless", Port: 1000, Network: "tcp", Security: "none"}
	gdb.Create(&ib)
	changes := 0
	s := &Service{DB: gdb, OnChange: func() { changes++ }, SubURL: func(tok, host string) string { return "http://" + host + "/sub/" + tok }}
	var in Input
	_ = json.Unmarshal([]byte(`{"username":"alice","data_limit":100,"expire_at":null,"reset_strategy":"week","inbound_ids":[1,1]}`), &in)
	v, err := s.Create(&in, "h:1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != db.StatusActive || len(v.InboundIDs) != 1 || v.SubURL != "http://h:1/sub/"+v.SubToken || v.UUID == "" {
		t.Fatalf("bad view %+v", v)
	}
	if _, err := s.Create(&in, ""); err == nil {
		t.Fatal("duplicate username accepted")
	}
	var bad Input
	_ = json.Unmarshal([]byte(`{"username":"bob","inbound_ids":[99]}`), &bad)
	if _, err := s.Create(&bad, ""); err == nil {
		t.Fatal("unknown inbound accepted")
	}
	var p Input
	_ = json.Unmarshal([]byte(`{"status":"disabled","note":"n"}`), &p)
	v, err = s.Update(v.ID, &p, "")
	if err != nil || v.Status != db.StatusDisabled || v.Note != "n" || len(v.InboundIDs) != 1 {
		t.Fatalf("patch: %v %+v", err, v)
	}
	var p2 Input
	_ = json.Unmarshal([]byte(`{"status":"limited"}`), &p2)
	if _, err := s.Update(v.ID, &p2, ""); err == nil {
		t.Fatal("status limited should be rejected")
	}
	old := v.SubToken
	v, _ = s.RevokeSub(v.ID, "")
	if v.SubToken == old {
		t.Fatal("token not rotated")
	}
	if err := s.Extend(3, v.ID); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.List(ListParams{Search: "ALI"}, "")
	if err != nil || total != 1 || items[0].Username != "alice" {
		t.Fatalf("list: %v %d", err, total)
	}
	if n, err := s.Delete(v.ID); err != nil || n != 1 {
		t.Fatalf("delete: %v %d", err, n)
	}
	var links int64
	gdb.Model(&db.UserInbound{}).Count(&links)
	if links != 0 {
		t.Fatal("links not removed")
	}
	if changes == 0 {
		t.Fatal("OnChange never called")
	}
}

// On SQLite the pool has one connection: editing an active user (which
// recomputes the time-window suspension) must not read through the pool
// inside its own transaction, or it waits for itself forever.
func TestUpdateActiveUserDoesNotDeadlockOnSQLite(t *testing.T) {
	gdb := testutil.DB(t)
	ib := db.Inbound{Remark: "x", Enabled: true, Protocol: "vless", Port: 1000, Network: "tcp", Security: "none"}
	gdb.Create(&ib)
	_ = db.SetSetting(gdb, db.KeyTimezone, "Asia/Tehran")
	s := &Service{DB: gdb, SubURL: func(tok, host string) string { return tok }}
	var in Input
	_ = json.Unmarshal([]byte(`{"username":"alice","inbound_ids":[1]}`), &in)
	v, err := s.Create(&in, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var p Input
		_ = json.Unmarshal([]byte(`{"note":"x","inbound_ids":[1]}`), &p)
		_, err := s.Update(v.ID, &p, "")
		if err == nil {
			err = s.ResetTraffic(v.ID)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("updating an active user deadlocked")
	}
}

// One day is a valid expiry, from today (expire_at) and from the first
// connection (on_hold_days), on create and on edit; a new user never
// inherits another user's speed limit.
func TestOneDayExpiryAndIndependentSpeed(t *testing.T) {
	gdb := testutil.DB(t)
	gdb.Create(&db.Inbound{Remark: "x", Enabled: true, Protocol: "vless", Port: 1000, Network: "tcp", Security: "none"})
	s := &Service{DB: gdb, SubURL: func(tok, host string) string { return tok }}
	exp := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	mk := func(body string) *View {
		t.Helper()
		var in Input
		if err := json.Unmarshal([]byte(body), &in); err != nil {
			t.Fatal(err)
		}
		v, err := s.Create(&in, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a := mk(`{"username":"usera","expire_at":"` + exp.Format(time.RFC3339) + `","speed_limit_down":1,"speed_limit_up":1,"inbound_ids":[1]}`)
	if a.ExpireAt == nil || a.ExpireAt.Sub(time.Now()) < 23*time.Hour || a.Status != db.StatusActive || a.SpeedLimitDown != 1 {
		t.Fatalf("one-day user %+v", a)
	}
	b := mk(`{"username":"userb","on_hold_days":1,"inbound_ids":[1]}`)
	if b.OnHoldDays == nil || *b.OnHoldDays != 1 || b.Status != db.StatusOnHold {
		t.Fatalf("one-day on-hold user %+v", b)
	}
	if b.SpeedLimitDown != 0 || b.SpeedLimitUp != 0 {
		t.Fatalf("user b inherited a speed limit: %+v", b)
	}
	var p Input
	_ = json.Unmarshal([]byte(`{"on_hold_days":null,"expire_at":"`+exp.Format(time.RFC3339)+`"}`), &p)
	if v, err := s.Update(b.ID, &p, ""); err != nil || v.ExpireAt == nil || v.Status != db.StatusActive {
		t.Fatalf("edit to one day: %v %+v", err, v)
	}
}
