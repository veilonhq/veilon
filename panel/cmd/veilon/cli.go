package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/config"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/version"
)

func cmdAdmin(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: veilon admin create|reset-password|reset-credentials --username U [--password P]")
	}
	if args[0] == "reset-credentials" {
		return cmdAdminResetCredentials(args[1:])
	}
	fs := flag.NewFlagSet("admin "+args[0], flag.ContinueOnError)
	username := fs.String("username", "", "admin username")
	password := fs.String("password", "", "password (random if empty)")
	sudo := fs.Bool("sudo", true, "grant sudo (create only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("--username is required")
	}
	_, gdb, err := openDB()
	if err != nil {
		return err
	}
	pw := *password
	generated := false
	if pw == "" {
		pw, generated = util.RandString(16), true
	}
	switch args[0] {
	case "create":
		if _, err := auth.CreateAdmin(gdb, *username, pw, *sudo); err != nil {
			return err
		}
		fmt.Printf("admin %q created\n", *username)
	case "reset-password":
		var a db.Admin
		if err := gdb.Where("username = ?", *username).First(&a).Error; err != nil {
			return fmt.Errorf("admin %q not found", *username)
		}
		if err := auth.ValidatePassword(pw); err != nil {
			return err
		}
		if err := auth.SetPassword(gdb, &a, pw); err != nil {
			return err
		}
		fmt.Printf("password for %q updated\n", *username)
	default:
		return fmt.Errorf("unknown admin command %q", args[0])
	}
	if generated {
		fmt.Printf("password: %s\n", pw)
	}
	return nil
}

// cmdAdminResetCredentials changes the username and/or password of an admin
// (default: the first sudo admin). Without --password a random one is set.
func cmdAdminResetCredentials(args []string) error {
	fs := flag.NewFlagSet("admin reset-credentials", flag.ContinueOnError)
	target := fs.String("current", "", "admin to change (default: first sudo admin)")
	username := fs.String("username", "", "new username (default: keep)")
	password := fs.String("password", "", "new password (random if empty)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, gdb, err := openDB()
	if err != nil {
		return err
	}
	var a db.Admin
	q := gdb.Order("is_sudo DESC, id")
	if *target != "" {
		q = gdb.Where("username = ?", *target)
	}
	if err := q.First(&a).Error; err != nil {
		if *target != "" {
			return fmt.Errorf("admin %q not found", *target)
		}
		// No admin yet: create one.
		name := *username
		if name == "" {
			name = "admin"
		}
		pw := *password
		if pw == "" {
			pw = util.RandString(16)
		}
		if _, err := auth.CreateAdmin(gdb, name, pw, true); err != nil {
			return err
		}
		fmt.Printf("username: %s\npassword: %s\n", name, pw)
		return nil
	}
	pw := *password
	if pw == "" {
		pw = util.RandString(16)
	}
	if err := auth.ValidatePassword(pw); err != nil {
		return err
	}
	if *username != "" && *username != a.Username {
		if err := auth.RenameAdmin(gdb, &a, *username); err != nil {
			return err
		}
	}
	if err := auth.SetPassword(gdb, &a, pw); err != nil {
		return err
	}
	fmt.Printf("username: %s\npassword: %s\n", a.Username, pw)
	return nil
}

func cmdSetPort(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: veilon set-port <port>")
	}
	p, err := strconv.Atoi(args[0])
	if err != nil || p < 1 || p > 65535 {
		return errors.New("port must be 1-65535")
	}
	cfg, gdb, err := openDB()
	if err != nil {
		return err
	}
	var n int64
	gdb.Model(&db.Inbound{}).Where("port = ?", p).Count(&n)
	if n > 0 || db.GetSetting(gdb, db.KeyXrayAPIPort) == args[0] || db.GetSetting(gdb, db.KeySingboxAPIPort) == args[0] {
		return fmt.Errorf("port %d is used by an inbound or the core API", p)
	}
	if err := db.SetSetting(gdb, db.KeyPanelPort, strconv.Itoa(p)); err != nil {
		return err
	}
	fmt.Printf("panel port set to %d\n", p)
	reloadService(cfg)
	return nil
}

func cmdSetPath(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: veilon set-path <path|random>")
	}
	cfg, gdb, err := openDB()
	if err != nil {
		return err
	}
	st := db.LoadSettings(gdb)
	st.PanelPath = strings.Trim(args[0], "/")
	if st.PanelPath == "random" {
		st.PanelPath = randPath()
	}
	if len(st.PanelPath) < 2 || len(st.PanelPath) > 64 || strings.Trim(st.PanelPath, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		return errors.New("path must be 2-64 chars of letters, digits, _ or -")
	}
	if st.PanelPath == st.SubPath {
		return errors.New("panel path must differ from the subscription path")
	}
	if err := db.SetSetting(gdb, db.KeyPanelPath, st.PanelPath); err != nil {
		return err
	}
	fmt.Printf("panel path set to /%s/\n", st.PanelPath)
	reloadService(cfg)
	return nil
}

func cmdSetAddress(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: veilon set-address <host-or-ip>")
	}
	cfg, gdb, err := openDB()
	if err != nil {
		return err
	}
	if err := db.SetSetting(gdb, db.KeyServerAddress, strings.TrimSpace(args[0])); err != nil {
		return err
	}
	reloadService(cfg)
	return nil
}

func cmdSetLanguage(args []string) error {
	if len(args) != 1 || (args[0] != "fa" && args[0] != "en") {
		return errors.New("usage: veilon set-language fa|en")
	}
	cfg, gdb, err := openDB()
	if err != nil {
		return err
	}
	if err := db.SetSetting(gdb, db.KeyLanguage, args[0]); err != nil {
		return err
	}
	reloadService(cfg)
	return nil
}

// reloadService asks a running service to reload settings and restart the
// panel listener. It reports whether the change is live.
func reloadService(cfg *config.Config) bool {
	c := ctlClient{path: ctlSocketPath(cfg.DataDir)}
	if err := c.do("POST", "/reload", nil, nil, 10*time.Second); err != nil {
		if errors.Is(err, errServiceDown) {
			fmt.Println("the service is not running; the change applies when it starts")
		} else {
			fmt.Println("could not reach the service:", err, "- restart it: veilon restart")
		}
		return false
	}
	fmt.Println("applied to the running service")
	return true
}

func primaryIP() string {
	c, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

func panelURL(st db.Settings) string {
	host := st.PanelDomain
	if host == "" {
		host = st.ServerAddress
	}
	if host == "" {
		host = primaryIP()
	}
	scheme := "http"
	if st.PanelCertID != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s/", scheme, net.JoinHostPort(host, strconv.Itoa(st.PanelPort)), st.PanelPath)
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	onlyURL := fs.Bool("url", false, "print only the panel URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, gdb, err := openDB()
	if err != nil {
		return err
	}
	st := db.LoadSettings(gdb)
	u := panelURL(st)
	if *onlyURL {
		fmt.Println(u)
		return nil
	}
	if *asJSON {
		var admin db.Admin
		gdb.Order("is_sudo DESC, id").Limit(1).Find(&admin)
		info := map[string]any{
			"url": u, "port": st.PanelPort, "path": "/" + st.PanelPath + "/", "sub_path": "/" + st.SubPath + "/",
			"version": version.Version, "https": st.PanelCertID != nil, "panel_domain": st.PanelDomain,
			"server_address": st.ServerAddress, "admin": admin.Username, "panel_cert_id": st.PanelCertID,
		}
		b, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("Panel URL:  %s\nPanel port: %d\nPanel path: /%s/\nAPI base:   %sapi/v1\nSub path:   /%s/<token>\n", u, st.PanelPort, st.PanelPath, u, st.SubPath)
	if st.PanelCertID != nil {
		var c db.Cert
		if gdb.First(&c, *st.PanelCertID).Error == nil {
			fmt.Printf("Panel TLS:  certificate #%d (%s), expires %s\n", c.ID, c.Domain, c.NotAfter.Format("2006-01-02 15:04 MST"))
		}
	} else {
		fmt.Println("Panel TLS:  none (plain HTTP)")
	}
	return nil
}

// portUse is a port the firewall helper should open.
type portUse struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"` // tcp | udp
	What  string `json:"what"`
}

// udpProtocols are inbound protocols that carry traffic over UDP.
var udpProtocols = map[string]bool{"wireguard": true, "amneziawg": true, "hysteria": true, "hysteria2": true, "tuic": true, "masque": true}

// cmdPorts prints the ports Veilon listens on (panel, inbounds, ACME http-01).
func cmdPorts(args []string) error {
	fs := flag.NewFlagSet("ports", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, gdb, err := openDB()
	if err != nil {
		return err
	}
	st := db.LoadSettings(gdb)
	out := []portUse{{st.PanelPort, "tcp", "panel"}}
	var ibs []db.Inbound
	gdb.Where("enabled = ?", true).Order("port").Find(&ibs)
	for _, ib := range ibs {
		what := "inbound " + ib.Remark
		if udpProtocols[ib.Protocol] {
			out = append(out, portUse{ib.Port, "udp", what})
			continue
		}
		out = append(out, portUse{ib.Port, "tcp", what})
		if ib.Protocol == "shadowsocks" || ib.Protocol == "mixed" || ib.Network == "kcp" || ib.Network == "quic" {
			out = append(out, portUse{ib.Port, "udp", what})
		}
	}
	var certs []db.Cert
	gdb.Where("source = ? AND auto_renew = ?", "acme", true).Find(&certs)
	seen := map[int]bool{}
	for _, c := range certs {
		p := c.HTTPPort
		if p == 0 {
			p = 80
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, portUse{p, "tcp", "ACME http-01 (certificate renewal)"})
		}
	}
	if *asJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	for _, p := range out {
		fmt.Printf("%5d/%s  %s\n", p.Port, p.Proto, p.What)
	}
	return nil
}

func cmdStatus(args []string) error {
	cfg, gdb, err := openDB()
	if err != nil {
		fmt.Println("database:   ERROR", err)
		return err
	}
	st := db.LoadSettings(gdb)
	var users, inbounds, admins int64
	gdb.Model(&db.User{}).Count(&users)
	gdb.Model(&db.Inbound{}).Count(&inbounds)
	gdb.Model(&db.Admin{}).Count(&admins)
	fmt.Printf("version:    %s\n", version.Version)
	fmt.Printf("database:   ok (%s, schema v%d)\n", cfg.DBDriver, db.SchemaVersion(gdb))
	fmt.Printf("admins:     %d\nusers:      %d\ninbounds:   %d\n", admins, users, inbounds)
	if out, err := exec.Command("systemctl", "is-active", "veilon").Output(); err == nil || len(out) > 0 {
		fmt.Printf("service:    %s\n", strings.TrimSpace(string(out)))
	}
	scheme := "http"
	if st.PanelCertID != nil {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://127.0.0.1:%d/%s/api/v1/health", scheme, st.PanelPort, st.PanelPath)
	cl := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // local health probe
	if resp, err := cl.Get(url); err == nil {
		resp.Body.Close()
		fmt.Printf("panel:      responding on port %d (HTTP %d)\n", st.PanelPort, resp.StatusCode)
	} else {
		fmt.Printf("panel:      not responding on port %d\n", st.PanelPort)
	}
	if _, err := exec.LookPath(cfg.XrayBin); err == nil {
		out, _ := exec.Command(cfg.XrayBin, "version").Output()
		first, _, _ := strings.Cut(string(out), "\n")
		fmt.Printf("xray:       %s\n", strings.TrimSpace(first))
	} else {
		fmt.Printf("xray:       binary not installed at %s (downloaded on first serve)\n", cfg.XrayBin)
	}
	if exec.Command("pgrep", "-f", cfg.XrayBin+" run").Run() == nil {
		fmt.Println("core:       running")
	} else {
		fmt.Println("core:       not running")
	}
	return nil
}
