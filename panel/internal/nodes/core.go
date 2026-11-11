package nodes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/veilonhq/veilon/panel/internal/xray"
)

// ProcCore runs the Xray binary as a child process and restarts it when it
// exits on its own.
type ProcCore struct {
	Bin     string
	Dir     string // config.json is written here
	APIPort int
	// OnLine receives every Xray output line (the access log feeds the
	// IP limits). Access log lines are not written to xray.log.
	OnLine func(string)

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	stopped bool // stopped on purpose (no automatic restart)
	lastErr string
	version string
	stats   *xray.StatsClient
}

func (c *ProcCore) cfgPath() string { return filepath.Join(c.Dir, "xray.json") }

// Apply tests cfg and restarts Xray with it.
func (c *ProcCore) Apply(ctx context.Context, cfg []byte) error {
	if _, err := os.Stat(c.Bin); err != nil {
		return fmt.Errorf("xray is not installed at %s", c.Bin)
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	next := filepath.Join(c.Dir, ".xray-next.json")
	if err := os.WriteFile(next, cfg, 0o600); err != nil {
		return err
	}
	defer os.Remove(next)
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, c.Bin, "run", "-test", "-c", next)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(c.Bin))
	if out, err := cmd.CombinedOutput(); err != nil {
		return errorf("xray rejected the config: %s", lastLines(string(out), 3))
	}
	c.Stop()
	if err := os.Rename(next, c.cfgPath()); err != nil {
		return err
	}
	return c.start()
}

func (c *ProcCore) start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version == "" {
		c.version = xrayVersion(c.Bin)
	}
	cmd := exec.Command(c.Bin, "run", "-c", c.cfgPath())
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(c.Bin))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	logf, err := os.OpenFile(filepath.Join(c.Dir, "xray.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err == nil {
		lw := &lineWriter{file: logf, onLine: c.OnLine}
		cmd.Stdout, cmd.Stderr = lw, lw
	}
	if err := cmd.Start(); err != nil {
		if logf != nil {
			logf.Close()
		}
		c.lastErr = err.Error()
		return err
	}
	done := make(chan struct{})
	c.cmd, c.done, c.stopped, c.lastErr = cmd, done, false, ""
	go func() {
		err := cmd.Wait()
		if logf != nil {
			logf.Close()
		}
		close(done)
		c.mu.Lock()
		if c.cmd != cmd {
			c.mu.Unlock()
			return
		}
		c.cmd = nil
		restart := !c.stopped
		if err != nil {
			c.lastErr = "xray exited: " + err.Error()
		} else {
			c.lastErr = "xray exited"
		}
		c.mu.Unlock()
		if restart {
			slog.Warn("xray exited on the node; restarting in 5s", "err", err)
			time.Sleep(5 * time.Second)
			c.mu.Lock()
			again := c.cmd == nil && !c.stopped
			c.mu.Unlock()
			if again {
				_ = c.start()
			}
		}
	}()
	return nil
}

// Stop stops Xray (no automatic restart).
func (c *ProcCore) Stop() {
	c.mu.Lock()
	cmd, done := c.cmd, c.done
	c.stopped = true
	c.cmd = nil
	if c.stats != nil {
		c.stats.Close()
		c.stats = nil
	}
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

// Status reports the process state.
func (c *ProcCore) Status() (bool, string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version == "" {
		c.version = xrayVersion(c.Bin)
	}
	return c.cmd != nil, c.version, c.lastErr
}

// Collect reads and resets the traffic counters.
func (c *ProcCore) Collect(ctx context.Context) (xray.Traffic, error) {
	c.mu.Lock()
	if c.stats == nil {
		sc, err := xray.NewStatsClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(c.APIPort)))
		if err != nil {
			c.mu.Unlock()
			return xray.Traffic{}, err
		}
		c.stats = sc
	}
	sc := c.stats
	c.mu.Unlock()
	st, err := sc.Query(ctx, "", true)
	if err != nil {
		return xray.Traffic{}, err
	}
	return xray.ParseStats(st), nil
}

// Online lists the users with open connections (GetAllOnlineUsers).
func (c *ProcCore) Online(ctx context.Context) ([]int64, error) {
	running, ver, _ := c.Status()
	if !running {
		return nil, errors.New("xray is not running")
	}
	if !xray.TracksConnections(ver) {
		return nil, ErrOnlineUnsupported
	}
	c.mu.Lock()
	if c.stats == nil {
		sc, err := xray.NewStatsClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(c.APIPort)))
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.stats = sc
	}
	sc := c.stats
	c.mu.Unlock()
	ids, err := sc.OnlineUsers(ctx)
	if status.Code(err) == codes.Unimplemented {
		return nil, ErrOnlineUnsupported
	}
	return ids, err
}

func xrayVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return ""
	}
	if f := strings.Fields(string(out)); len(f) >= 2 {
		return f[1]
	}
	return ""
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.TrimSpace(strings.Join(ls, " | "))
}

// lineWriter splits Xray's output into lines: every line goes to onLine,
// and all but access log lines (one per connection) go to the log file, so
// the file does not grow with traffic.
type lineWriter struct {
	mu     sync.Mutex
	file   *os.File
	onLine func(string)
	buf    []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if w.onLine != nil {
			w.onLine(line)
		}
		if !(strings.Contains(line, " accepted ") && strings.Contains(line, " from ")) {
			_, _ = w.file.WriteString(line + "\n")
		}
	}
	if len(w.buf) > 64<<10 {
		w.buf = w.buf[:0] // a line this long is not Xray's; drop it
	}
	return len(p), nil
}
