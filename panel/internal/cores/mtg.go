package cores

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// MTGVersion is the mtg-multi release Veilon is tested with.
const MTGVersion = "1.15.0"

// MTGConfig renders the mtg-multi TOML config of one MTProto inbound.
func MTGConfig(ib *inbounds.View, in *xray.Input, apiPort int) []byte {
	var b strings.Builder
	listen := ib.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	fmt.Fprintf(&b, "# Veilon: MTProto inbound %d (%s)\n", ib.ID, strings.ReplaceAll(ib.Remark, "\n", " "))
	fmt.Fprintf(&b, "bind-to = %q\n", net.JoinHostPort(listen, strconv.Itoa(ib.Port)))
	fmt.Fprintf(&b, "api-bind-to = %q\n", "127.0.0.1:"+strconv.Itoa(apiPort))
	b.WriteString("prefer-ip = \"prefer-ipv4\"\n\n")
	b.WriteString("[defense.blocklist]\nenabled = false\n\n")
	b.WriteString("[defense.anti-replay]\nenabled = true\nmax-size = \"1mib\"\nerror-rate = 0.001\n\n")
	// [secrets] must be the last section.
	b.WriteString("[secrets]\n")
	for _, u := range in.Users[ib.ID] {
		fmt.Fprintf(&b, "%q = %q\n", xray.Email(&u), inbounds.MTProtoSecret(ib.Settings.FakeTLSDomain, u.Password))
	}
	return []byte(b.String())
}

// MTG runs one mtg-multi process per MTProto inbound.
type MTG struct {
	*Supervisor
	opt   Options
	mu    sync.Mutex
	ports map[int64]int // inbound id -> stats API port
	cnt   counter
}

type mtgMeta struct {
	InboundID int64
	APIPort   int
}

// NewMTG creates the MTProto engine.
func NewMTG(opt Options) *MTG {
	e := &MTG{opt: opt, ports: map[int64]int{}, cnt: counter{}}
	e.Supervisor = NewSupervisor(Spec{
		DB:           opt.DB,
		Name:         "mtg",
		Protocols:    []string{inbounds.MTProto},
		Bin:          func() string { return opt.BinPath },
		Ensure:       e.ensure,
		Version:      func() string { return strings.TrimPrefix(opt.Version, "v") },
		ReloadSignal: syscall.SIGHUP, // reloads [secrets] without dropping other users
		Plan:         e.plan,
		Collect:      e.collect,
		ConfigGlob:   filepath.Join(opt.DataDir, "mtg", "inbound-*.toml"),
	})
	return e
}

func (e *MTG) apiPort(id int64) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.ports[id]; ok {
		return p
	}
	p := 0
	if l, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		p = l.Addr().(*net.TCPAddr).Port
		l.Close()
	}
	e.ports[id] = p
	return p
}

func (e *MTG) plan() ([]Instance, error) {
	in, err := xray.LoadInput(e.opt.DB, e.opt.DataDir, 0)
	if err != nil {
		return nil, err
	}
	var out []Instance
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if ib.Protocol != inbounds.MTProto || len(in.Users[ib.ID]) == 0 {
			continue // mtg refuses to start without a secret
		}
		port := e.apiPort(ib.ID)
		path := filepath.Join(e.opt.DataDir, "mtg", fmt.Sprintf("inbound-%d.toml", ib.ID))
		out = append(out, Instance{
			Key: ib.Tag(), Config: MTGConfig(ib, in, port), ConfigFile: path,
			Args:      []string{"run", path},
			ReloadKey: fmt.Sprintf("%s|%d|%d|%s", ib.Listen, ib.Port, port, ib.Settings.FakeTLSDomain),
			Meta:      mtgMeta{InboundID: ib.ID, APIPort: port},
		})
	}
	return out, nil
}

func (e *MTG) ensure(ctx context.Context) error {
	if _, err := os.Stat(e.opt.BinPath); err == nil {
		return nil
	}
	if !e.opt.AutoDownload {
		return fmt.Errorf("mtg-multi binary not found at %s (auto-download disabled)", e.opt.BinPath)
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return fmt.Errorf("mtg-multi has no release for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	ver := strings.TrimPrefix(e.opt.Version, "v")
	if ver == "" || ver == "latest" {
		tag, err := ResolveLatest(ctx, "dolonet/mtg-multi")
		if err != nil {
			return err
		}
		ver = strings.TrimPrefix(tag, "v")
	}
	base := fmt.Sprintf("https://github.com/dolonet/mtg-multi/releases/download/v%s/", ver)
	return downloadTarBinary(ctx, base+fmt.Sprintf("mtg-multi-%s-linux-%s.tar.gz", ver, runtime.GOARCH),
		base+fmt.Sprintf("mtg-multi-%s-checksums.txt", ver), "mtg-multi", e.opt.BinPath)
}

type mtgStats struct {
	Users map[string]struct {
		BytesIn  int64 `json:"bytes_in"`
		BytesOut int64 `json:"bytes_out"`
	} `json:"users"`
}

func (e *MTG) collect(ctx context.Context, running []Running) {
	deltas := map[int64][2]int64{}
	var su, sd int64
	for _, r := range running {
		m, ok := r.Meta.(mtgMeta)
		if !ok || m.APIPort == 0 {
			continue
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/stats", m.APIPort), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		var st mtgStats
		err = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		if err != nil {
			continue
		}
		e.mu.Lock()
		for name, u := range st.Users {
			// bytes_in: received from the client (upload).
			du, dd := e.cnt.delta(fmt.Sprintf("%d/%s", m.InboundID, name), r.Gen, u.BytesIn, u.BytesOut)
			su += du
			sd += dd
			if id, ok := xray.ParseEmail(name); ok {
				d := deltas[id]
				deltas[id] = [2]int64{d[0] + du, d[1] + dd}
			}
		}
		e.mu.Unlock()
	}
	e.AddSystem(su, sd)
	if err := xray.ApplyUserTraffic(e.opt.DB, deltas, time.Now().UTC()); err != nil {
		slog.Warn("store mtproto user traffic", "err", err)
	}
}
