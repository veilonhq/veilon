package main

// "veilon cert ...": certificate management through the running service's
// control socket (the service binds the HTTP-01 port, stores the
// certificate and hot-reloads the panel and inbounds).

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/config"
)

const certUsage = `Usage:
  veilon cert list
  veilon cert issue --domain D [--email E] [--http-port 80] [--panel] [-y]
  veilon cert issue --ip ADDR|auto [--ipv6 ADDR|auto] [--email E] [--http-port 80] [--panel] [-y]
  veilon cert renew <id>
  veilon cert revoke <id> [-y]
  veilon cert delete <id> [-y]
  veilon cert info <id>
  veilon cert panel <id|none>     serve the panel with this certificate (none = plain HTTP)

Domain certificates are valid 90 days and renew 30 days before expiry. IP
certificates use the short-lived profile (about 6 days) and renew every ~3
days. Validation is HTTP-01: port 80/tcp must be reachable from the internet.
`

func ctl() (ctlClient, error) {
	cfg, err := config.Load()
	if err != nil {
		return ctlClient{}, err
	}
	return ctlClient{path: ctlSocketPath(cfg.DataDir)}, nil
}

func cmdCert(args []string) error {
	if len(args) < 1 {
		fmt.Print(certUsage)
		return errors.New("missing cert command")
	}
	c, err := ctl()
	if err != nil {
		return err
	}
	rest := args[1:]
	switch args[0] {
	case "list", "ls":
		return certList(c)
	case "issue", "new":
		return certIssue(c, rest)
	case "renew":
		id, err := certID(rest)
		if err != nil {
			return err
		}
		stepf("renewing certificate #%d ...", id)
		var v certs.View
		if err := c.do("POST", fmt.Sprintf("/certs/%d/renew", id), nil, &v, 5*time.Minute); err != nil {
			return err
		}
		okf("renewed: valid until %s", fmtTime(v.NotAfter))
		return nil
	case "revoke", "delete", "rm":
		fs := flag.NewFlagSet("cert "+args[0], flag.ContinueOnError)
		yes := fs.Bool("y", false, "do not ask for confirmation")
		ids, err := parseInterspersed(fs, rest)
		if err != nil {
			return err
		}
		id, err := certID(ids)
		if err != nil {
			return err
		}
		verb := "Revoke"
		if args[0] != "revoke" {
			verb = "Delete"
		}
		if !*yes {
			if !interactive() {
				return fmt.Errorf("refusing to %s without -y in a non-interactive session", strings.ToLower(verb))
			}
			if !confirm(fmt.Sprintf("%s certificate #%d?", verb, id), false) {
				return errAborted
			}
		}
		if args[0] == "revoke" {
			err = c.do("POST", fmt.Sprintf("/certs/%d/revoke", id), nil, nil, 2*time.Minute)
		} else {
			err = c.do("DELETE", fmt.Sprintf("/certs/%d", id), nil, nil, 30*time.Second)
		}
		if err != nil {
			return err
		}
		if args[0] == "revoke" {
			okf("certificate #%d revoked at the CA and removed", id)
		} else {
			okf("certificate #%d deleted", id)
		}
		return nil
	case "info", "show":
		id, err := certID(rest)
		if err != nil {
			return err
		}
		return certInfo(c, id)
	case "panel":
		if len(rest) != 1 {
			return errors.New("usage: veilon cert panel <id|none>")
		}
		var id int64
		if rest[0] != "none" && rest[0] != "0" {
			if id, err = certID(rest); err != nil {
				return err
			}
		}
		if err := c.do("POST", "/panel/cert", map[string]int64{"id": id}, nil, 30*time.Second); err != nil {
			return err
		}
		time.Sleep(1500 * time.Millisecond) // the service restarts its listener after ~1s
		if id == 0 {
			okf("the panel now serves plain HTTP")
		} else {
			okf("the panel now uses certificate #%d (HTTPS)", id)
		}
		printPanelURL()
		return nil
	case "help", "-h", "--help":
		fmt.Print(certUsage)
		return nil
	}
	fmt.Print(certUsage)
	return fmt.Errorf("unknown cert command %q", args[0])
}

// parseInterspersed parses flags that may come before or after positionals.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func certID(args []string) (int64, error) {
	if len(args) != 1 {
		return 0, errors.New("a certificate id is required (see: veilon cert list)")
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid certificate id %q", args[0])
	}
	return id, nil
}

func fmtTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04 MST") }

func panelCertID(c ctlClient) int64 {
	var p struct {
		CertID *int64 `json:"cert_id"`
	}
	if c.do("GET", "/panel", nil, &p, 5*time.Second) == nil && p.CertID != nil {
		return *p.CertID
	}
	return 0
}

func certList(c ctlClient) error {
	var l []certs.View
	if err := c.do("GET", "/certs", nil, &l, 15*time.Second); err != nil {
		return err
	}
	if len(l) == 0 {
		fmt.Println("no certificates (issue one: veilon cert issue --domain example.com | --ip auto)")
		return nil
	}
	panel := panelCertID(c)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTYPE\tNAMES\tSOURCE\tEXPIRES\tNEXT RENEWAL\tUSED BY")
	for _, v := range l {
		renew := "-"
		if v.RenewAt != nil {
			renew = fmtTime(*v.RenewAt)
		}
		used := "-"
		switch {
		case v.ID == panel:
			used = "panel"
		case v.InUse:
			used = "inbound"
		}
		exp := fmtTime(v.NotAfter)
		if time.Until(v.NotAfter) < 0 {
			exp += " (expired)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", v.ID, v.Type, strings.Join(v.Identifiers, ", "), v.Source, exp, renew, used)
	}
	return tw.Flush()
}

func certInfo(c ctlClient, id int64) error {
	var l []certs.View
	if err := c.do("GET", "/certs", nil, &l, 15*time.Second); err != nil {
		return err
	}
	for _, v := range l {
		if v.ID != id {
			continue
		}
		row := func(k, val string) { fmt.Printf("%-14s %s\n", k+":", val) }
		row("ID", strconv.FormatInt(v.ID, 10))
		row("Type", v.Type)
		row("Names", strings.Join(v.Identifiers, ", "))
		row("Source", v.Source)
		row("Issuer", v.Issuer)
		if v.Profile != "" {
			row("Profile", v.Profile)
		}
		if v.NotBefore != nil {
			row("Valid from", fmtTime(*v.NotBefore))
		}
		left := time.Until(v.NotAfter).Round(time.Hour)
		row("Valid until", fmt.Sprintf("%s (%s left)", fmtTime(v.NotAfter), humanDuration(left)))
		row("Auto renew", strconv.FormatBool(v.AutoRenew))
		if v.RenewAt != nil {
			row("Next renewal", fmtTime(*v.RenewAt))
		}
		if v.HTTPPort != 0 {
			row("HTTP-01 port", strconv.Itoa(v.HTTPPort))
		}
		if v.LastAttemptAt != nil {
			row("Last attempt", fmtTime(*v.LastAttemptAt))
		}
		if v.LastError != "" {
			row("Last error", red(v.LastError))
		}
		used := "no"
		if v.ID == panelCertID(c) {
			used = "panel"
		} else if v.InUse {
			used = "inbound"
		}
		row("In use", used)
		return nil
	}
	return fmt.Errorf("certificate #%d not found", id)
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh", hours)
}

// certIssueOpts is a parsed "cert issue" request.
type certIssueOpts struct {
	domain, ip, ipv6, email string
	httpPort                int
	panel, yes              bool
}

func certIssue(c ctlClient, args []string) error {
	fs := flag.NewFlagSet("cert issue", flag.ContinueOnError)
	var o certIssueOpts
	fs.StringVar(&o.domain, "domain", "", "domain name (Let's Encrypt, 90 days)")
	fs.StringVar(&o.ip, "ip", "", "IPv4/IPv6 address or auto (Let's Encrypt short-lived, ~6 days)")
	fs.StringVar(&o.ipv6, "ipv6", "", "also cover this IPv6 address (or auto) in an IP certificate")
	fs.StringVar(&o.email, "email", "", "contact email for the ACME account (optional)")
	fs.IntVar(&o.httpPort, "http-port", 80, "local port for the HTTP-01 listener")
	fs.BoolVar(&o.panel, "panel", false, "serve the panel with the new certificate")
	fs.BoolVar(&o.yes, "y", false, "do not ask; use detected addresses")
	fs.BoolVar(&o.yes, "yes", false, "same as -y")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	return issueCert(c, o)
}

func issueCert(c ctlClient, o certIssueOpts) error {
	if (o.domain == "") == (o.ip == "" && o.ipv6 == "") {
		fmt.Print(certUsage)
		return errors.New("give either --domain or --ip")
	}
	ask := !o.yes && interactive()
	req := map[string]any{"email": o.email, "http_port": o.httpPort, "use_for_panel": o.panel}
	if o.domain != "" {
		req["type"], req["domain"] = "domain", o.domain
		stepf("requesting a Let's Encrypt certificate for %s (HTTP-01 on port %d)", bold(o.domain), o.httpPort)
	} else {
		ip4, ip6, err := resolveCertIPs(c, o.ip, o.ipv6, ask)
		if err != nil {
			return err
		}
		req["type"], req["ip"], req["ipv6"] = "ip", ip4, ip6
		names := ip4
		if ip6 != "" {
			names += ", " + ip6
		}
		stepf("requesting a short-lived IP certificate for %s (HTTP-01 on port %d)", bold(names), o.httpPort)
	}
	openACMEPort(o.httpPort)
	stepf("this can take up to a minute ...")
	var v certs.View
	if err := c.do("POST", "/certs/acme", req, &v, 5*time.Minute); err != nil {
		return err
	}
	okf("certificate #%d issued for %s, valid until %s", v.ID, strings.Join(v.Identifiers, ", "), fmtTime(v.NotAfter))
	if v.RenewAt != nil {
		fmt.Printf("  renews automatically around %s\n", fmtTime(*v.RenewAt))
	}
	if o.panel {
		okf("the panel now uses this certificate (HTTPS)")
		printPanelURL()
	}
	return nil
}

// resolveCertIPs turns --ip/--ipv6 values ("auto" or an address) into
// confirmed addresses. With a person at the terminal the detected
// candidates are shown and they confirm or type another address; detection
// can be wrong behind NAT.
func resolveCertIPs(c ctlClient, ip4, ip6 string, ask bool) (string, string, error) {
	needDetect := ip4 == "" || ip4 == "auto" || ip6 == "auto" || ask
	var det certs.PublicIPResult
	if needDetect {
		stepf("detecting the server's public IP ...")
		if err := c.do("GET", "/public-ip", nil, &det, 20*time.Second); err != nil {
			return "", "", err
		}
	}
	if ip4 == "" || ip4 == "auto" {
		ip4 = det.IPv4
		if ip4 == "" && ip6 == "" {
			ip4 = det.IPv6
		}
	}
	if ip6 == "auto" {
		ip6 = det.IPv6
		if ip6 == "" {
			warnf("no public IPv6 address was detected; issuing for IPv4 only")
		}
	}
	if ask {
		if len(det.Candidates) > 0 {
			fmt.Println("Detected public addresses:")
			for _, cand := range det.Candidates {
				fmt.Printf("  %s %s %s\n", bold(fmt.Sprintf("%-40s", cand.IP)), cand.Family, dim("("+strings.Join(cand.Sources, ", ")+")"))
			}
		} else {
			warnf("could not detect a public IP address")
		}
		ans, err := prompt("IP address for the certificate", ip4)
		if err != nil {
			return "", "", err
		}
		ip4 = ans
		if ip6 == "" && det.IPv6 != "" && net.ParseIP(ip4) != nil && net.ParseIP(ip4).To4() != nil {
			if confirm("Also include IPv6 "+det.IPv6+"?", false) {
				ip6 = det.IPv6
			}
		}
	}
	if ip4 == "" && ip6 == "" {
		return "", "", errors.New("could not detect the public IP; give it with --ip ADDR")
	}
	for _, v := range []string{ip4, ip6} {
		if v != "" && net.ParseIP(strings.Trim(v, "[]")) == nil {
			return "", "", fmt.Errorf("%q is not an IP address", v)
		}
	}
	return ip4, ip6, nil
}

// openACMEPort opens the HTTP-01 port in an active ufw (root only).
func openACMEPort(port int) {
	if os.Geteuid() != 0 || !ufwActive() {
		return
	}
	if exec.Command("ufw", "allow", fmt.Sprintf("%d/tcp", port)).Run() == nil {
		stepf("opened %d/tcp in ufw for validation and renewals", port)
	}
}

func printPanelURL() {
	out, err := selfOutput("info", "--url")
	if err == nil && len(out) > 0 {
		fmt.Printf("  panel: %s\n", bold(strings.TrimSpace(string(out))))
	}
}
