// Package config loads Veilon runtime configuration from an env-style file
// and environment variables (environment wins).
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultFile is the default configuration file location.
const DefaultFile = "/etc/veilon/veilon.env"

// Config is the process-level configuration. Settings that admins can change
// at runtime (port, panel path, domains...) live in the database instead.
type Config struct {
	File             string
	DataDir          string
	DBDriver         string // postgres | sqlite
	DBDSN            string
	Listen           string // bind host for the panel, default 0.0.0.0
	XrayBin          string
	XrayAutoDownload bool
	XrayVersion      string // "latest" or a tag such as v25.1.30
	SingboxBin       string
	SingboxVersion   string // release such as 1.14.4, or "latest"
	MTGBin           string
	MTGVersion       string // mtg-multi release, or "latest"
	TrustProxy       bool
	ACMEDirectory    string
	// ACMECAFile and ACMEAllowPrivateIPs exist only for local testing
	// against a private ACME CA such as Pebble: a PEM bundle of extra roots
	// trusted for the ACME directory, and permission to order certificates
	// for private or loopback IPs.
	ACMECAFile          string
	ACMEAllowPrivateIPs bool
	JWTTTL              time.Duration
	// Seed values used only when the settings row does not exist yet.
	InitPort int
	InitPath string
	// Optional bootstrap admin created on first serve when no admin exists.
	InitAdminUser string
	InitAdminPass string
}

// Load reads the config file (VEILON_CONFIG or DefaultFile, optional) and env.
func Load() (*Config, error) {
	file := os.Getenv("VEILON_CONFIG")
	if file == "" {
		file = DefaultFile
	}
	vals := map[string]string{}
	if f, err := os.Open(file); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			v = strings.Trim(v, `"'`)
			vals[strings.TrimSpace(k)] = v
		}
		f.Close()
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config %s: %w", file, err)
	}
	get := func(k, def string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		if v, ok := vals[k]; ok {
			return v
		}
		return def
	}
	c := &Config{File: file}
	c.DataDir = get("VEILON_DATA_DIR", "/var/lib/veilon")
	c.DBDriver = strings.ToLower(get("VEILON_DB_DRIVER", "postgres"))
	c.DBDSN = get("VEILON_DB_DSN", "")
	if c.DBDSN == "" {
		switch c.DBDriver {
		case "sqlite", "sqlite3":
			c.DBDSN = filepath.Join(c.DataDir, "veilon.db")
		default:
			c.DBDSN = "host=127.0.0.1 port=5432 user=veilon dbname=veilon sslmode=disable"
		}
	}
	if c.DBDriver == "sqlite3" {
		c.DBDriver = "sqlite"
	}
	if c.DBDriver != "postgres" && c.DBDriver != "sqlite" {
		return nil, fmt.Errorf("VEILON_DB_DRIVER must be postgres or sqlite, got %q", c.DBDriver)
	}
	c.Listen = get("VEILON_LISTEN", "0.0.0.0")
	c.XrayBin = get("VEILON_XRAY_BIN", filepath.Join(c.DataDir, "xray", "xray"))
	c.XrayAutoDownload = parseBool(get("VEILON_XRAY_AUTO_DOWNLOAD", "true"))
	c.XrayVersion = get("VEILON_XRAY_VERSION", "latest")
	c.SingboxBin = get("VEILON_SINGBOX_BIN", filepath.Join(c.DataDir, "sing-box", "sing-box"))
	c.SingboxVersion = get("VEILON_SINGBOX_VERSION", "1.14.4")
	c.MTGBin = get("VEILON_MTG_BIN", filepath.Join(c.DataDir, "mtg", "mtg-multi"))
	c.MTGVersion = get("VEILON_MTG_VERSION", "1.15.0")
	c.TrustProxy = parseBool(get("VEILON_TRUST_PROXY", "false"))
	c.ACMEDirectory = get("VEILON_ACME_DIRECTORY", "https://acme-v02.api.letsencrypt.org/directory")
	c.ACMECAFile = get("VEILON_ACME_CA_FILE", "")
	c.ACMEAllowPrivateIPs = parseBool(get("VEILON_ACME_ALLOW_PRIVATE_IPS", "false"))
	ttl, err := time.ParseDuration(get("VEILON_JWT_TTL", "24h"))
	if err != nil || ttl <= 0 {
		ttl = 24 * time.Hour
	}
	c.JWTTTL = ttl
	c.InitPort, _ = strconv.Atoi(get("VEILON_PANEL_PORT", "0"))
	c.InitPath = get("VEILON_PANEL_PATH", "")
	c.InitAdminUser = get("VEILON_ADMIN_USERNAME", "")
	c.InitAdminPass = get("VEILON_ADMIN_PASSWORD", "")
	return c, nil
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// EnsureDirs creates the data directory tree.
func (c *Config) EnsureDirs() error {
	for _, d := range []string{c.DataDir, filepath.Join(c.DataDir, "xray"), filepath.Join(c.DataDir, "certs"),
		filepath.Join(c.DataDir, "sing-box"), filepath.Join(c.DataDir, "mtg"), filepath.Join(c.DataDir, "amneziawg")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	return nil
}
