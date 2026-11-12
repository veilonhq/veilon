package tgbot_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
)

func TestServerStatus(t *testing.T) {
	r := newRig(t)
	r.start()
	c := r.press("sys", "وضعیت سرور")
	text := fmt.Sprint(c.p["text"])
	for _, want := range []string{"پردازنده", "رم", "دیسک", "روشن از", "آنلاین", "Xray"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status lacks %q: %s", want, text)
		}
	}
	button(t, c, "لاگ ها")
}

func TestLogsSentAsFileWhenLong(t *testing.T) {
	r := newRig(t)
	r.start()
	n := r.tg.mark()
	r.press("sl:all", "لاگ ها")
	var doc *call
	deadline := time.Now().Add(3 * time.Second)
	for doc == nil && time.Now().Before(deadline) {
		for _, c := range r.tg.since(n) {
			if c.method == "sendDocument" {
				c := c
				doc = &c
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if doc == nil {
		t.Fatalf("long logs not sent as a file:%s", r.tg.dump(n))
	}
	if !strings.HasSuffix(fmt.Sprint(doc.p["_name"]), ".log") || !strings.Contains(fmt.Sprint(doc.p["_data"]), "accepted tcp") {
		t.Fatalf("log file %v", doc.p["_name"])
	}
}

func TestCoreRestartNeedsConfirm(t *testing.T) {
	r := newRig(t)
	r.start()
	v := r.press("sys", "وضعیت سرور")
	c := r.press(button(t, v, "راه اندازی دوباره هسته"), "دوباره راه اندازی شوند")
	count := func() int64 {
		var n int64
		r.gdb.Model(&db.AuditLog{}).Where("action = ?", "core.restart").Count(&n)
		return n
	}
	if count() != 0 {
		t.Fatal("restarted before confirm")
	}
	r.press(button(t, c, "بله"), "وضعیت سرور")
	if count() != 1 {
		t.Fatal("core not restarted")
	}
}
