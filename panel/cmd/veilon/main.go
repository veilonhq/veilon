// Command veilon is the Veilon proxy panel: API, web UI, subscription server
// and Xray-core supervisor in one binary.
package main

import (
	"errors"
	"fmt"
	"os"
	_ "time/tzdata" // IANA zones even on hosts without /usr/share/zoneinfo

	"github.com/veilonhq/veilon/panel/internal/awgd"
	"github.com/veilonhq/veilon/panel/internal/version"
)

const usage = `Veilon proxy panel

Usage:
  veilon                                management menu (as root, on a terminal)
  veilon serve                          run the panel (what the systemd unit runs)

Panel:
  veilon info [--json|--url]            panel URL, port, path and TLS
  veilon admin create --username U [--password P] [--sudo=true]
  veilon admin reset-password --username U [--password P]
  veilon admin reset-credentials [--current U] [--username NEW] [--password P]
  veilon set-port <port>                change the panel port (applied live)
  veilon set-path <path|random>         change the panel path (applied live)
  veilon set-address <host>             public server address used in links
  veilon set-language fa|en             default panel language (Persian unless changed)
  veilon ports [--json]                 ports Veilon listens on

Certificates (Let's Encrypt, HTTP-01 on port 80):
  veilon cert list
  veilon cert issue --domain D [--email E] [--http-port 80] [--panel] [-y]
  veilon cert issue --ip ADDR|auto [--ipv6 ADDR|auto] [--email E] [--http-port 80] [--panel] [-y]
  veilon cert renew|revoke|delete|info <id>
  veilon cert panel <id|none>

Service and system (root):
  veilon start|stop|restart|enable|disable
  veilon status                         service, database and core status
  veilon logs [-f] [-n N]
  veilon update [version] [-y]          install a release (checksum verified, data kept)
  veilon uninstall [--purge] [-y]
  veilon bbr [status]                   enable TCP BBR
  veilon firewall [--dry-run]           open Veilon ports in ufw or firewalld
  veilon geo update                     download fresh geoip.dat / geosite.dat
  veilon version

Nodes (run on a remote server; the token comes from the panel's Nodes page):
  veilon node install --token TOKEN     set up this server as a node
  veilon node serve | status | uninstall

Configuration is read from /etc/veilon/veilon.env (override with VEILON_CONFIG)
and environment variables: VEILON_DB_DRIVER (postgres|sqlite), VEILON_DB_DSN,
VEILON_DATA_DIR, VEILON_LISTEN, VEILON_XRAY_BIN, VEILON_XRAY_AUTO_DOWNLOAD,
VEILON_XRAY_VERSION, VEILON_SINGBOX_BIN, VEILON_SINGBOX_VERSION, VEILON_MTG_BIN,
VEILON_MTG_VERSION, VEILON_TRUST_PROXY, VEILON_ACME_DIRECTORY, VEILON_JWT_TTL.
`

// dbCommands open the database directly; as root they re-run themselves as
// the service user so files they create are not owned by root.
var dbCommands = map[string]bool{
	"admin": true, "status": true, "info": true, "set-port": true, "set-path": true,
	"set-address": true, "set-language": true, "ports": true,
}

func main() {
	if len(os.Args) < 2 {
		if os.Geteuid() == 0 && interactive() {
			if err := cmdMenu(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if os.Geteuid() == 0 && os.Getenv("VEILON_CRED_CHILD") != "1" && os.Getenv("VEILON_NO_REEXEC") != "1" &&
		credentialChanging(os.Args[1:]) {
		if _, err := os.Stat(credFile); err == nil {
			os.Exit(runRefreshingCredFile())
		}
	}
	if dbCommands[os.Args[1]] {
		dropToServiceUser()
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "awg-run":
		// Internal: the AmneziaWG sidecar started by "serve".
		if len(args) < 1 {
			err = errors.New("usage: veilon awg-run <config.json>")
		} else {
			err = awgd.Run(args[0])
		}
	case "serve":
		err = cmdServe(args)
	case "admin":
		err = cmdAdmin(args)
	case "status":
		err = cmdStatus(args)
	case "info":
		err = cmdInfo(args)
	case "set-port":
		err = cmdSetPort(args)
	case "set-path":
		err = cmdSetPath(args)
	case "set-address":
		err = cmdSetAddress(args)
	case "set-language":
		err = cmdSetLanguage(args)
	case "ports":
		err = cmdPorts(args)
	case "cert", "certs", "ssl":
		err = cmdCert(args)
	case "start", "stop", "restart", "enable", "disable":
		err = cmdSystemctl(os.Args[1])
	case "logs", "log":
		err = cmdLogs(args)
	case "update", "upgrade":
		err = cmdUpdate(args)
	case "node":
		err = cmdNode(args)
	case "self-update":
		err = cmdSelfUpdate(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "bbr":
		err = cmdBBR(args)
	case "firewall":
		err = cmdFirewall(args)
	case "geo":
		err = cmdGeo(args)
	case "menu":
		err = cmdMenu()
	case "version", "--version", "-v":
		fmt.Printf("veilon %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildDate)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, errAborted) {
			fmt.Fprintln(os.Stderr, "aborted")
		} else {
			fmt.Fprintln(os.Stderr, red("error:"), err)
		}
		os.Exit(1)
	}
}
