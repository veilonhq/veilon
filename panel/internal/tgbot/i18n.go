package tgbot

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// The bot speaks Persian and English. Each admin picks a language the first
// time they open the bot (/start) and can change it from the main menu.
//
// Screens are built once with both languages in them: T and Tf wrap a
// Persian text and its English translation (from the en catalog) in private
// use marks, and the text is resolved to one language right before it goes
// to Telegram (Client.Send/Edit/..., req.toast). User data (names, notes,
// errors) is never wrapped, so it is shown exactly as it was typed.

// Languages.
const (
	langFa = "fa"
	langEn = "en"
)

// Marks of a two language text: mOpen + Persian + mSep + English + mClose.
// Texts nest (a translated label inside a translated sentence).
const (
	mOpen  = '\uE000'
	mSep   = '\uE001'
	mClose = '\uE002'
)

// both returns a text that reads fa in Persian and en in English.
func both(fa, en string) string {
	if fa == en {
		return fa
	}
	return string(mOpen) + fa + string(mSep) + en + string(mClose)
}

// T translates a Persian text with the en catalog. A text with no
// translation is shown as is in both languages (TestCatalogComplete keeps
// the catalog whole).
func T(fa string) string {
	en, ok := enCatalog[fa]
	if !ok {
		return fa
	}
	return both(fa, en)
}

// Tf formats a Persian format string and its English translation with the
// same arguments.
func Tf(format string, args ...any) string {
	fa := fmt.Sprintf(format, args...)
	enf, ok := enCatalog[format]
	if !ok {
		return fa
	}
	return both(fa, fmt.Sprintf(enf, args...))
}

// localize resolves a two language text to lang. In English, Persian digits
// written by fa() for numbers are already Latin; only the marks are removed.
func localize(s, lang string) string {
	if !strings.ContainsRune(s, mOpen) {
		return s
	}
	en := lang == langEn
	var out strings.Builder
	out.Grow(len(s))
	// keep[i]: whether the text at depth i+1 is shown. A text inside a part
	// that is not shown is not shown either.
	var keep []bool
	shown := func() bool {
		for _, k := range keep {
			if !k {
				return false
			}
		}
		return true
	}
	for _, c := range s {
		switch c {
		case mOpen:
			keep = append(keep, !en)
		case mSep:
			if n := len(keep); n > 0 {
				keep[n-1] = en
			}
		case mClose:
			if n := len(keep); n > 0 {
				keep = keep[:n-1]
			}
		default:
			if shown() {
				out.WriteRune(c)
			}
		}
	}
	return out.String()
}

// localizeKB resolves the button texts of a keyboard.
func localizeKB(kb Keyboard, lang string) Keyboard {
	if kb == nil {
		return nil
	}
	out := make(Keyboard, len(kb))
	for i, row := range kb {
		out[i] = make([]Button, len(row))
		for j, b := range row {
			b.Text = localize(b.Text, lang)
			out[i][j] = b
		}
	}
	return out
}

// cutText shortens a text to n characters in each language.
func cutText(s string, n int) string {
	if !strings.ContainsRune(s, mOpen) {
		if utf8.RuneCountInString(s) <= n {
			return s
		}
		rs := []rune(s)
		return string(rs[:n-1]) + "…"
	}
	return both(cutText(localize(s, langFa), n), cutText(localize(s, langEn), n))
}

// --- the admin's language ----------------------------------------------------

// keyLang stores an admin's language: keyLang + Telegram id = "fa" | "en".
const keyLang = "_telegram_lang_"

func loadLang(gdb *gorm.DB, tgID int64) string {
	switch v := db.GetSetting(gdb, fmt.Sprintf("%s%d", keyLang, tgID)); v {
	case langFa, langEn:
		return v
	}
	return ""
}

func saveLang(gdb *gorm.DB, tgID int64, lang string) error {
	return db.SetSetting(gdb, fmt.Sprintf("%s%d", keyLang, tgID), lang)
}

// langOf is the language of a chat (Persian until the admin picks one).
func (b *Bot) langOf(chat int64) string {
	if b.DB == nil {
		return langFa
	}
	if l := loadLang(b.DB, chat); l != "" {
		return l
	}
	return langFa
}

func init() {
	// lang:<fa|en> saves the admin's language and opens the main menu.
	on("lang", func(r *req, a []string) {
		l := argStr(a, 0)
		if l != langFa && l != langEn {
			langMenu(r, nil)
			return
		}
		if err := saveLang(r.b.DB, r.from, l); err != nil {
			r.fail(err, "home")
			return
		}
		r.toast(both("✅ فارسی", "✅ English"))
		home(r, nil)
	})
	on("langm", langMenu)
}

// langMenu asks the admin to pick a language (in both languages, since it
// may not be known yet).
func langMenu(r *req, _ []string) {
	r.st.pending = nil
	text := "🌐 <b>Veilon</b>\n" + line + "\nزبان ربات را انتخاب کنید:\nChoose the bot's language:"
	r.show(view{text: text, kb: Keyboard{{btn("🇮🇷 فارسی", "lang", langFa), btn("🇬🇧 English", "lang", langEn)}}})
}
