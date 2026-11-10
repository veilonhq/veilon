package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// UpdaterUnit is the systemd path unit that runs the privileged updater
// when the panel writes a request.
const (
	UpdaterPathUnit    = "/etc/systemd/system/veilon-updater.path"
	UpdaterServiceUnit = "/etc/systemd/system/veilon-updater.service"
)

// Service is the panel side: it knows the latest release and starts updates.
type Service struct {
	Checker *Checker
	DataDir string
	Current string
	// HealthURL is the loopback URL of this panel's health endpoint.
	HealthURL func() string
	// Self is the running binary (used when the updater is spawned).
	Self string
}

func unitInstalled() bool {
	_, err := os.Stat(UpdaterPathUnit)
	return err == nil
}

// spawnAllowed: run the updater ourselves (as root without the unit, or in
// development with VEILON_UPDATER_SPAWN=1).
func spawnAllowed() bool { return os.Getenv("VEILON_UPDATER_SPAWN") == "1" || os.Geteuid() == 0 }

// CanUpdate reports whether an update can be started from the panel.
func (s *Service) CanUpdate() bool { return unitInstalled() || spawnAllowed() }

// Status returns the current or last update.
func (s *Service) Status() *Status { return ReadStatus(s.DataDir) }

// Start asks the updater to install tag. tag must be the newest stable
// release the checker knows.
func (s *Service) Start(tag, by string) (*Status, error) {
	latest, _ := s.Checker.Latest()
	if latest == nil || latest.Tag != tag {
		return nil, fmt.Errorf("%s is not an available update", tag)
	}
	if !s.CanUpdate() {
		return nil, fmt.Errorf("the updater is not installed on this server; run \"sudo veilon update\" once")
	}
	now := time.Now().UTC()
	if Active(ReadStatus(s.DataDir), now) {
		return nil, ErrBusy
	}
	st := &Status{Tag: tag, From: s.Current, Phase: PhaseQueued, Running: true, StartedAt: now, UpdatedAt: now}
	if err := writeJSONAtomic(StatusPath(s.DataDir), st, 0o644); err != nil {
		return nil, err
	}
	req := Request{Tag: tag, From: s.Current, HealthURL: s.HealthURL(), RequestedBy: by, RequestedAt: now}
	// Written in place (not renamed): the systemd path unit fires on
	// close-after-write.
	if err := writeJSONInPlace(RequestPath(s.DataDir), req); err != nil {
		return nil, err
	}
	if !unitInstalled() {
		if err := s.spawn(); err != nil {
			st.Running, st.Phase, st.Error = false, PhaseFailed, err.Error()
			_ = writeJSONAtomic(StatusPath(s.DataDir), st, 0o644)
			return st, err
		}
	}
	return st, nil
}

// spawn starts "veilon self-update --run" outside this service: through
// systemd-run when available (a restart of veilon.service would otherwise
// kill it with the rest of the service's cgroup), else detached.
func (s *Service) spawn() error {
	self := s.Self
	if self == "" {
		self, _ = os.Executable()
	}
	if os.Geteuid() == 0 {
		if sr, err := exec.LookPath("systemd-run"); err == nil {
			unit := "veilon-update-" + strconv.FormatInt(time.Now().Unix(), 10)
			args := []string{"--unit=" + unit, "--collect", "--quiet"}
			if c := os.Getenv("VEILON_CONFIG"); c != "" {
				args = append(args, "--setenv=VEILON_CONFIG="+c)
			}
			args = append(args, self, "self-update", "--run")
			if err := exec.Command(sr, args...).Run(); err == nil {
				return nil
			}
		}
	}
	cmd := exec.Command(self, "self-update", "--run")
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logf, err := os.OpenFile(Dir(s.DataDir)+"/updater.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout, cmd.Stderr = logf, logf
		defer logf.Close()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func writeJSONInPlace(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func chownLike(path string, fi os.FileInfo) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		_ = os.Chown(path, int(st.Uid), int(st.Gid))
	}
}
