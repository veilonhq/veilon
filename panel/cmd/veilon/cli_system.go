package main

// System commands: service control, logs, BBR, firewall, geo files, update
// and uninstall.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/veilonhq/veilon/panel/internal/config"
	"github.com/veilonhq/veilon/panel/internal/version"
)

const (
	unitName      = "veilon"
	unitFile      = "/etc/systemd/system/veilon.service"
	confDir       = "/etc/veilon"
	credFile      = "/root/veilon-credentials.txt"
	defaultBin    = "/usr/local/bin/veilon"
	bbrSysctlFile = "/etc/sysctl.d/99-veilon-bbr.conf"
	installerURL  = "https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh"
)

func releaseRepo() string {
	if r := os.Getenv("VEILON_REPO"); r != "" {
		return r
	}
	return "veilonhq/veilon"
}

// cmdSystemctl runs start|stop|restart|enable|disable on the service.
func cmdSystemctl(action string) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if err := runAttached("systemctl", action, unitName); err != nil {
		return fmt.Errorf("systemctl %s %s: %w", action, unitName, err)
	}
	switch action {
	case "start", "restart":
		time.Sleep(time.Second)
		out, _ := exec.Command("systemctl", "is-active", unitName).Output()
		st := strings.TrimSpace(string(out))
		if st == "active" {
			okf("service %sed", action)
		} else {
			warnf("service state: %s (see: veilon logs)", st)
		}
	case "stop":
		okf("service stopped")
	case "enable":
		okf("autostart enabled")
	case "disable":
		okf("autostart disabled")
	}
	return nil
}

func cmdLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "follow")
	n := fs.Int("n", 100, "number of lines")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jargs := []string{"-u", unitName, "-n", fmt.Sprint(*n), "--no-pager", "-o", "short-iso"}
	if *follow {
		jargs = append(jargs, "-f")
	}
	return runAttached("journalctl", jargs...)
}

func sysctlGet(key string) string {
	out, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cmdBBR enables TCP BBR congestion control with the fq qdisc.
func cmdBBR(args []string) error {
	if len(args) > 0 && args[0] == "status" {
		fmt.Printf("congestion control: %s\nqdisc:              %s\navailable:          %s\n",
			sysctlGet("net.ipv4.tcp_congestion_control"), sysctlGet("net.core.default_qdisc"),
			sysctlGet("net.ipv4.tcp_available_congestion_control"))
		return nil
	}
	if sysctlGet("net.ipv4.tcp_congestion_control") == "bbr" && sysctlGet("net.core.default_qdisc") == "fq" {
		okf("BBR is already enabled")
		return nil
	}
	if err := requireRoot(); err != nil {
		return err
	}
	_ = exec.Command("modprobe", "tcp_bbr").Run()
	if !strings.Contains(sysctlGet("net.ipv4.tcp_available_congestion_control"), "bbr") {
		return errors.New("this kernel does not provide BBR (Linux 4.9+ with tcp_bbr needed)")
	}
	conf := "# Written by veilon bbr\nnet.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\n"
	if err := os.WriteFile(bbrSysctlFile, []byte(conf), 0o644); err != nil {
		return err
	}
	_ = os.WriteFile("/etc/modules-load.d/veilon-bbr.conf", []byte("tcp_bbr\n"), 0o644)
	if out, err := exec.Command("sysctl", "-p", bbrSysctlFile).CombinedOutput(); err != nil {
		return fmt.Errorf("sysctl: %s", strings.TrimSpace(string(out)))
	}
	if sysctlGet("net.ipv4.tcp_congestion_control") != "bbr" {
		return errors.New("BBR could not be activated (container or restricted kernel?)")
	}
	okf("BBR enabled (net.ipv4.tcp_congestion_control=bbr, qdisc fq); persists across reboots")
	return nil
}

func ufwActive() bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false
	}
	out, _ := exec.Command("ufw", "status").Output()
	return strings.Contains(string(out), "Status: active")
}

func firewalldActive() bool {
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return false
	}
	out, _ := exec.Command("firewall-cmd", "--state").Output()
	return strings.TrimSpace(string(out)) == "running"
}

// cmdFirewall opens the panel port, inbound ports and the ACME port in ufw
// or firewalld.
func cmdFirewall(args []string) error {
	fs := flag.NewFlagSet("firewall", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "only print what would be opened")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := selfOutput("ports", "--json")
	if err != nil {
		return fmt.Errorf("could not read the port list: %w", err)
	}
	var ports []portUse
	if err := json.Unmarshal(out, &ports); err != nil {
		return err
	}
	var tool string
	switch {
	case ufwActive():
		tool = "ufw"
	case firewalldActive():
		tool = "firewalld"
	}
	if tool == "" || *dry {
		if tool == "" {
			warnf("no active firewall found (ufw or firewalld); nothing to change")
			fmt.Println("Ports Veilon uses (open them in your provider's firewall / security group too):")
		} else {
			fmt.Printf("Would open in %s:\n", tool)
		}
		for _, p := range ports {
			fmt.Printf("  %5d/%s  %s\n", p.Port, p.Proto, p.What)
		}
		return nil
	}
	if err := requireRoot(); err != nil {
		return err
	}
	done := map[string]bool{}
	for _, p := range ports {
		spec := fmt.Sprintf("%d/%s", p.Port, p.Proto)
		if done[spec] {
			continue
		}
		done[spec] = true
		var cmd *exec.Cmd
		if tool == "ufw" {
			cmd = exec.Command("ufw", "allow", spec, "comment", "veilon "+p.What)
		} else {
			cmd = exec.Command("firewall-cmd", "--permanent", "--add-port="+spec)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			warnf("%s: %s", spec, strings.TrimSpace(string(out)))
			continue
		}
		okf("%-10s %s", spec, dim(p.What))
	}
	if tool == "firewalld" {
		if out, err := exec.Command("firewall-cmd", "--reload").CombinedOutput(); err != nil {
			return fmt.Errorf("firewall-cmd --reload: %s", strings.TrimSpace(string(out)))
		}
	}
	fmt.Println("Also allow these ports in your cloud provider's firewall / security group if it has one.")
	return nil
}

func cmdGeo(args []string) error {
	if len(args) == 0 || args[0] != "update" {
		return errors.New("usage: veilon geo update")
	}
	c, err := ctl()
	if err != nil {
		return err
	}
	stepf("downloading geoip.dat and geosite.dat ...")
	if err := c.do("POST", "/geo/update", nil, nil, 6*time.Minute); err != nil {
		return err
	}
	okf("geo files updated; the core was restarted")
	return nil
}

// cmdUpdate replaces the binary with a GitHub release (checksum verified)
// and restarts the service. Data and settings are kept.
func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	yes := fs.Bool("y", false, "do not ask")
	force := fs.Bool("force", false, "reinstall even if the version matches")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := requireRoot(); err != nil {
		return err
	}
	tag := "latest"
	if len(pos) > 0 && pos[0] != "latest" {
		tag = pos[0]
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if tag == "latest" {
		stepf("checking the latest release of %s ...", releaseRepo())
		t, err := latestTag(ctx)
		if err != nil {
			return err
		}
		tag = t
	}
	fmt.Printf("installed: %s   target: %s\n", bold(version.Version), bold(tag))
	if tag == version.Version && !*force {
		okf("already up to date")
		ensureUpdaterUnits()
		return nil
	}
	if !*yes && interactive() && !confirm("Update now? Data and settings are kept", true) {
		return errAborted
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(self); err == nil {
		self = p
	}
	asset := "veilon-linux-" + runtime.GOARCH
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s/", releaseRepo(), tag)
	stepf("downloading %s %s ...", asset, tag)
	tmp, err := os.CreateTemp(filepath.Dir(self), ".veilon-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if err := httpGet(ctx, base+asset, io.MultiWriter(tmp, h)); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	var sums strings.Builder
	if err := httpGet(ctx, base+"checksums.txt", &sums); err != nil {
		return fmt.Errorf("checksums.txt: %w (refusing to install an unverified binary)", err)
	}
	want := checksumFor(sums.String(), asset)
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", asset)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s)", asset, got, want)
	}
	okf("checksum verified")
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	out, err := exec.Command(tmp.Name(), "version").Output()
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run: %w", err)
	}
	stepf("new binary: %s", strings.TrimSpace(string(out)))
	if err := os.Rename(tmp.Name(), self); err != nil {
		return err
	}
	okf("installed %s", self)
	if _, err := os.Stat(unitFile); err == nil {
		// let the panel offer the next updates itself (new binary's CLI)
		if err := exec.Command(self, "self-update", "--install-units").Run(); err != nil {
			warnf("panel updates not enabled: %v", err)
		}
		if err := exec.Command("systemctl", "restart", unitName).Run(); err != nil {
			return fmt.Errorf("restart the service: %w", err)
		}
		okf("service restarted")
	}
	return nil
}

func httpGet(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "veilon/"+version.Version)
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// latestTag resolves the newest release tag through the releases/latest
// redirect (no API token or rate limit involved).
func latestTag(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/latest", releaseRepo())
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	cl := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", fmt.Errorf("no release found for %s", releaseRepo())
	}
	return loc[i+len("/tag/"):], nil
}

// cmdUninstall removes the service and binary; --purge also removes the
// configuration, data, database and service user.
func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also remove configuration, data, database and the service user")
	yes := fs.Bool("y", false, "do not ask for confirmation")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if err := requireRoot(); err != nil {
		return err
	}
	what := "the service and the binary (data is kept)"
	if *purge {
		what = red("the service, binary, configuration, ALL DATA and the database")
	}
	if !*yes {
		if !interactive() {
			return errors.New("refusing to uninstall non-interactively without -y")
		}
		if !confirm("This removes "+what+". Continue?", false) {
			return errAborted
		}
	}
	cfg, cerr := config.Load()
	_ = exec.Command("systemctl", "disable", "--now", unitName, "veilon-updater.path").Run()
	_ = os.Remove(unitFile)
	_ = os.Remove("/etc/systemd/system/veilon-updater.path")
	_ = os.Remove("/etc/systemd/system/veilon-updater.service")
	_ = exec.Command("systemctl", "daemon-reload").Run()
	self, _ := os.Executable()
	for _, p := range []string{self, defaultBin} {
		if p != "" {
			_ = os.Remove(p)
		}
	}
	okf("service and binary removed")
	if !*purge {
		fmt.Printf("  data kept in %s and %s (remove with: uninstall --purge)\n", dataDirOr(cfg), confDir)
		return nil
	}
	if cerr == nil && cfg.DBDriver == "postgres" {
		if _, err := exec.LookPath("psql"); err == nil {
			for _, q := range []string{"DROP DATABASE IF EXISTS veilon", "DROP ROLE IF EXISTS veilon"} {
				_ = exec.Command("runuser", "-u", "postgres", "--", "psql", "-q", "-c", q).Run()
			}
		}
	}
	for _, p := range []string{confDir, dataDirOr(cfg), credFile} {
		_ = os.RemoveAll(p)
	}
	_ = exec.Command("userdel", serviceUser).Run()
	okf("configuration, data and database removed")
	return nil
}

func dataDirOr(cfg *config.Config) string {
	if cfg != nil && cfg.DataDir != "" {
		return cfg.DataDir
	}
	return "/var/lib/veilon"
}

// checksumFor returns the SHA-256 for name from a sha256sum-style list.
func checksumFor(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

// ensureUpdaterUnits adds the in-panel updater units when the panel service
// is installed but the units are not (servers updated by an older CLI).
func ensureUpdaterUnits() {
	if _, err := os.Stat(unitFile); err != nil {
		return
	}
	if _, err := os.Stat("/etc/systemd/system/veilon-updater.path"); err == nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	if err := exec.Command(self, "self-update", "--install-units").Run(); err != nil {
		warnf("panel updates not enabled: %v", err)
		return
	}
	okf("panel updates enabled (veilon-updater.path)")
}
