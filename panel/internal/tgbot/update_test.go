package tgbot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/selfupdate"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

const notes = "## فارسی\n- پشتیبان گیری از ربات تلگرام\n- رفع `bug` در نود ها\n\n## English\n- Telegram bot backups\n- Fixed a node bug\n"

func updateRig(t *testing.T) (*rig, *selfupdate.Service) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v9.0.0", "name": "Veilon v9.0.0", "body": notes, "html_url": "https://example.invalid/v9.0.0"}})
	}))
	t.Cleanup(feed.Close)
	// The updater is "spawned" as /bin/true: nothing is really installed.
	t.Setenv("VEILON_UPDATER_SPAWN", "1")
	up := &selfupdate.Service{Checker: &selfupdate.Checker{Current: "v0.3.8", URL: feed.URL}, DataDir: t.TempDir(), Current: "v0.3.8",
		Self: "/bin/true", HealthURL: func() string { return "http://127.0.0.1:1/health" }}
	if err := up.Checker.CheckOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := newRigWith(t, func(srv *api.Server, _ *tgbot.Bot) { srv.Update = up })
	return r, up
}

func TestUpdateShowsNotesInPanelLanguage(t *testing.T) {
	r, _ := updateRig(t)
	r.start()
	c := r.press("upd", "بروزرسانی پنل")
	text := fmt.Sprint(c.p["text"])
	if !strings.Contains(text, "v9.0.0") || !strings.Contains(text, "پشتیبان گیری از ربات تلگرام") || !strings.Contains(text, "<code>bug</code>") {
		t.Fatalf("notes: %s", text)
	}
	if strings.Contains(text, "Telegram bot backups") {
		t.Fatalf("English notes shown on a Persian panel: %s", text)
	}
	// English panel: the English section.
	st := db.LoadSettings(r.gdb)
	st.Language = "en"
	_ = db.SaveSettings(r.gdb, st)
	c = r.press("upd", "بروزرسانی پنل")
	if text := fmt.Sprint(c.p["text"]); !strings.Contains(text, "Telegram bot backups") || strings.Contains(text, "پشتیبان گیری از ربات") {
		t.Fatalf("english notes: %s", text)
	}
}

func TestUpdateApplyNeedsConfirm(t *testing.T) {
	r, up := updateRig(t)
	r.start()
	v := r.press("upd", "بروزرسانی پنل")
	c := r.press(button(t, v, "نصب v9.0.0"), "بروزرسانی شود")
	requested := func() bool {
		_, err := os.Stat(selfupdate.RequestPath(up.DataDir))
		return err == nil
	}
	if requested() {
		t.Fatal("update requested before confirm")
	}
	r.press(button(t, c, "نه"), "بروزرسانی پنل")
	if requested() {
		t.Fatal("update requested on no")
	}
	c = r.press("upa", "بروزرسانی شود")
	c = r.press(button(t, c, "بله"), "در صف")
	if !requested() {
		t.Fatal("update not requested")
	}
	if !strings.Contains(fmt.Sprint(c.p["text"]), "⏳") {
		t.Fatalf("progress screen %v", c.p["text"])
	}
	var e db.AuditLog
	r.gdb.Where("action = ?", "panel.update").Limit(1).Find(&e)
	if e.AdminName != fmt.Sprintf("telegram:%d", adminID) {
		t.Fatalf("audit actor %q", e.AdminName)
	}
}
