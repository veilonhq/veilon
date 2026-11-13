package main

// "veilon node ...": the agent that runs on a remote server and serves the
// inbounds the master assigns to it.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/cores"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/nodes"
	"github.com/veilonhq/veilon/panel/internal/porthop"
	"github.com/veilonhq/veilon/panel/internal/version"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

const (
	nodeDir      = "/var/lib/veilon-node"
	nodeUnit     = "veilon-node"
	nodeUnitFile = "/etc/systemd/system/veilon-node.service"
)

const nodeUsage = `Usage:
  veilon node install --token TOKEN [--dir DIR]   set up this server as a node (root)
  veilon node serve [--dir DIR] [--listen ADDR]   run the node agent (what the unit runs)
  veilon node status [--dir DIR]                  show the node's service and enrollment
  veilon node uninstall [--purge]                 remove the node service (root)
  veilon node apply-update [--dir DIR] [--watch]  install a staged agent update (started by the agent)

The token comes from the panel: Nodes > Add node (or "New install command").
`

func cmdNode(args []string) error {
	if len(args) == 0 {
		fmt.Print(nodeUsage)
		return nil
	}
	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	dir := fs.String("dir", envOr("VEILON_NODE_DIR", nodeDir), "state directory")
	token := fs.String("token", os.Getenv("VEILON_NODE_TOKEN"), "enrollment token from the panel")
	listen := fs.String("listen", "", "listen address (default :<port from the token>)")
	purge := fs.Bool("purge", false, "with uninstall: also remove the state directory")
	watch := fs.Bool("watch", false, "with apply-update: only wait for the new agent's confirmation")
	if _, err := parseInterspersed(fs, args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		return nodeServe(*dir, *token, *listen)
	case "install":
		return nodeInstall(*dir, *token)
	case "status":
		return nodeStatus(*dir)
	case "uninstall":
		return nodeUninstall(*dir, *purge)
	case "apply-update":
		return nodeApplyUpdate(*dir, *watch)
	}
	return fmt.Errorf("unknown node command %q\n\n%s", args[0], nodeUsage)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func enrollmentPath(dir string) string { return filepath.Join(dir, "enrollment.json") }

func loadEnrollment(dir, token string) (*nodes.Enrollment, error) {
	if token != "" {
		return nodes.ParseToken(token)
	}
	b, err := os.ReadFile(enrollmentPath(dir))
	if err != nil {
		return nil, fmt.Errorf("this server is not enrolled as a node yet (run: veilon node install --token TOKEN): %w", err)
	}
	var e nodes.Enrollment
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func saveEnrollment(dir string, e *nodes.Enrollment) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(e, "", "  ")
	tmp := enrollmentPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, enrollmentPath(dir))
}

func nodeXrayDir(dir string) string { return filepath.Join(dir, "xray") }

func ensureNodeXray(ctx context.Context, dir string) error {
	bin := filepath.Join(nodeXrayDir(dir), "xray")
	if _, err := os.Stat(bin); err == nil {
		return nil
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return xray.Download(dctx, nodeXrayDir(dir), envOr("VEILON_XRAY_VERSION", "latest"))
}

func nodeServe(dir, token, listen string) error {
	e, err := loadEnrollment(dir, token)
	if err != nil {
		return err
	}
	if token != "" {
		if err := saveEnrollment(dir, e); err != nil {
			return err
		}
	}
	if listen == "" {
		listen = ":" + strconv.Itoa(e.Port)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := ensureNodeXray(ctx, dir); err != nil {
		slog.Warn("xray is not installed yet; the node reports it until it is", "err", err)
	}
	apiPort := nodes.DefaultAPIPort
	if v, err := strconv.Atoi(os.Getenv("VEILON_NODE_API_PORT")); err == nil && v > 0 {
		apiPort = v
	}
	ips := &guard.IPCollector{}
	shaper := guard.New(nil, "") // speed limits from the master, per connection of this node
	onLine := func(line string) { ips.ObserveLine(line); shaper.ObserveConnLine(line) }
	core := &nodes.ProcCore{Bin: filepath.Join(nodeXrayDir(dir), "xray"), Dir: dir, APIPort: apiPort, OnLine: onLine}
	self, _ := os.Executable()
	engines := &cores.NodeEngines{Dir: dir, Self: self, MTGBin: filepath.Join(dir, "mtg", "mtg-multi"),
		MTGVersion: envOr("VEILON_MTG_VERSION", cores.MTGVersion), AutoDownload: os.Getenv("VEILON_NODE_AUTO_DOWNLOAD") != "false",
		AWGPeers: shaper.SetAWGPeers}
	go shaper.RunAWGLoop(ctx)
	a := &nodes.Agent{Enrollment: e, Dir: dir, Core: core, APIPort: apiPort, Version: version.Version,
		IPs: ips, Firewall: &guard.NodeFirewall{}, Shaper: shaper, Updater: nodeUpdater(dir), Engines: engines,
		Hop: &porthop.Applier{}}
	defer shaper.ClearShapes()
	defer func() { _ = a.Hop.Clear() }()
	a.Updater.Resume()
	a.Restore(ctx)
	return a.Serve(ctx, listen)
}

func nodeInstall(dir, token string) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if token == "" {
		return errors.New("--token is required (copy the command from the panel: Nodes > Add node)")
	}
	e, err := nodes.ParseToken(token)
	if err != nil {
		return err
	}
	if err := saveEnrollment(dir, e); err != nil {
		return err
	}
	okf("enrolled as node %d", e.ID)
	if err := ensureNodeXray(context.Background(), dir); err != nil {
		return fmt.Errorf("download xray: %w", err)
	}
	okf("xray installed in %s", nodeXrayDir(dir))
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(bin); err == nil {
		bin = p
	}
	unit := fmt.Sprintf(`[Unit]
Description=Veilon node
Documentation=https://github.com/veilonhq/veilon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=VEILON_NODE_DIR=%s
ExecStart=%s node serve
Restart=always
RestartSec=5
LimitNOFILE=1048576
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, dir, bin, dir)
	if err := os.WriteFile(nodeUnitFile, []byte(unit), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	if err := exec.Command("systemctl", "enable", "veilon-node").Run(); err != nil {
		return fmt.Errorf("enable %s: %w", nodeUnit, err)
	}
	if err := exec.Command("systemctl", "restart", "veilon-node").Run(); err != nil {
		return fmt.Errorf("start %s: %w", nodeUnit, err)
	}
	openNodePort(e.Port)
	okf("node service running on port %d; the panel connects to it within a few seconds", e.Port)
	return nil
}

// openNodePort allows the agent port in an active ufw or firewalld.
func openNodePort(port int) {
	p := strconv.Itoa(port) + "/tcp"
	if out, err := exec.Command("ufw", "status").Output(); err == nil && len(out) > 0 && containsActive(string(out)) {
		_ = exec.Command("ufw", "allow", p).Run()
		return
	}
	if out, err := exec.Command("firewall-cmd", "--state").Output(); err == nil && string(out) == "running\n" {
		_ = exec.Command("firewall-cmd", "--permanent", "--add-port="+p).Run()
		_ = exec.Command("firewall-cmd", "--reload").Run()
	}
}

func containsActive(s string) bool {
	return len(s) >= 14 && s[:14] == "Status: active"
}

func nodeStatus(dir string) error {
	e, err := loadEnrollment(dir, "")
	if err != nil {
		return err
	}
	state, _ := exec.Command("systemctl", "is-active", nodeUnit).Output()
	fmt.Printf("node id:  %d\nport:     %d\nservice:  %s", e.ID, e.Port, string(state))
	if len(state) == 0 {
		fmt.Println()
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(e.Port)), 2*time.Second)
	if err == nil {
		c.Close()
		fmt.Println("listening: yes")
	} else {
		fmt.Println("listening: no")
	}
	return nil
}

func nodeUninstall(dir string, purge bool) error {
	if err := requireRoot(); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "disable", "--now", nodeUnit).Run()
	_ = os.Remove(nodeUnitFile)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	okf("node service removed")
	if purge {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		okf("removed %s", dir)
		// the binary goes too, unless this server also runs the panel
		if _, err := os.Stat(unitFile); os.IsNotExist(err) {
			self, _ := os.Executable()
			for _, p := range []string{self, defaultBin} {
				if p != "" {
					_ = os.Remove(p)
				}
			}
			okf("removed the veilon binary")
		}
	}
	return nil
}

// nodeUpdater is the agent's self-updater. The helper must run outside the
// agent's unit (which cannot write the binary and is stopped by the
// restart): under systemd it is a transient unit (systemd-run); otherwise
// (development, tests with VEILON_NODE_RESTART_CMD) a detached process.
func nodeUpdater(dir string) *nodes.SelfUpdater {
	bin := os.Getenv("VEILON_NODE_BIN")
	if bin == "" {
		bin, _ = os.Executable()
		if p, err := filepath.EvalSymlinks(bin); err == nil {
			bin = p
		}
	}
	u := &nodes.SelfUpdater{Dir: dir, BinPath: bin, Version: version.Version, RestartCmd: os.Getenv("VEILON_NODE_RESTART_CMD")}
	if d, err := time.ParseDuration(os.Getenv("VEILON_NODE_UPDATE_TIMEOUT")); err == nil && d >= 10*time.Second {
		u.Timeout = d
	}
	_, sdErr := os.Stat("/run/systemd/system")
	sdRun, lookErr := exec.LookPath("systemd-run")
	switch {
	case u.RestartCmd == "" && sdErr == nil && lookErr == nil:
		u.Launch = func(exe string, args []string) error {
			unit := fmt.Sprintf("veilon-node-update-%d", time.Now().Unix())
			cmd := exec.Command(sdRun, append([]string{"--unit=" + unit, "--collect", "--quiet", "--description=Veilon node update", exe}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("systemd-run: %v: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	case u.RestartCmd != "":
		u.Launch = func(exe string, args []string) error {
			if err := os.MkdirAll(filepath.Join(dir, "update"), 0o700); err != nil {
				return err
			}
			logf, err := os.OpenFile(filepath.Join(dir, "update", "helper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			defer logf.Close()
			cmd := exec.Command(exe, args...)
			cmd.Stdout, cmd.Stderr = logf, logf
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := cmd.Start(); err != nil {
				return err
			}
			return cmd.Process.Release()
		}
	}
	return u
}

func nodeApplyUpdate(dir string, watchOnly bool) error {
	h := &nodes.Helper{Dir: dir}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if watchOnly {
		return h.Watch(ctx)
	}
	return h.Apply(ctx)
}
