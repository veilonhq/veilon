package tgbot_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

func newNotifyRig(t *testing.T, n tgbot.Notify) *rig {
	r := newRigWith(t, func(_ *api.Server, b *tgbot.Bot) { b.NotifyEvery = 80 * time.Millisecond })
	r.setNotify(n)
	return r
}

func (r *rig) setNotify(n tgbot.Notify) {
	r.t.Helper()
	c := tgbot.LoadConfig(r.gdb)
	c.Notify = n
	if err := tgbot.SaveConfig(r.gdb, c); err != nil {
		r.t.Fatal(err)
	}
}

// notices returns the notification messages sent since mark n.
func (r *rig) notices(n int) []call {
	var out []call
	for _, c := range r.tg.since(n) {
		if c.method == "sendMessage" && strings.Contains(fmt.Sprint(c.p["text"]), "اعلان ها") {
			out = append(out, c)
		}
	}
	return out
}

func (r *rig) waitNotices(n, want int) []call {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := r.notices(n); len(got) >= want {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatalf("%d notices, want %d:%s", len(r.notices(n)), want, r.tg.dump(n))
	return nil
}

func (r *rig) setUsage(id int64, used int64) {
	r.gdb.Model(&db.User{}).Where("id = ?", id).Updates(map[string]any{"up": 0, "down": used})
}

func TestNotifySentOnce(t *testing.T) {
	r := newNotifyRig(t, tgbot.Notify{Traffic: true})
	n := r.tg.mark()
	u := r.createUser("alice", 10, 30)
	r.setUsage(u.ID, 95*(10<<30)/100)
	got := r.waitNotices(n, 1)
	text := fmt.Sprint(got[0].p["text"])
	if !strings.Contains(text, "alice") || !strings.Contains(text, "۹۵٪") {
		t.Fatalf("notice %s", text)
	}
	if got[0].p["chat_id"].(float64) != float64(adminID) {
		t.Fatalf("sent to %v", got[0].p["chat_id"])
	}
	button(t, got[0], "alice")
	button(t, got[0], "۳۰ روز")
	// Many rounds later: still one notice.
	time.Sleep(600 * time.Millisecond)
	if c := len(r.notices(n)); c != 1 {
		t.Fatalf("%d notices for one event:%s", c, r.tg.dump(n))
	}
	// Crossing 100% is a new event.
	r.setUsage(u.ID, 10<<30)
	got = r.waitNotices(n, 2)
	if !strings.Contains(fmt.Sprint(got[1].p["text"]), "حجمش تمام شد") {
		t.Fatalf("second notice %v", got[1].p["text"])
	}
	// After a usage reset the 90% notice is armed again.
	r.setUsage(u.ID, 0)
	time.Sleep(300 * time.Millisecond)
	r.setUsage(u.ID, 92*(10<<30)/100)
	got = r.waitNotices(n, 3)
	if !strings.Contains(fmt.Sprint(got[2].p["text"]), "حجمش را مصرف کرد") {
		t.Fatalf("third notice %v", got[2].p["text"])
	}
	time.Sleep(300 * time.Millisecond)
	if c := len(r.notices(n)); c != 3 {
		t.Fatalf("%d notices:%s", c, r.tg.dump(n))
	}
}

func TestNotifyToggleOffSendsNothing(t *testing.T) {
	r := newNotifyRig(t, tgbot.Notify{Expiry: true})
	n := r.tg.mark()
	u := r.createUser("bob", 10, 30)
	r.setUsage(u.ID, 10<<30) // traffic is switched off
	time.Sleep(600 * time.Millisecond)
	if got := r.notices(n); len(got) != 0 {
		t.Fatalf("notice with traffic off:%s", r.tg.dump(n))
	}
	r.setNotify(tgbot.Notify{})
	exp := time.Now().UTC().Add(5 * time.Hour)
	r.gdb.Model(&db.User{}).Where("id = ?", u.ID).Update("expire_at", exp)
	time.Sleep(600 * time.Millisecond)
	if got := r.notices(n); len(got) != 0 {
		t.Fatalf("notice with everything off:%s", r.tg.dump(n))
	}
	var rows int64
	r.gdb.Model(&db.TelegramNotice{}).Count(&rows)
	if rows != 0 {
		t.Fatalf("%d notices recorded while off", rows)
	}
	// Switched on again: the pending event goes out.
	r.setNotify(tgbot.Notify{Expiry: true})
	got := r.waitNotices(n, 1)
	if !strings.Contains(fmt.Sprint(got[0].p["text"]), "کمتر از یک روز") {
		t.Fatalf("notice %v", got[0].p["text"])
	}
}

func TestNotifyGroupedInOneMessage(t *testing.T) {
	r := newRigWith(t, func(_ *api.Server, b *tgbot.Bot) { b.NotifyEvery = 300 * time.Millisecond })
	a := r.createUser("ann", 10, 30)
	c := r.createUser("cid", 10, 30)
	d := r.createUser("dee", 10, 30)
	r.setUsage(a.ID, 10<<30)
	r.setUsage(c.ID, 11<<30)
	r.gdb.Model(&db.User{}).Where("id = ?", d.ID).Update("expire_at", time.Now().UTC().Add(-time.Hour))
	n := r.tg.mark()
	r.setNotify(tgbot.Notify{Traffic: true, Expiry: true})
	got := r.waitNotices(n, 1)
	time.Sleep(700 * time.Millisecond)
	if all := r.notices(n); len(all) != 1 {
		t.Fatalf("%d messages, want one:%s", len(all), r.tg.dump(n))
	}
	text := fmt.Sprint(got[0].p["text"])
	for _, w := range []string{"ann", "cid", "dee", "📊 حجم", "⏳ انقضا", "منقضی شد"} {
		if !strings.Contains(text, w) {
			t.Fatalf("missing %q in %s", w, text)
		}
	}
	// The buttons open a new screen below the notice; renew asks first.
	r.menu = 0
	m := r.tg.mark()
	r.tg.press(adminID, 999, button(t, got[0], "۳۰ روز"))
	conf := r.tg.screen(m, "۳۰ روز به زمان")
	if conf.method != "sendMessage" {
		t.Fatalf("renew screen drawn with %s", conf.method)
	}
	before := r.user("ann").ExpireAt
	m = r.tg.mark()
	r.tg.press(adminID, 999, button(t, conf, "بله"))
	r.tg.screen(m, "ann")
	after := r.user("ann").ExpireAt
	if d := after.Sub(*before); d < 30*24*time.Hour-time.Minute || d > 30*24*time.Hour+time.Minute {
		t.Fatalf("extended by %v", d)
	}
}
