package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/veilonhq/veilon/panel/internal/api"
	"github.com/veilonhq/veilon/panel/internal/auth"
	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/cores"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/guard"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/jobs"
	"github.com/veilonhq/veilon/panel/internal/logbuf"
	"github.com/veilonhq/veilon/panel/internal/nodes"
	"github.com/veilonhq/veilon/panel/internal/presence"
	"github.com/veilonhq/veilon/panel/internal/selfupdate"
	"github.com/veilonhq/veilon/panel/internal/stats"
	"github.com/veilonhq/veilon/panel/internal/sub"
	usagemeter "github.com/veilonhq/veilon/panel/internal/usage"
	"github.com/veilonhq/veilon/panel/internal/users"
	"github.com/veilonhq/veilon/panel/internal/util"
	"github.com/veilonhq/veilon/panel/internal/version"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

func cmdServe(args []string) error {
	panelLogs := logbuf.New("panel", 2000)
	slog.SetDefault(slog.New(&logbuf.Handler{Next: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}), Ring: panelLogs}))
	cfg, gdb, err := openDB()
	if err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	slog.Info("veilon starting", "version", version.Version, "db", cfg.DBDriver, "data_dir", cfg.DataDir)

	var admins int64
	gdb.Model(&db.Admin{}).Count(&admins)
	if admins == 0 {
		if cfg.InitAdminUser != "" && cfg.InitAdminPass != "" {
			if _, err := auth.CreateAdmin(gdb, cfg.InitAdminUser, cfg.InitAdminPass, true); err != nil {
				return fmt.Errorf("create initial admin: %w", err)
			}
			slog.Info("initial admin created", "username", cfg.InitAdminUser)
		} else {
			slog.Warn("no admin exists; create one with: veilon admin create --username admin")
		}
	}

	authSvc, err := auth.NewService(gdb, cfg.JWTTTL)
	if err != nil {
		return err
	}
	apiPort, _ := strconv.Atoi(db.GetSetting(gdb, db.KeyXrayAPIPort))

	sbPort, _ := strconv.Atoi(db.GetSetting(gdb, db.KeySingboxAPIPort))
	if sbPort == 0 {
		sbPort = randPort(30001, 39999, apiPort)
		_ = db.SetSetting(gdb, db.KeySingboxAPIPort, strconv.Itoa(sbPort))
	}
	connGuard := guard.New(gdb, filepath.Join(filepath.Dir(cfg.XrayBin), "geoip.dat"))
	xcore := xray.NewManager(xray.Options{
		DB: gdb, DataDir: cfg.DataDir, BinPath: cfg.XrayBin, APIPort: apiPort, BridgePort: sbPort,
		AutoDownload: cfg.XrayAutoDownload, Version: cfg.XrayVersion, OnLine: connGuard.ObserveLine,
	})
	self, _ := os.Executable()
	core := &cores.Set{Engines: []cores.Engine{
		xcore,
		cores.NewSingbox(cores.Options{DB: gdb, DataDir: cfg.DataDir, BinPath: cfg.SingboxBin, Version: cfg.SingboxVersion, AutoDownload: cfg.XrayAutoDownload, BridgePort: sbPort,
			OnLine: connGuard.ObserveSingboxLine}),
		cores.NewMTG(cores.Options{DB: gdb, DataDir: cfg.DataDir, BinPath: cfg.MTGBin, Version: cfg.MTGVersion, AutoDownload: cfg.XrayAutoDownload}),
		cores.NewAWG(cores.Options{DB: gdb, DataDir: cfg.DataDir}, self),
	}}
	// Remote nodes get every change too (their config is rendered from the
	// same users, inbounds and template).
	nodeMgr := &nodes.Manager{DB: gdb, DataDir: cfg.DataDir, PKI: &nodes.PKI{Dir: filepath.Join(cfg.DataDir, "nodes")},
		OnIPs: connGuard.ObserveRemote, Bans: connGuard.Bans, Shapes: connGuard.Shapes, Version: version.Version}
	reloadAll := func() { core.RequestReload(); nodeMgr.Kick() }
	certSvc := &certs.Service{DB: gdb, DataDir: cfg.DataDir, ACMEDirectory: cfg.ACMEDirectory, OnChange: reloadAll,
		AllowNonPublicIPs: cfg.ACMEAllowPrivateIPs}
	if cfg.ACMECAFile != "" {
		hc, err := acmeHTTPClient(cfg.ACMECAFile)
		if err != nil {
			return err
		}
		certSvc.ACMEHTTPClient = hc
		slog.Warn("using extra ACME CA roots (testing only)", "file", cfg.ACMECAFile)
	}
	if err := certSvc.SyncFiles(); err != nil {
		slog.Warn("certificate file sync failed", "err", err)
	}
	restartListener := make(chan struct{}, 1)
	var srv *api.Server
	userSvc := &users.Service{
		DB:       gdb,
		OnChange: func() { reloadAll(); connGuard.Refresh() },
		SubURL:   func(token, host string) string { return sub.SubURL(srv.Settings(), token, host) },
	}
	inbSvc := &inbounds.Service{
		DB:         gdb,
		OnChange:   func() { reloadAll(); connGuard.Refresh() },
		SelfSigned: certSvc.SelfSigned,
		ReservedPorts: func() []int {
			return []int{srv.Settings().PanelPort, apiPort, sbPort}
		},
		NodeCaps: nodeMgr.Caps,
	}
	sampler := &stats.Sampler{DiskPath: cfg.DataDir}
	// Loopback health URL of this panel, for the updater's health check.
	var healthURL atomic.Value
	healthURL.Store("")
	updates := &selfupdate.Service{
		Checker: &selfupdate.Checker{Current: version.Version, FirstDelay: time.Minute, Interval: 6 * time.Hour},
		DataDir: cfg.DataDir, Current: version.Version, Self: self,
		HealthURL: func() string { return healthURL.Load().(string) },
	}
	if d, err := time.ParseDuration(os.Getenv("VEILON_UPDATE_FIRST_CHECK")); err == nil {
		updates.Checker.FirstDelay = d
	}
	srv = api.New(&api.Server{
		DB: gdb, Auth: authSvc, Users: userSvc, Inbounds: inbSvc, Certs: certSvc,
		Sub: &sub.Service{DB: gdb}, Core: core, Sampler: sampler, TrustProxy: cfg.TrustProxy, PanelLogs: panelLogs, Update: updates,
		Nodes: &nodes.Service{DB: gdb, PKI: nodeMgr.PKI, Manager: nodeMgr, CheckMoveToMain: inbSvc.CheckMoveToMain},
		Live:  func(id int64) []string { return connGuard.Tracker.Live(id, time.Now().UTC()) },
		OnListenerChange: func() {
			select {
			case restartListener <- struct{}{}:
			default:
			}
		},
	})
	// The panel answers ACME http-01 itself when it listens with plain HTTP
	// on the challenge port.
	var plainPanelPort atomic.Int64
	certSvc.PanelServesHTTP01 = func(port int) bool { return int64(port) == plainPanelPort.Load() }
	runner := &jobs.Runner{
		DB: gdb, TakeServers: usagemeter.TakeAll, RestoreServers: usagemeter.RestoreAll,
		OnUsersChanged: reloadAll,
		RenewCerts:     certSvc.RenewDue,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	coreDone := make(chan struct{})
	go func() { util.Supervise(ctx, "cores", core.Run); close(coreDone) }()
	go util.Supervise(ctx, "system stats", func(ctx context.Context) { sampler.Run(ctx, 3*time.Second) })
	go util.Supervise(ctx, "update check", updates.Checker.Run)
	go util.Supervise(ctx, "nodes", nodeMgr.Run)
	// Online status lives in memory; last_online_at is stored on changes
	// and at most once a minute while a user stays online.
	go util.Supervise(ctx, "online status", func(ctx context.Context) { presence.Default.Run(ctx, gdb, 5*time.Second) })
	guardDone := make(chan struct{})
	go func() { util.Supervise(ctx, "connection guard", connGuard.RunLoop); close(guardDone) }()
	// The jobs runner stops only after the cores: their final counter reads
	// land in the users' totals, and the runner's last flush must store them
	// in the traffic history too.
	jobsCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	jobsDone := make(chan struct{})
	go func() { util.Supervise(jobsCtx, "jobs", runner.Run); close(jobsDone) }()
	finish := func() {
		stop()
		<-coreDone
		stopJobs()
		<-jobsDone
	}
	ctl := &ctlServer{certs: certSvc, api: srv, updateGeo: func(ctx context.Context) error {
		if err := updateGeoFiles(ctx, filepath.Dir(cfg.XrayBin)); err != nil {
			return err
		}
		xcore.Restart()
		return nil
	}}
	go util.Safe("control socket", func() { serveCtl(ctx, ctlSocketPath(cfg.DataDir), ctl.handler()) })
	// Ad blocking needs geosite.dat (geosite:category-ads-all); fetch the
	// geo files once when an older install lacks them.
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.XrayBin), "geosite.dat")); err != nil {
		go util.Safe("geo files download", func() {
			if err := updateGeoFiles(ctx, filepath.Dir(cfg.XrayBin)); err != nil {
				slog.Warn("geo files download failed", "err", err)
				return
			}
			xcore.Restart()
		})
	}

	for {
		st := srv.Settings()
		addr := net.JoinHostPort(cfg.Listen, strconv.Itoa(st.PanelPort))
		hs := &http.Server{
			Addr: addr, Handler: srv,
			ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute, IdleTimeout: 2 * time.Minute,
		}
		useTLS := false
		if st.PanelCertID != nil {
			id := *st.PanelCertID
			if _, err := certSvc.Certificate(id); err != nil {
				slog.Warn("panel certificate unavailable, serving plain HTTP", "cert_id", id, "err", err)
			} else {
				useTLS = true
				hs.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					return certSvc.Certificate(id)
				}}
			}
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			finish()
			return fmt.Errorf("listen %s: %w", addr, err)
		}
		errCh := make(chan error, 1)
		go func() {
			if useTLS {
				errCh <- hs.ServeTLS(ln, "", "")
			} else {
				errCh <- hs.Serve(ln)
			}
		}()
		scheme := "http"
		if useTLS {
			scheme = "https"
			plainPanelPort.Store(0)
		} else {
			plainPanelPort.Store(int64(st.PanelPort))
		}
		host := cfg.Listen
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		healthURL.Store(fmt.Sprintf("%s://%s/%s/api/v1/health", scheme, net.JoinHostPort(host, strconv.Itoa(st.PanelPort)), st.PanelPath))
		slog.Info("panel listening", "addr", addr, "url", fmt.Sprintf("%s://%s/%s/", scheme, addr, st.PanelPath))

		select {
		case <-ctx.Done():
			shut(hs)
			finish()
			<-guardDone
			slog.Info("veilon stopped")
			return nil
		case <-restartListener:
			slog.Info("restarting panel listener")
			shut(hs)
			srv.ReloadSettings()
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				finish()
				return err
			}
		}
	}
}

func shut(hs *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
}

// acmeHTTPClient trusts the system roots plus the PEM roots in file
// (VEILON_ACME_CA_FILE, for testing against a private ACME CA).
func acmeHTTPClient(file string) (*http.Client, error) {
	pemData, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("VEILON_ACME_CA_FILE: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("VEILON_ACME_CA_FILE: no certificates in %s", file)
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}, nil
}
