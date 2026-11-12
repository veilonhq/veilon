package tgbot_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/backup"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

func backupRig(t *testing.T) (*rig, *backup.Service, *atomic.Int32) {
	dir := t.TempDir()
	var exits atomic.Int32
	var bs *backup.Service
	r := newRigWith(t, func(srv *api.Server, b *tgbot.Bot) {
		bs = &backup.Service{DB: srv.DB, Driver: "sqlite", DataDir: dir, Version: "v0.3.8",
			Exit: func() { exits.Add(1) }, ExitDelay: 10 * time.Millisecond}
		b.Backups = bs
	})
	return r, bs, &exits
}

// sendFile has the admin send a file to the bot.
func (r *rig) sendFile(name string, data []byte, want string) call {
	r.t.Helper()
	n := r.tg.mark()
	id := fmt.Sprintf("file%d", n)
	r.tg.mu.Lock()
	r.tg.files[id] = data
	r.tg.mu.Unlock()
	r.tg.push(map[string]any{"message": map[string]any{"message_id": 600 + n, "date": time.Now().Unix(),
		"document": map[string]any{"file_id": id, "file_name": name, "file_size": len(data)},
		"from":     map[string]any{"id": adminID, "is_bot": false, "first_name": "A"}, "chat": map[string]any{"id": adminID, "type": "private"}}})
	return r.tg.screen(n, want)
}

func (r *rig) document(n int) *call {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range r.tg.since(n) {
			if c.method == "sendDocument" {
				c := c
				return &c
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatalf("no document sent:%s", r.tg.dump(n))
	return nil
}

func TestBackupSendsDocument(t *testing.T) {
	r, _, _ := backupRig(t)
	r.start()
	v := r.press("bk", "پشتیبان")
	if !strings.Contains(fmt.Sprint(v.p["text"]), "۲۰ مگابایت") {
		t.Fatalf("no 20 MB note: %v", v.p["text"])
	}
	n := r.tg.mark()
	r.press(button(t, v, "گرفتن پشتیبان"), "فرستاده شد")
	doc := r.document(n)
	if doc.p["_field"] != "document" || !strings.HasSuffix(fmt.Sprint(doc.p["_name"]), ".db") ||
		!strings.HasPrefix(fmt.Sprint(doc.p["_data"]), "SQLite format 3") || fmt.Sprint(doc.p["chat_id"]) != fmt.Sprint(adminID) {
		t.Fatalf("document %v %v", doc.p["_name"], doc.p["chat_id"])
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "backup.create").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
}

func TestRestoreStagesFileAndSafetyCopy(t *testing.T) {
	r, bs, exits := backupRig(t)
	r.start()
	// A backup made by the bot itself.
	n := r.tg.mark()
	r.press("bkc", "فرستاده شد")
	doc := r.document(n)
	data := []byte(fmt.Sprint(doc.p["_data"]))

	v := r.press("bk", "پشتیبان")
	r.press(button(t, v, "بازگردانی"), "فایل پشتیبان پایگاه داده")
	// Not a backup: asked again.
	r.sendFile("notes.txt", []byte("hello"), "قابل بازگردانی نیست")
	c := r.sendFile("veilon.db", data, "بازگردانی انجام شود")
	staged := filepath.Join(backup.StageDir(bs.DataDir), "pending.db")
	if _, err := os.Stat(staged); err == nil {
		t.Fatal("staged before confirm")
	}
	c = r.press(button(t, c, "بله"), "نسخه ایمن")
	got, err := os.ReadFile(staged)
	if err != nil || string(got) != string(data) {
		t.Fatalf("staged file: %v (%d bytes)", err, len(got))
	}
	safety, _ := filepath.Glob(filepath.Join(backup.SafetyDir(bs.DataDir), "before-restore-*.db"))
	if len(safety) != 1 {
		t.Fatalf("safety copies %v", safety)
	}
	if !strings.Contains(fmt.Sprint(c.p["text"]), safety[0]) {
		t.Fatalf("safety path not shown: %v", c.p["text"])
	}
	time.Sleep(100 * time.Millisecond)
	if exits.Load() != 1 {
		t.Fatalf("restart asked %d times", exits.Load())
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "backup.restore").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
}

func TestRestoreTooLargeRefused(t *testing.T) {
	r, bs, _ := backupRig(t)
	r.start()
	r.press("bkr", "فایل پشتیبان پایگاه داده")
	n := r.tg.mark()
	r.tg.push(map[string]any{"message": map[string]any{"message_id": 900, "date": time.Now().Unix(),
		"document": map[string]any{"file_id": "big", "file_name": "big.db", "file_size": 25 << 20},
		"from":     map[string]any{"id": adminID, "is_bot": false, "first name": "A"}, "chat": map[string]any{"id": adminID, "type": "private"}}})
	r.tg.screen(n, "۲۰ مگابایت")
	if _, err := os.Stat(backup.StageDir(bs.DataDir)); err == nil {
		t.Fatal("staged a file over the limit")
	}
}
