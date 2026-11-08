package db

import (
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Setting keys. Keys prefixed with "_" are internal and never exposed by the API.
const (
	KeyPanelPort      = "panel_port"
	KeyPanelPath      = "panel_path"
	KeyPanelDomain    = "panel_domain"
	KeyPanelCertID    = "panel_cert_id"
	KeySubPath        = "sub_path"
	KeySubDomain      = "sub_domain"
	KeySubTitle       = "sub_title"
	KeySubUpdateHours = "sub_update_hours"
	KeyServerAddress  = "server_address"
	KeyTimezone       = "timezone"
	KeyLanguage       = "language"
	KeyAdBlock        = "ad_block"
	KeyBlockTorrent   = "block_torrent"

	KeyJWTSecret   = "_jwt_secret"
	KeyXrayAPIPort = "_xray_api_port"
	// KeySingboxAPIPort is the loopback port where sing-box hands TUIC traffic
	// to Xray (bridge inbound).
	KeySingboxAPIPort = "_singbox_api_port"
	KeyACMEAccount    = "_acme_account_key"
	KeyTrafficUp      = "_traffic_total_up"
	KeyTrafficDown    = "_traffic_total_down"
	KeyInstalledAt    = "_installed_at"
)

// DefaultLanguage is the UI language used when none is stored.
const DefaultLanguage = "fa"

// Settings is the typed, public view of runtime settings.
type Settings struct {
	PanelPort      int    `json:"panel_port"`
	PanelPath      string `json:"panel_path"`
	PanelDomain    string `json:"panel_domain"`
	PanelCertID    *int64 `json:"panel_cert_id"`
	SubPath        string `json:"sub_path"`
	SubDomain      string `json:"sub_domain"`
	SubTitle       string `json:"sub_title"`
	SubUpdateHours int    `json:"sub_update_hours"`
	ServerAddress  string `json:"server_address"`
	Timezone       string `json:"timezone"`
	Language       string `json:"language"`
	// AdBlock blocks ad domains (geosite:category-ads-all) for every user.
	AdBlock bool `json:"ad_block"`
	// BlockTorrent blocks BitTorrent for every user (per-user otherwise).
	BlockTorrent bool `json:"block_torrent"`
}

// GetSetting returns a raw setting value ("" if missing).
func GetSetting(gdb *gorm.DB, key string) string {
	var s Setting
	if err := gdb.Where("key = ?", key).Limit(1).Find(&s).Error; err != nil {
		return ""
	}
	return s.Value
}

// SetSetting upserts a raw setting value.
func SetSetting(gdb *gorm.DB, key, value string) error {
	return gdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Setting{Key: key, Value: value}).Error
}

// SetDefault stores value only if key is missing. Returns the effective value.
func SetDefault(gdb *gorm.DB, key, value string) (string, error) {
	var s Setting
	err := gdb.Where("key = ?", key).Limit(1).Find(&s).Error
	if err != nil {
		return "", err
	}
	if s.Key != "" {
		return s.Value, nil
	}
	return value, SetSetting(gdb, key, value)
}

// AddCounter atomically increments an integer setting.
func AddCounter(gdb *gorm.DB, key string, delta int64) error {
	if delta == 0 {
		return nil
	}
	return gdb.Transaction(func(tx *gorm.DB) error {
		cur, _ := strconv.ParseInt(GetSetting(tx, key), 10, 64)
		return SetSetting(tx, key, strconv.FormatInt(cur+delta, 10))
	})
}

// LoadSettings reads all public settings.
func LoadSettings(gdb *gorm.DB) Settings {
	var rows []Setting
	gdb.Find(&rows)
	m := map[string]string{}
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	s := Settings{
		PanelPath:     m[KeyPanelPath],
		PanelDomain:   m[KeyPanelDomain],
		SubPath:       m[KeySubPath],
		SubDomain:     m[KeySubDomain],
		SubTitle:      m[KeySubTitle],
		ServerAddress: m[KeyServerAddress],
		Timezone:      m[KeyTimezone],
		Language:      m[KeyLanguage],
	}
	s.AdBlock = m[KeyAdBlock] == "true"
	s.BlockTorrent = m[KeyBlockTorrent] != "false" // on unless turned off
	s.PanelPort, _ = strconv.Atoi(m[KeyPanelPort])
	s.SubUpdateHours, _ = strconv.Atoi(m[KeySubUpdateHours])
	if v := strings.TrimSpace(m[KeyPanelCertID]); v != "" && v != "0" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.PanelCertID = &id
		}
	}
	if s.SubPath == "" {
		s.SubPath = "sub"
	}
	if s.SubUpdateHours <= 0 {
		s.SubUpdateHours = 12
	}
	if s.Language != "en" {
		s.Language = DefaultLanguage
	}
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	return s
}

// SaveSettings writes all public settings.
func SaveSettings(gdb *gorm.DB, s Settings) error {
	cert := ""
	if s.PanelCertID != nil {
		cert = strconv.FormatInt(*s.PanelCertID, 10)
	}
	kv := map[string]string{
		KeyPanelPort:      strconv.Itoa(s.PanelPort),
		KeyPanelPath:      s.PanelPath,
		KeyPanelDomain:    s.PanelDomain,
		KeyPanelCertID:    cert,
		KeySubPath:        s.SubPath,
		KeySubDomain:      s.SubDomain,
		KeySubTitle:       s.SubTitle,
		KeySubUpdateHours: strconv.Itoa(s.SubUpdateHours),
		KeyServerAddress:  s.ServerAddress,
		KeyTimezone:       s.Timezone,
		KeyLanguage:       s.Language,
		KeyAdBlock:        strconv.FormatBool(s.AdBlock),
		KeyBlockTorrent:   strconv.FormatBool(s.BlockTorrent),
	}
	return gdb.Transaction(func(tx *gorm.DB) error {
		for k, v := range kv {
			if err := SetSetting(tx, k, v); err != nil {
				return err
			}
		}
		return nil
	})
}
