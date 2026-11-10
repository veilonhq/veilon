package main

// Interactive management menu: "veilon" with no arguments, as root, on a
// terminal.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/version"
)

type menuItem struct {
	key   string
	label string
	run   func() error
}

func menuHeader() {
	state := "not installed"
	if out, err := exec.Command("systemctl", "is-active", unitName).Output(); err == nil || len(out) > 0 {
		state = strings.TrimSpace(string(out))
	}
	switch state {
	case "active":
		state = green("● running")
	case "inactive", "failed":
		state = red("● " + state)
	default:
		state = yellow("● " + state)
	}
	auto := ""
	if out, _ := exec.Command("systemctl", "is-enabled", unitName).Output(); strings.TrimSpace(string(out)) == "enabled" {
		auto = dim("  autostart on")
	}
	line := strings.Repeat("─", 46)
	fmt.Println()
	fmt.Println(cyan("╭" + line + "╮"))
	title := fmt.Sprintf("  Veilon %s", version.Version)
	fmt.Printf("%s%s%s%s\n", cyan("│"), bold(title), strings.Repeat(" ", max(0, 46-len([]rune(title)))), cyan("│"))
	fmt.Println(cyan("╰" + line + "╯"))
	fmt.Printf("  service: %s%s\n", state, auto)
	if out, err := selfOutputQuiet("info", "--url"); err == nil {
		fmt.Printf("  panel:   %s\n", bold(strings.TrimSpace(string(out))))
	}
	fmt.Println()
}

func selfOutputQuiet(args ...string) ([]byte, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.Command(self, args...).Output()
}

func printMenu(title string, items []menuItem) {
	if title != "" {
		fmt.Println(bold(title))
	}
	for _, it := range items {
		if it.key == "" {
			fmt.Println(dim("  " + it.label))
			continue
		}
		fmt.Printf("  %s  %s\n", cyan(fmt.Sprintf("%2s", it.key)), it.label)
	}
}

// runMenu shows items until 0 is chosen. header is printed before each round.
func runMenu(title string, header func(), items []menuItem, exitLabel string) error {
	for {
		if header != nil {
			header()
		}
		printMenu(title, append(items, menuItem{key: "0", label: exitLabel}))
		fmt.Println()
		choice, err := prompt("Choose", "")
		if err != nil {
			return nil
		}
		if choice == "0" || choice == "q" {
			return nil
		}
		if choice == "" {
			continue // Enter alone shows the menu again
		}
		var picked *menuItem
		for i := range items {
			if items[i].key != "" && items[i].run != nil && items[i].key == choice {
				picked = &items[i]
			}
		}
		if picked == nil {
			warnf("unknown option %q", choice)
			continue
		}
		fmt.Println()
		if err := picked.run(); err != nil {
			if errors.Is(err, errAborted) {
				fmt.Println(dim("cancelled"))
			} else {
				fmt.Fprintln(os.Stderr, red("✘ "+err.Error()))
			}
		}
		fmt.Println()
		if _, err := prompt(dim("Press Enter to continue"), ""); err != nil {
			return nil
		}
	}
}

func cmdMenu() error {
	items := []menuItem{
		{"", "── Install", nil},
		{"1", "Install / update", menuInstallUpdate},
		{"2", "Uninstall", menuUninstall},
		{"", "── Panel", nil},
		{"3", "Reset username and password", menuResetCredentials},
		{"4", "Reset panel path", menuResetPath},
		{"5", "Change panel port", menuChangePort},
		{"6", "Show settings and URL", func() error { return runSelf("info") }},
		{"", "── Service", nil},
		{"7", "Start", func() error { return cmdSystemctl("start") }},
		{"8", "Stop", func() error { return cmdSystemctl("stop") }},
		{"9", "Restart", func() error { return cmdSystemctl("restart") }},
		{"10", "Status", func() error { return runSelf("status") }},
		{"11", "Logs (Ctrl+C to return)", func() error { return cmdLogs([]string{"-f", "-n", "50"}) }},
		{"12", "Enable autostart", func() error { return cmdSystemctl("enable") }},
		{"13", "Disable autostart", func() error { return cmdSystemctl("disable") }},
		{"", "── Tools", nil},
		{"14", "SSL certificates", menuSSL},
		{"15", "Enable BBR", func() error { return cmdBBR(nil) }},
		{"16", "Firewall: open Veilon ports", func() error { return cmdFirewall(nil) }},
		{"17", "Update geo files", func() error { return cmdGeo([]string{"update"}) }},
		{"18", "Version", func() error {
			fmt.Printf("veilon %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildDate)
			return nil
		}},
	}
	return runMenu("", menuHeader, items, "Exit")
}

func menuInstallUpdate() error {
	if _, err := os.Stat(unitFile); err != nil {
		stepf("Veilon is not installed as a service; running the installer")
		return runAttached("bash", "-c", "bash <(curl -fsSL "+installerURL+")")
	}
	v, err := prompt("Version to install (Enter = latest)", "latest")
	if err != nil {
		return err
	}
	return cmdUpdate([]string{v})
}

func menuUninstall() error {
	purge := confirm("Also delete all data, settings and the database?", false)
	args := []string{}
	if purge {
		args = append(args, "--purge")
	}
	if err := cmdUninstall(args); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func menuResetCredentials() error {
	var info struct {
		Admin string `json:"admin"`
	}
	if out, err := selfOutputQuiet("info", "--json"); err == nil {
		_ = json.Unmarshal(out, &info)
	}
	user, err := prompt("New username", info.Admin)
	if err != nil {
		return err
	}
	pass, err := prompt("New password (Enter = random)", "")
	if err != nil {
		return err
	}
	args := []string{"admin", "reset-credentials", "--username", user}
	if pass != "" {
		args = append(args, "--password", pass)
	}
	return runSelf(args...)
}

func menuResetPath() error {
	p, err := prompt("New panel path (Enter = random)", "random")
	if err != nil {
		return err
	}
	if err := runSelf("set-path", p); err != nil {
		return err
	}
	printPanelURL()
	return nil
}

func menuChangePort() error {
	p, err := prompt("New panel port 1-65535 (Enter = random)", "")
	if err != nil {
		return err
	}
	if p == "" {
		p = strconv.Itoa(randPort(10000, 60000))
	}
	if err := runSelf("set-port", p); err != nil {
		return err
	}
	printPanelURL()
	if (ufwActive() || firewalldActive()) && confirm("Open the new port in the firewall?", true) {
		return cmdFirewall(nil)
	}
	return nil
}

func menuSSL() error {
	c, err := ctl()
	if err != nil {
		return err
	}
	items := []menuItem{
		{"1", "Issue certificate for a domain (Let's Encrypt, 90 days)", func() error { return menuIssue(c, false) }},
		{"2", "Issue certificate for the server IP (Let's Encrypt, ~6 days, auto renew)", func() error { return menuIssue(c, true) }},
		{"3", "List certificates", func() error { return certList(c) }},
		{"4", "Renew a certificate", func() error { return menuCertAction(c, "renew") }},
		{"5", "Certificate details", func() error { return menuCertAction(c, "info") }},
		{"6", "Set the panel certificate", func() error { return menuCertAction(c, "panel") }},
		{"7", "Revoke a certificate", func() error { return menuCertAction(c, "revoke") }},
	}
	header := func() { fmt.Println(); fmt.Println(bold("SSL certificates")) }
	return runMenu("", header, items, "Back")
}

func menuIssue(c ctlClient, ip bool) error {
	o := certIssueOpts{httpPort: 80}
	if ip {
		o.ip = "auto"
	} else {
		d, err := prompt("Domain (its DNS A/AAAA record must point to this server)", "")
		if err != nil {
			return err
		}
		if d == "" {
			return errAborted
		}
		o.domain = d
	}
	o.panel = confirm("Use it for the panel (HTTPS)?", true)
	fmt.Println(dim("Port 80/tcp must be reachable from the internet for validation."))
	return issueCert(c, o)
}

func menuCertAction(c ctlClient, action string) error {
	if err := certList(c); err != nil {
		return err
	}
	hint := "Certificate ID"
	if action == "panel" {
		hint = "Certificate ID for the panel (none = plain HTTP)"
	}
	id, err := prompt(hint, "")
	if err != nil || id == "" {
		return errAborted
	}
	return cmdCert([]string{action, id})
}
