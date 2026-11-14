package guard

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// The script: one IPv4 interval set per listed user, one reject rule per
// peer of that user; IPv6 entries, names, other users and bad peers are
// left out; nothing to do -> "".
func TestUserFWScript(t *testing.T) {
	peers := []AWGPeer{
		{Iface: "vawg3", Addr: "10.66.66.2/32", UserID: 5},
		{Iface: "vawg3", Addr: "10.66.66.3/32", UserID: 6},
		{Iface: "vawg4", Addr: "10.77.0.2/32", UserID: 5},
		{Iface: "eth0", Addr: "10.66.66.9/32", UserID: 5},     // not an AmneziaWG interface
		{Iface: "vawg3", Addr: "fd00::2/128", UserID: 5},      // not IPv4
		{Iface: "vawg3", Addr: "10.66.66.2/32", UserID: 5},    // duplicate
		{Iface: "vawg3", Addr: "10.66.66.4/32", UserID: 7},    // only IPv6 entries
		{Iface: "vawg3; drop", Addr: "10.66.66.5", UserID: 5}, // injection attempt
	}
	lists := map[int64][]string{5: {"93.184.215.14/32", "10.0.0.0/8", "2001:db8::/32"}, 7: {"2001:db8::/32"}}
	s := UserFWScript(peers, lists)
	want := []string{
		"add table inet veilon_userfw\ndelete table inet veilon_userfw\n",
		"set u5 {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n\t\telements = { 10.0.0.0/8, 93.184.215.14/32 }\n\t}",
		"type filter hook forward priority -5; policy accept;",
		"iifname \"vawg3\" ip saddr 10.66.66.2 ip daddr @u5 reject\n",
		"iifname \"vawg4\" ip saddr 10.77.0.2 ip daddr @u5 reject\n",
	}
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Fatalf("script lacks %q:\n%s", w, s)
		}
	}
	for _, bad := range []string{"10.66.66.3", "u6", "u7", "2001:db8", "eth0", "drop", "10.66.66.9"} {
		if strings.Contains(s, bad) {
			t.Fatalf("script has %q:\n%s", bad, s)
		}
	}
	if strings.Count(s, "reject") != 2 {
		t.Fatalf("rules:\n%s", s)
	}
	if UserFWScript(peers, nil) != "" || UserFWScript(nil, lists) != "" {
		t.Fatal("empty plan rendered a script")
	}
}

// Main server: Refresh reads the lists from the database and applies them
// to the AmneziaWG peers; a change re-applies, an emptied list deletes the
// table, shutdown clears it; UserFirewalls hands the lists to the nodes.
func TestUserFWMainServer(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, nil)
	g.DB = testutil.DB(t)
	now := time.Now().UTC()
	u := db.User{Username: "alice", Status: db.StatusActive, UUID: "u1", Password: "p", SubToken: "t1", CreatedAt: now, UpdatedAt: now,
		Firewall: "example.com\n93.184.215.14\n10.0.0.0/8"}
	g.DB.Create(&u)
	g.DB.Create(&db.User{Username: "bob", Status: db.StatusActive, UUID: "u2", Password: "p", SubToken: "t2", CreatedAt: now, UpdatedAt: now, Firewall: "only.names.example"})
	g.SetAWGPeers([]AWGPeer{{Iface: "vawg2", Addr: "10.66.66.2/32", UserID: u.ID}, {Iface: "vawg2", Addr: "10.66.66.3/32", UserID: u.ID + 1}})
	g.Refresh()
	last := func() string {
		for i := len(calls) - 1; i >= 0; i-- {
			if strings.Contains(calls[i].stdin, UserFWTable) {
				return calls[i].stdin
			}
		}
		return ""
	}
	s := last()
	if !strings.Contains(s, "elements = { 10.0.0.0/8, 93.184.215.14/32 }") || !strings.Contains(s, `iifname "vawg2" ip saddr 10.66.66.2 ip daddr @u1 reject`) || strings.Contains(s, "10.66.66.3") {
		t.Fatalf("applied:\n%s", s)
	}
	fw := g.UserFirewalls()
	if len(fw) != 1 || fw[0].UserID != u.ID || strings.Join(fw[0].CIDRs, ",") != "10.0.0.0/8,93.184.215.14/32" {
		t.Fatalf("for nodes: %+v", fw)
	}
	n := len(calls)
	g.Refresh() // unchanged: nothing runs
	if len(calls) != n {
		t.Fatalf("unchanged plan ran nft again: %v", calls[n:])
	}
	g.DB.Model(&db.User{}).Where("id = ?", u.ID).Update("firewall", "")
	g.Refresh()
	if s := last(); strings.Contains(s, "table inet "+UserFWTable+" {") || !strings.Contains(s, "delete table inet "+UserFWTable) {
		t.Fatalf("emptied list did not delete the table:\n%s", s)
	}
	g.DB.Model(&db.User{}).Where("id = ?", u.ID).Update("firewall", "1.1.1.1")
	g.Refresh()
	n = len(calls)
	g.clearUserFW()
	if len(calls) != n+1 || !strings.Contains(calls[n].stdin, "delete table inet "+UserFWTable) {
		t.Fatalf("clear: %v", calls[n:])
	}
}

// A node: lists come from the master (bad prefixes dropped) and survive
// the main-server refresh path; nft missing -> one warning, nothing run;
// an nft failure deletes the half-built table and retries next time.
func TestUserFWNode(t *testing.T) {
	var calls []call
	g := recordingGuard(&calls, func(c call) bool { return strings.Contains(c.stdin, "1.2.3.4/32") })
	g.SetAWGPeers([]AWGPeer{{Iface: "vawg9", Addr: "10.9.0.2/32", UserID: 3}})
	g.ApplyUserFirewall([]UserFW{{UserID: 3, CIDRs: []string{"8.8.8.8/32", "bogus", ""}}, {UserID: 0, CIDRs: []string{"9.9.9.9/32"}}})
	if g.fwSig == "" || !strings.Contains(g.fwSig, "elements = { 8.8.8.8/32 }") || strings.Contains(g.fwSig, "9.9.9.9") {
		t.Fatalf("node sig %q", g.fwSig)
	}
	g.refreshUserFW() // no DB on a node: keeps the master's lists
	if len(g.UserFirewalls()) != 1 {
		t.Fatal("lists lost")
	}
	g.ApplyUserFirewall([]UserFW{{UserID: 3, CIDRs: []string{"1.2.3.4/32"}}})
	if g.fwSig != "" || !g.fwNftFailed || !strings.Contains(calls[len(calls)-1].stdin, "delete table") {
		t.Fatalf("failed apply not rolled back: sig %q calls %v", g.fwSig, calls[len(calls)-1])
	}
	g.ApplyUserFirewall([]UserFW{})
	if g.fwSig != "" {
		t.Fatal("empty list kept rules")
	}

	var calls2 []call
	g2 := recordingGuard(&calls2, nil)
	g2.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	g2.SetAWGPeers([]AWGPeer{{Iface: "vawg9", Addr: "10.9.0.2/32", UserID: 3}})
	g2.ApplyUserFirewall([]UserFW{{UserID: 3, CIDRs: []string{"8.8.8.8/32"}}})
	for _, c := range calls2 {
		if strings.Contains(c.stdin, UserFWTable) {
			t.Fatalf("ran nft without nft: %v", c)
		}
	}
	if !g2.fwNftFailed {
		t.Fatal("missing nft not noted")
	}
}
