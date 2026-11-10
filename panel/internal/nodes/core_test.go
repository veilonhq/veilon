package nodes

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ProcCore with the real Xray binary (VEILON_TEST_XRAY): a valid config
// starts it and its stats API answers; an invalid one is refused while the
// running process keeps going.
func TestProcCoreWithRealXray(t *testing.T) {
	src := os.Getenv("VEILON_TEST_XRAY")
	if src == "" {
		t.Skip("set VEILON_TEST_XRAY to run with a real xray binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray", "xray")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	b, _ := os.ReadFile(src)
	if err := os.WriteFile(bin, b, 0o755); err != nil {
		t.Fatal(err)
	}
	api, socks := freePort(t), freePort(t)
	c := &ProcCore{Bin: bin, Dir: dir, APIPort: api}
	defer c.Stop()
	cfg := fmt.Sprintf(`{"api":{"tag":"api","services":["StatsService"]},"stats":{},
"policy":{"levels":{"0":{"statsUserUplink":true,"statsUserDownlink":true}},"system":{"statsInboundUplink":true,"statsInboundDownlink":true}},
"inbounds":[{"tag":"api","listen":"127.0.0.1","port":%d,"protocol":"dokodemo-door","settings":{"address":"127.0.0.1"}},
{"tag":"in-1","listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth"}}],
"outbounds":[{"tag":"direct","protocol":"freedom"}],
"routing":{"rules":[{"type":"field","inboundTag":["api"],"outboundTag":"api"}]}}`, api, socks)
	ctx := context.Background()
	if err := c.Apply(ctx, []byte(cfg)); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 40; i++ {
		if _, err = c.Collect(ctx); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("stats API: %v", err)
	}
	running, ver, _ := c.Status()
	if !running || ver == "" {
		t.Fatalf("running=%v version=%q", running, ver)
	}
	if err := c.Apply(ctx, []byte(`{"inbounds":[{"protocol":"nope","port":1}]}`)); err == nil {
		t.Fatal("invalid config accepted")
	}
	if running, _, _ := c.Status(); !running {
		t.Fatal("a refused config stopped the running xray")
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(socks), time.Second)
	if err != nil {
		t.Fatalf("inbound not served after a refused config: %v", err)
	}
	conn.Close()
}
