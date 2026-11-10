package main

// "veilon self-update": the privileged half of the in-panel update.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/config"
	"github.com/veilonhq/veilon/panel/internal/selfupdate"
	"github.com/veilonhq/veilon/panel/internal/version"
)

func cmdSelfUpdate(args []string) error {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	run := fs.Bool("run", false, "perform the update the panel requested")
	units := fs.Bool("install-units", false, "install the systemd units that let the panel request updates")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	switch {
	case *units:
		return installUpdaterUnits()
	case *run:
		return runSelfUpdate()
	}
	return errors.New("usage: veilon self-update --run | --install-units")
}

func runSelfUpdate() error {
	if os.Geteuid() != 0 && os.Getenv("VEILON_UPDATER_SPAWN") != "1" {
		return errors.New("the updater must run as root")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	bin := os.Getenv("VEILON_UPDATE_BIN")
	if bin == "" {
		if bin, err = os.Executable(); err != nil {
			return err
		}
		if p, err := filepath.EvalSymlinks(bin); err == nil {
			bin = p
		}
	}
	backups := os.Getenv("VEILON_BACKUP_DIR")
	if backups == "" {
		backups = "/root/backups"
	}
	u := &selfupdate.Updater{
		DataDir: cfg.DataDir, BinPath: bin, BackupDir: backups, DBDriver: cfg.DBDriver, DBDSN: cfg.DBDSN,
		Current: version.Version,
		Restart: func(ctx context.Context) error {
			if c := os.Getenv("VEILON_RESTART_CMD"); c != "" {
				return exec.CommandContext(ctx, "/bin/sh", "-c", c).Run()
			}
			return exec.CommandContext(ctx, "systemctl", "restart", unitName).Run()
		},
	}
	if v := os.Getenv("VEILON_UPDATE_HEALTH_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			u.HealthTimeout = d
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	return u.RunRequest(ctx)
}

// installUpdaterUnits writes veilon-updater.path/.service: the panel (an
// unprivileged service) writes an update request into its data directory
// and systemd runs the updater as root.
func installUpdaterUnits() error {
	if err := requireRoot(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(bin); err == nil {
		bin = p
	}
	conf := os.Getenv("VEILON_CONFIG")
	if conf == "" {
		conf = confDir + "/veilon.env"
	}
	dir := selfupdate.Dir(cfg.DataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// the panel runs as the service user and must be able to write requests
	if fi, err := os.Stat(cfg.DataDir); err == nil {
		chownTo(dir, fi)
	}
	pathUnit := fmt.Sprintf(`[Unit]
Description=Veilon panel update requests

[Path]
PathChanged=%s
Unit=veilon-updater.service

[Install]
WantedBy=multi-user.target
`, selfupdate.RequestPath(cfg.DataDir))
	svcUnit := fmt.Sprintf(`[Unit]
Description=Veilon panel updater (download, verify, back up, swap, restart, roll back)

[Service]
Type=oneshot
Environment=VEILON_CONFIG=%s
ExecStart=%s self-update --run
TimeoutStartSec=20min
`, conf, bin)
	if err := os.WriteFile(selfupdate.UpdaterServiceUnit, []byte(svcUnit), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(selfupdate.UpdaterPathUnit, []byte(pathUnit), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	if err := exec.Command("systemctl", "enable", "--now", "veilon-updater.path").Run(); err != nil {
		return fmt.Errorf("enable veilon-updater.path: %w", err)
	}
	okf("panel updates enabled (veilon-updater.path)")
	return nil
}

func chownTo(path string, like os.FileInfo) {
	if st, ok := like.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(path, int(st.Uid), int(st.Gid))
	}
}
