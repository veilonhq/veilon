// Package awgd is the AmneziaWG sidecar: a userspace AmneziaWG server
// (amneziawg-go) that Veilon runs as a child process ("veilon awg-run").
// It creates one TUN interface per AmneziaWG inbound, assigns the server
// address, enables forwarding and NAT for the client subnet, applies peers
// on SIGHUP without dropping sessions and reports per-peer traffic on stdout.
//
// It needs CAP_NET_ADMIN (TUN, addresses, NAT); the systemd unit written by
// the installer grants it.
package awgd

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
)

// Version is the embedded amneziawg-go version.
const Version = "amneziawg-go 0.2.12"

// StatsPrefix starts a stats line on stdout.
const StatsPrefix = "@@stats "

// Peer is one client.
type Peer struct {
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	AllowedIP    string `json:"allowed_ip"`
}

// Params are AmneziaWG obfuscation parameters.
type Params struct {
	Jc   int    `json:"jc"`
	Jmin int    `json:"jmin"`
	Jmax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	H1   uint32 `json:"h1"`
	H2   uint32 `json:"h2"`
	H3   uint32 `json:"h3"`
	H4   uint32 `json:"h4"`
}

// Iface is one AmneziaWG server interface.
type Iface struct {
	Name       string `json:"name"`
	ListenPort int    `json:"listen_port"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"` // server address with prefix
	MTU        int    `json:"mtu"`
	Params     Params `json:"params"`
	Peers      []Peer `json:"peers"`
}

// Config is the sidecar configuration file.
type Config struct {
	Interfaces []Iface `json:"interfaces"`
}

func b64hex(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("bad key %q", s)
	}
	return hex.EncodeToString(b), nil
}

// UAPI renders the amneziawg-go "set" operation for an interface. With
// peersOnly only the peer list is replaced.
func UAPI(i Iface, peersOnly bool) (string, error) {
	var b strings.Builder
	if !peersOnly {
		k, err := b64hex(i.PrivateKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "private_key=%s\nlisten_port=%d\n", k, i.ListenPort)
		p := i.Params
		fmt.Fprintf(&b, "jc=%d\njmin=%d\njmax=%d\ns1=%d\ns2=%d\nh1=%d\nh2=%d\nh3=%d\nh4=%d\n", p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.H1, p.H2, p.H3, p.H4)
	}
	b.WriteString("replace_peers=true\n")
	for _, p := range i.Peers {
		pk, err := b64hex(p.PublicKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "public_key=%s\n", pk)
		if p.PresharedKey != "" {
			psk, err := b64hex(p.PresharedKey)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "preshared_key=%s\n", psk)
		}
		fmt.Fprintf(&b, "replace_allowed_ips=true\nallowed_ip=%s\n", p.AllowedIP)
	}
	return b.String(), nil
}

// ParseStats parses amneziawg-go "get" output into public key (base64) ->
// {rx, tx} bytes.
func ParseStats(get string) map[string][2]int64 {
	out := map[string][2]int64{}
	cur := ""
	sc := bufio.NewScanner(strings.NewReader(get))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "public_key":
			if b, err := hex.DecodeString(v); err == nil {
				cur = base64.StdEncoding.EncodeToString(b)
				out[cur] = [2]int64{}
			}
		case "rx_bytes", "tx_bytes":
			if cur == "" {
				continue
			}
			n, _ := strconv.ParseInt(v, 10, 64)
			c := out[cur]
			if k == "rx_bytes" {
				c[0] = n
			} else {
				c[1] = n
			}
			out[cur] = c
		}
	}
	return out
}

// Load reads a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

type running struct {
	cfg Iface
	dev *device.Device
	net *netSetup
}

// Run serves the config until SIGTERM/SIGINT. SIGHUP re-reads the file and
// replaces peers.
func Run(path string) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	logLevel := device.LogLevelError
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = device.LogLevelVerbose
	}
	var devs []*running
	cleanup := func() {
		for _, r := range devs {
			r.dev.Close()
			r.net.down()
		}
	}
	for _, i := range cfg.Interfaces {
		t, err := tun.CreateTUN(i.Name, i.MTU)
		if err != nil {
			cleanup()
			return fmt.Errorf("create TUN %s: %w (the service needs CAP_NET_ADMIN and /dev/net/tun)", i.Name, err)
		}
		dev := device.NewDevice(t, conn.NewDefaultBind(), device.NewLogger(logLevel, "("+i.Name+") "))
		u, err := UAPI(i, false)
		if err == nil {
			err = dev.IpcSet(u)
		}
		if err == nil {
			err = dev.Up()
		}
		if err != nil {
			dev.Close()
			cleanup()
			return fmt.Errorf("configure %s: %w", i.Name, err)
		}
		ns := &netSetup{iface: i.Name, address: i.Address, mtu: i.MTU}
		if err := ns.up(); err != nil {
			dev.Close()
			cleanup()
			return fmt.Errorf("network setup %s: %w", i.Name, err)
		}
		devs = append(devs, &running{cfg: i, dev: dev, net: ns})
		fmt.Printf("interface %s listening on udp/%d with %d peers\n", i.Name, i.ListenPort, len(i.Peers))
	}
	sig := make(chan os.Signal, 4)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	report := func() {
		all := map[string]map[string][2]int64{}
		for _, r := range devs {
			if g, err := r.dev.IpcGet(); err == nil {
				all[r.cfg.Name] = ParseStats(g)
			}
		}
		b, _ := json.Marshal(all)
		fmt.Println(StatsPrefix + string(b))
	}
	for {
		select {
		case s := <-sig:
			if s != syscall.SIGHUP {
				report()
				cleanup()
				return nil
			}
			report()
			nc, err := Load(path)
			if err != nil {
				fmt.Println("reload failed:", err)
				continue
			}
			for _, r := range devs {
				for _, i := range nc.Interfaces {
					if i.Name != r.cfg.Name {
						continue
					}
					u, err := UAPI(i, true)
					if err == nil {
						err = r.dev.IpcSet(u)
					}
					if err != nil {
						fmt.Println("reload", i.Name, "failed:", err)
					} else {
						r.cfg.Peers = i.Peers
						fmt.Printf("interface %s reloaded with %d peers\n", i.Name, len(i.Peers))
					}
				}
			}
		case <-tick.C:
			report()
		}
	}
}

// netSetup configures the interface address, forwarding and NAT with the
// standard ip / iptables (or nft) tools.
type netSetup struct {
	iface, address string
	mtu            int
	nat            string // "iptables" | "nft" | ""
	rules          [][]string
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (n *netSetup) up() error {
	if err := run("ip", "address", "replace", n.address, "dev", n.iface); err != nil {
		return err
	}
	if err := run("ip", "link", "set", "dev", n.iface, "mtu", strconv.Itoa(n.mtu), "up"); err != nil {
		return err
	}
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
		fmt.Println("warning: cannot enable IPv4 forwarding:", err)
	}
	p, err := netip.ParsePrefix(n.address)
	if err != nil {
		return err
	}
	subnet := p.Masked().String()
	if _, err := exec.LookPath("iptables"); err == nil {
		n.nat = "iptables"
		n.rules = [][]string{
			{"-t", "nat", "POSTROUTING", "-s", subnet, "!", "-o", n.iface, "-j", "MASQUERADE"},
			{"-t", "filter", "FORWARD", "-i", n.iface, "-j", "ACCEPT"},
			{"-t", "filter", "FORWARD", "-o", n.iface, "-j", "ACCEPT"},
		}
		for _, r := range n.rules {
			check := append([]string{r[0], r[1], "-C"}, r[2:]...)
			if run("iptables", check...) == nil {
				continue
			}
			add := append([]string{r[0], r[1], "-I"}, r[2:]...)
			if err := run("iptables", add...); err != nil {
				fmt.Println("warning:", err)
			}
		}
		return nil
	}
	if _, err := exec.LookPath("nft"); err == nil {
		n.nat = "nft"
		script := fmt.Sprintf(`table ip veilon_%[1]s {
	chain post { type nat hook postrouting priority 100; ip saddr %[2]s oifname != "%[1]s" masquerade; }
	chain fwd { type filter hook forward priority 0; iifname "%[1]s" accept; oifname "%[1]s" accept; }
}
`, n.iface, subnet)
		cmd := exec.Command("nft", "-f", "-")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Println("warning: nft:", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	fmt.Println("warning: neither iptables nor nft found; clients will not reach the internet (configure NAT manually)")
	return nil
}

func (n *netSetup) down() {
	switch n.nat {
	case "iptables":
		for _, r := range n.rules {
			del := append([]string{r[0], r[1], "-D"}, r[2:]...)
			_ = run("iptables", del...)
		}
	case "nft":
		_ = run("nft", "delete", "table", "ip", "veilon_"+n.iface)
	}
}
