package nodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/guard"
)

// Every Xray line reaches the IP collector; access lines stay out of the
// node's log file, so it does not grow with traffic.
func TestLineWriterSplitsAndFilters(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "xray.log"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	w := &lineWriter{file: f, onLine: func(s string) { got = append(got, s) }}
	acc := accessLine("198.51.100.1", 1, "alice")
	_, _ = w.Write([]byte("Xray 26.11.9 started\n" + acc[:20]))
	_, _ = w.Write([]byte(acc[20:] + "\nwarning: x\n"))
	f.Close()
	if len(got) != 3 || got[1] != acc {
		t.Fatalf("lines %q", got)
	}
	b, _ := os.ReadFile(f.Name())
	if strings.Contains(string(b), "accepted") || !strings.Contains(string(b), "warning: x") {
		t.Fatalf("log file:\n%s", b)
	}
}

// The node firewall guards the inbound ports of the pushed config only.
func TestAgentPortsFromConfig(t *testing.T) {
	rec := &nftRec{}
	fw := &guard.NodeFirewall{Run: rec.run, LookPath: func(string) (string, error) { return "nft", nil }}
	a := &Agent{Firewall: fw}
	a.setPorts([]byte(`{"inbounds":[{"tag":"api","port":62051},{"tag":"inbound-17","port":2096},{"tag":"inbound-18","port":"8443"}]}`))
	if err := fw.Apply([]guard.Ban{{IP: "198.51.100.3", TTL: 60}}); err != nil {
		t.Fatal(err)
	}
	s := rec.all()
	if !strings.Contains(s, "ports { 2096, 8443 }") || strings.Contains(s, "62051") {
		t.Fatalf("firewall script:\n%s", s)
	}
}
