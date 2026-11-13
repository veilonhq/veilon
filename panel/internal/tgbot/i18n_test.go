package tgbot_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

var rePersian = regexp.MustCompile(`[\x{0600}-\x{06FF}]`)

func TestLanguagePickedOnFirstStart(t *testing.T) {
	r := newRig(t)
	r.gdb.Where("key = ?", fmt.Sprintf("_telegram_lang_%d", adminID)).Delete(&db.Setting{})

	// The first /start asks for the language, in both languages.
	n := r.tg.mark()
	r.tg.text(adminID, 1, "/start")
	c := r.tg.screen(n, "Choose the bot's language")
	if !strings.Contains(fmt.Sprint(c.p["text"]), "زبان ربات را انتخاب کنید") {
		t.Fatalf("picker: %v", c.p["text"])
	}
	r.menu = r.tg.nextMsgOf(n)
	c = r.press(button(t, c, "English"), "Panel management")
	button(t, c, "👥 Users")
	if got := db.GetSetting(r.gdb, fmt.Sprintf("_telegram_lang_%d", adminID)); got != "en" {
		t.Fatalf("saved language %q", got)
	}

	// Later starts open the menu right away, in English.
	n = r.tg.mark()
	r.tg.text(adminID, 2, "/start")
	c = r.tg.screen(n, "Panel management")
	r.menu = r.tg.nextMsgOf(n)

	// The menu has a button to change the language.
	c = r.press(button(t, c, "Language"), "Choose the bot's language")
	c = r.press(button(t, c, "فارسی"), "مدیریت پنل")
	button(t, c, "🌐 زبان")
	if got := db.GetSetting(r.gdb, fmt.Sprintf("_telegram_lang_%d", adminID)); got != "fa" {
		t.Fatalf("saved language %q", got)
	}
}

// TestEnglishScreensHaveNoPersian opens the bot's sections in English and
// checks that no Persian text is left on them.
func TestEnglishScreensHaveNoPersian(t *testing.T) {
	r, _ := newXrayRig(t)
	if err := db.SetSetting(r.gdb, fmt.Sprintf("_telegram_lang_%d", adminID), "en"); err != nil {
		t.Fatal(err)
	}
	n := r.tg.mark()
	r.tg.text(adminID, 1, "/start")
	r.tg.screen(n, "Panel management")
	r.menu = r.tg.nextMsgOf(n)
	u := r.createUserEn("alice")

	screens := []struct{ data, want string }{
		{"ul:1", "Users"},
		{fmt.Sprintf("uv:%d", u.ID), "alice"},
		{fmt.Sprintf("ue:%d", u.ID), "What should change"},
		{fmt.Sprintf("ur:%d", u.ID), "What should be done"},
		{"il", "Inbounds"},
		{fmt.Sprintf("iv:%d", r.ib), "Protocol"},
		{"inp:vless", "Step 1"},
		{fmt.Sprintf("wze:%d", r.ib), "Security and transport"},
		{"wzj:sec", "Security"},
		{"wzj:dest", "Reality target"},
		{"nl", "Nodes"},
		{"sys", "Server status"},
		{"bk", "Backup"},
		{"xr", "Xray settings"},
		{"xo", "Outbounds"},
		{"xu", "Routing rules"},
		{"xuv:0", "Rule 1 of 2"},
		{"xov:0", "warp"},
		{"ps", "Panel settings"},
		{"al", "Admins"},
		{"au:1", "Activity log"},
		{"home", "Panel management"},
	}
	for _, s := range screens {
		n := r.tg.mark()
		c := r.press(s.data, s.want)
		text := fmt.Sprint(c.p["text"]) + fmt.Sprint(c.p["reply_markup"])
		if m := rePersian.FindString(text); m != "" {
			t.Errorf("%s: Persian left: %s", s.data, text)
		}
		if strings.ContainsAny(text, "\uE000\uE001\uE002") {
			t.Errorf("%s: language marks left: %q", s.data, text)
		}
		for _, cl := range r.tg.since(n) {
			if cl.method == "answerCallbackQuery" && rePersian.MatchString(fmt.Sprint(cl.p["text"])) {
				t.Errorf("%s: Persian toast %v", s.data, cl.p["text"])
			}
		}
	}
}

// createUserEn creates a user through the bot's English screens.
func (r *rig) createUserEn(name string) db.User {
	r.t.Helper()
	c := r.press("ul:1", "Users")
	c = r.press(button(r.t, c, "New user"), "New user (1 of 4)")
	c = r.typeText(name, "New user (2 of 4)")
	c = r.typeText("10", "New user (3 of 4)")
	c = r.press(button(r.t, c, "30 days"), "New user (4 of 4)")
	c = r.press(button(r.t, c, "Create user"), "alice")
	return r.user(name)
}
