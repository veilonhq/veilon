package tgbot_test

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/tgbot"
)

// TestInboundViewXrayTooOld: an inbound whose settings need a newer
// Xray-core than the main server runs says so on its screen, and a new
// feature the server lacks is refused.
func TestInboundViewXrayTooOld(t *testing.T) {
	var ver atomic.Value
	ver.Store("")
	r := newRigWith(t, func(srv *api.Server, _ *tgbot.Bot) {
		srv.Inbounds.XrayVersion = func(int64) string { return ver.Load().(string) }
	})
	r.start()
	id := r.ib
	// Unknown version: ML-DSA-65 is let through.
	r.press(fmt.Sprintf("xs:%d:ml", id), "ML-DSA-65 روشن است")
	c := r.press(fmt.Sprintf("iv:%d", id), "پروتکل")
	if strings.Contains(fmt.Sprint(c.p["text"]), "Xray-core") {
		t.Fatalf("warning with an unknown version: %v", c.p["text"])
	}

	ver.Store("25.6.8")
	c = r.press(fmt.Sprintf("iv:%d", id), "سرور اصلی با Xray-core")
	text := strings.NewReplacer("\u2066", "", "\u2069", "").Replace(fmt.Sprint(c.p["text"]))
	if !strings.Contains(text, "این ورودی را اجرا نمی کند؛ نیاز: Reality ML-DSA-65 ≥ 25.7.26") || strings.ContainsRune(text, '\u200c') {
		t.Fatalf("fa: %s", text)
	}
	// The stored feature is not re-checked: the inbound can still be edited.
	r.press(fmt.Sprintf("it:%d", id), "⛔️ خاموش")

	if err := db.SetSetting(r.gdb, fmt.Sprintf("_telegram_lang_%d", adminID), "en"); err != nil {
		t.Fatal(err)
	}
	c = r.press(fmt.Sprintf("iv:%d", id), "runs Xray-core")
	text = strings.NewReplacer("\u2066", "", "\u2069", "").Replace(fmt.Sprint(c.p["text"]))
	if !strings.Contains(text, "leaves this inbound out; it needs: Reality ML-DSA-65 ≥ 25.7.26") || rePersian.MatchString(text) {
		t.Fatalf("en: %s", text)
	}
}
