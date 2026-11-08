package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/config"
	"github.com/veilonhq/veilon/panel/internal/db"
)

func randPort(lo, hi int, avoid ...int) int {
	for {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(hi-lo)))
		p := lo + int(n.Int64())
		ok := true
		for _, a := range avoid {
			if a == p {
				ok = false
			}
		}
		if ok {
			return p
		}
	}
}

func randPath() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 14)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

// openDB loads config, opens the database and seeds default settings.
func openDB() (*config.Config, *gorm.DB, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	if cfg.DBDriver == "sqlite" {
		if err := cfg.EnsureDirs(); err != nil {
			return nil, nil, err
		}
	}
	gdb, err := db.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		return nil, nil, err
	}
	if err := bootstrap(gdb, cfg); err != nil {
		return nil, nil, fmt.Errorf("bootstrap settings: %w", err)
	}
	return cfg, gdb, nil
}

func bootstrap(gdb *gorm.DB, cfg *config.Config) error {
	port := cfg.InitPort
	if port <= 0 || port > 65535 {
		port = randPort(10000, 60000)
	}
	path := strings.Trim(cfg.InitPath, "/")
	if path == "" {
		path = randPath()
	}
	ps, err := db.SetDefault(gdb, db.KeyPanelPort, strconv.Itoa(port))
	if err != nil {
		return err
	}
	pp, _ := strconv.Atoi(ps)
	defaults := [][2]string{
		{db.KeyPanelPath, path},
		{db.KeySubPath, "sub"},
		{db.KeySubTitle, "Veilon"},
		{db.KeySubUpdateHours, "12"},
		{db.KeyLanguage, "fa"},
		{db.KeyTimezone, "UTC"},
		{db.KeyXrayAPIPort, strconv.Itoa(randPort(20000, 30000, pp))},
		{db.KeySingboxAPIPort, strconv.Itoa(randPort(30001, 39999, pp))},
		{db.KeyInstalledAt, time.Now().UTC().Format(time.RFC3339)},
	}
	for _, kv := range defaults {
		if _, err := db.SetDefault(gdb, kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}
