package tgbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Setting keys (internal: never returned by the settings API).
const (
	KeyConfig = "_telegram"
	KeyToken  = "_telegram_token"
)

// MaxAdmins is the most Telegram admin ids a panel keeps.
const MaxAdmins = 20

// Notify switches the admin notifications on or off one by one.
type Notify struct {
	// Traffic: a user used 90% and 100% of the data limit.
	Traffic bool `json:"traffic"`
	// Expiry: a user expires within a day, and expired.
	Expiry bool `json:"expiry"`
	// Nodes: a node went offline or came back.
	Nodes bool `json:"nodes"`
	// Update: a new panel release is out.
	Update bool `json:"update"`
}

// Config is the bot's stored configuration. The token is kept apart and
// never leaves the server in full.
type Config struct {
	Enabled bool    `json:"enabled"`
	Admins  []int64 `json:"admins"`
	Proxy   string  `json:"proxy"`
	Notify  Notify  `json:"notify"`
	Token   string  `json:"-"`
}

// DefaultConfig is the configuration of a panel that never saved one.
func DefaultConfig() Config {
	return Config{Admins: []int64{}, Notify: Notify{Traffic: true, Expiry: true, Nodes: true, Update: true}}
}

// LoadConfig reads the stored configuration.
func LoadConfig(gdb *gorm.DB) Config {
	c := DefaultConfig()
	if raw := db.GetSetting(gdb, KeyConfig); raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.Admins == nil {
		c.Admins = []int64{}
	}
	c.Token = db.GetSetting(gdb, KeyToken)
	return c
}

// SaveConfig stores the configuration (token included).
func SaveConfig(gdb *gorm.DB, c Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return gdb.Transaction(func(tx *gorm.DB) error {
		if err := db.SetSetting(tx, KeyConfig, string(b)); err != nil {
			return err
		}
		return db.SetSetting(tx, KeyToken, c.Token)
	})
}

var tokenRe = regexp.MustCompile(`^[0-9]{3,15}:[A-Za-z0-9_-]{30,64}$`)

// ValidToken reports whether s looks like a bot token.
func ValidToken(s string) bool { return tokenRe.MatchString(s) }

// TokenHint is the safe form of a token shown to admins: the bot id and the
// last four characters.
func TokenHint(tok string) string {
	if tok == "" {
		return ""
	}
	id, rest, ok := strings.Cut(tok, ":")
	if !ok || len(rest) < 8 {
		return "••••"
	}
	return id + ":••••" + rest[len(rest)-4:]
}

// ParseAdmins reads "123, 456 789" into ids.
func ParseAdmins(s string) ([]int64, error) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '،' || r == ' ' || r == '\n' || r == '\t' })
	out := []int64{}
	seen := map[int64]bool{}
	for _, x := range f {
		id, err := strconv.ParseInt(toLatinDigits(x), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%q is not a numeric Telegram user id", x)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// Validate checks a configuration before it is saved.
func (c *Config) Validate() error {
	c.Proxy = strings.TrimSpace(c.Proxy)
	if c.Admins == nil {
		c.Admins = []int64{}
	}
	if len(c.Admins) > MaxAdmins {
		return fmt.Errorf("at most %d admin ids", MaxAdmins)
	}
	seen := map[int64]bool{}
	uniq := c.Admins[:0]
	for _, id := range c.Admins {
		if id <= 0 {
			return errors.New("admin ids must be positive numeric Telegram user ids")
		}
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	c.Admins = uniq
	if c.Token != "" && !ValidToken(c.Token) {
		return errors.New("the bot token does not look like a token from @BotFather (123456:ABC...)")
	}
	if c.Proxy != "" {
		u, err := url.Parse(c.Proxy)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
			return errors.New("proxy must be an http://, https:// or socks5:// URL")
		}
	}
	if c.Enabled {
		if c.Token == "" {
			return errors.New("a bot token is required to turn the bot on")
		}
		if len(c.Admins) == 0 {
			return errors.New("at least one admin id is required to turn the bot on")
		}
	}
	return nil
}

// IsAdmin reports whether a Telegram user may use the bot.
func (c *Config) IsAdmin(id int64) bool {
	for _, a := range c.Admins {
		if a == id {
			return true
		}
	}
	return false
}

func toLatinDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '۰' && r <= '۹':
			return '0' + (r - '۰')
		case r >= '٠' && r <= '٩':
			return '0' + (r - '٠')
		}
		return r
	}, s)
}
