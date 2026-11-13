// Package porthop redirects the port-hopping ranges of Hysteria 2 inbounds
// to their listen ports with nftables (table inet veilon_hop), on the main
// server and on nodes.
//
// The rules live in the nat prerouting hook only, so the server's own
// outgoing traffic to those ports is never redirected, and the input
// firewall sees the listen port (the redirect happens before it): only the
// cloud provider's firewall has to allow the ranges.
package porthop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// Table is the nftables table the rules live in.
const Table = "veilon_hop"

// Rule redirects Ranges (UDP) to Port on the inbound's listen address.
type Rule struct {
	InboundID int64               `json:"inbound_id"`
	Listen    string              `json:"listen,omitempty"`
	Port      int                 `json:"port"`
	Ranges    []inbounds.HopRange `json:"ranges"`
}

// FromViews returns the rules of the Hysteria 2 inbounds with hop ports.
func FromViews(ibs []inbounds.View) []Rule {
	var out []Rule
	for i := range ibs {
		ib := &ibs[i]
		if rs := ib.HopRanges(); len(rs) > 0 && ib.Port > 0 {
			out = append(out, Rule{InboundID: ib.ID, Listen: ib.Listen, Port: ib.Port, Ranges: rs})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InboundID < out[j].InboundID })
	return out
}

// Validate checks rules received from the master (a node).
func Validate(rules []Rule) error {
	for _, r := range rules {
		if r.InboundID <= 0 || r.Port < 1 || r.Port > 65535 {
			return fmt.Errorf("hop rule for inbound %d: bad port %d", r.InboundID, r.Port)
		}
		if r.Listen != "" && net.ParseIP(r.Listen) == nil {
			return fmt.Errorf("hop rule for inbound %d: bad listen address %q", r.InboundID, r.Listen)
		}
		if len(r.Ranges) == 0 || len(r.Ranges) > inbounds.MaxHopRanges {
			return fmt.Errorf("hop rule for inbound %d: bad ranges", r.InboundID)
		}
		for _, x := range r.Ranges {
			if x.From < 1024 || x.To > 65535 || x.From > x.To {
				return fmt.Errorf("hop rule for inbound %d: bad range %d-%d", r.InboundID, x.From, x.To)
			}
		}
	}
	return nil
}

// Script renders the nft script that replaces the table with rules ("" =
// no rules: the table is deleted instead).
func Script(rules []Rule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s\ndelete table inet %s\ntable inet %s {\n", Table, Table, Table)
	b.WriteString("\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	for _, r := range rules {
		parts := make([]string, len(r.Ranges))
		for i, x := range r.Ranges {
			parts[i] = x.String()
		}
		set := parts[0]
		if len(parts) > 1 {
			set = "{ " + strings.Join(parts, ", ") + " }"
		}
		match, target := "", fmt.Sprintf("redirect to :%d", r.Port)
		ip := net.ParseIP(r.Listen)
		switch {
		case r.Listen == "" || r.Listen == "::":
		case r.Listen == "0.0.0.0":
			match = "meta nfproto ipv4 "
		case ip != nil && ip.To4() != nil:
			match = "ip daddr " + ip.To4().String() + " "
			target = fmt.Sprintf("dnat ip to %s:%d", ip.To4().String(), r.Port)
		case ip != nil:
			match = "ip6 daddr " + ip.String() + " "
			target = fmt.Sprintf("dnat ip6 to [%s]:%d", ip.String(), r.Port)
		}
		fmt.Fprintf(&b, "\t\t%sudp dport %s %s comment \"inbound-%d\"\n", match, set, target, r.InboundID)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// Applier keeps the table in line with the last rules. Zero value works
// (uses the nft binary); safe for concurrent use.
type Applier struct {
	// Run runs an nft script (default: nft -f -); Delete removes the
	// table (default: nft delete table, "not found" is fine); Exists
	// reports whether the table is there (default: nft list table).
	Run    func(script string) error
	Delete func() error
	Exists func() bool

	mu      sync.Mutex
	done    bool   // applied at least once (a table left by a crash is replaced or removed)
	script  string // last applied ("" = none)
	lastErr string
	warned  bool
}

// Apply replaces the table with rules (nil removes it). Unchanged rules
// are not applied again.
func (a *Applier) Apply(rules []Rule) error {
	s := Script(rules)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done && s == a.script && a.lastErr == "" {
		return nil
	}
	a.done = true
	return a.applyLocked(s)
}

func (a *Applier) applyLocked(s string) error {
	var err error
	if s == "" {
		err = a.delete()
	} else {
		err = a.run(s)
	}
	if err != nil {
		a.lastErr = err.Error()
		if !a.warned {
			slog.Warn("port hopping rules could not be applied", "err", err)
			a.warned = true
		}
		return err
	}
	if s != a.script {
		slog.Info("port hopping rules applied", "rules", strings.Count(s, "comment \"inbound-"))
	}
	a.script, a.lastErr, a.warned = s, "", false
	return nil
}

// Clear removes the table (shutdown, engine switched off).
func (a *Applier) Clear() error { return a.Apply(nil) }

// Err is the last apply error ("" = fine).
func (a *Applier) Err() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastErr
}

// Keep re-applies the rules every interval when the table went missing
// (an external flush) or the last apply failed, until ctx ends; then it
// removes the table.
func (a *Applier) Keep(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = a.Clear()
			return
		case <-t.C:
			a.mu.Lock()
			if a.script != "" && (a.lastErr != "" || !a.exists()) {
				_ = a.applyLocked(a.script)
			}
			a.mu.Unlock()
		}
	}
}

func (a *Applier) run(s string) error {
	if a.Run != nil {
		return a.Run(s)
	}
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(s)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func (a *Applier) delete() error {
	if a.Delete != nil {
		return a.Delete()
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return nil // nothing could have been added without nft
	}
	out, err := exec.Command("nft", "delete", "table", "inet", Table).CombinedOutput()
	if err != nil {
		msg := string(out)
		if strings.Contains(msg, "No such file") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return errors.New("nft: " + strings.TrimSpace(msg))
	}
	return nil
}

func (a *Applier) exists() bool {
	if a.Exists != nil {
		return a.Exists()
	}
	return exec.Command("nft", "list", "table", "inet", Table).Run() == nil
}
