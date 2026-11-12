// Package backup makes copies of the panel database and restores one.
//
// A restore is never applied to the live database. The uploaded file is
// checked, a safety copy of the current database is written, and the file
// is staged in <data dir>/restore. The panel then exits with RestartCode,
// systemd (Restart=on-failure) starts it again, and ApplyPending puts the
// staged file in place before the database is opened.
package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// RestartCode is the exit status that asks for a restart to apply a staged
// restore. It is not 0, so systemd's Restart=on-failure starts the panel
// again.
const RestartCode = 75

// KeepSafety is how many safety copies are kept.
const KeepSafety = 5

var (
	sqliteMagic = []byte("SQLite format 3\x00")
	pgMagic     = []byte("PGDMP")
)

// Service creates and restores backups of the panel database.
type Service struct {
	// DB is the live database (SQLite copies are taken through it).
	DB      *gorm.DB
	Driver  string // sqlite | postgres
	DSN     string
	DataDir string
	Version string
	// Exit is called once, ExitDelay after a restore was staged, to stop
	// the panel so the restore is applied at the next start.
	Exit      func()
	ExitDelay time.Duration
	// Now is the clock (tests).
	Now func() time.Time

	exitOnce sync.Once
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// StageDir is where a restore waits for the next start.
func StageDir(dataDir string) string { return filepath.Join(dataDir, "restore") }

// SafetyDir keeps the copies taken before a restore.
func SafetyDir(dataDir string) string { return filepath.Join(dataDir, "backups") }

func (s *Service) ext() string {
	if s.Driver == "postgres" {
		return ".pgdump"
	}
	return ".db"
}

// Create returns a consistent copy of the database.
func (s *Service) Create(ctx context.Context) (string, []byte, error) {
	name := fmt.Sprintf("veilon-%s-%s%s", strings.TrimPrefix(s.Version, "v"), s.now().Format("20060102-150405"), s.ext())
	tmpDir, err := os.MkdirTemp(s.DataDir, ".backup-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmpDir)
	dst := filepath.Join(tmpDir, name)
	switch s.Driver {
	case "sqlite":
		if s.DB == nil {
			return "", nil, errors.New("database not available")
		}
		if err := s.DB.WithContext(ctx).Exec("VACUUM INTO ?", dst).Error; err != nil {
			return "", nil, fmt.Errorf("sqlite copy: %w", err)
		}
	case "postgres":
		pgDump, err := exec.LookPath("pg_dump")
		if err != nil {
			return "", nil, errors.New("pg_dump is not installed on this server")
		}
		out, err := exec.CommandContext(ctx, pgDump, "--format=custom", "--file="+dst, "--dbname="+s.DSN).CombinedOutput()
		if err != nil {
			return "", nil, fmt.Errorf("pg_dump: %v: %s", err, strings.TrimSpace(string(out)))
		}
	default:
		return "", nil, fmt.Errorf("unknown database driver %q", s.Driver)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		return "", nil, err
	}
	return name, data, nil
}

// Check validates an uploaded backup for this panel and describes it.
func (s *Service) Check(name string, data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, sqliteMagic):
		if s.Driver != "sqlite" {
			return "", errors.New("this is a SQLite backup, but this panel uses PostgreSQL")
		}
		return s.checkSQLite(data)
	case bytes.HasPrefix(data, pgMagic):
		if s.Driver != "postgres" {
			return "", errors.New("this is a PostgreSQL backup, but this panel uses SQLite")
		}
		return s.checkPG(data)
	}
	return "", errors.New("not a Veilon database backup")
}

func (s *Service) checkSQLite(data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp(s.DataDir, ".check-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	path := filepath.Join(tmpDir, "check.db")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return "", err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return "", err
	}
	defer sqlDB.Close()
	var ok string
	if err := gdb.Raw("PRAGMA quick_check").Scan(&ok).Error; err != nil || ok != "ok" {
		return "", fmt.Errorf("the database file is damaged (%s)", firstNonEmpty(ok, fmt.Sprint(err)))
	}
	for _, tbl := range []string{"schema_migrations", "admins", "settings", "users", "inbounds"} {
		var n int64
		gdb.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tbl).Scan(&n)
		if n == 0 {
			return "", fmt.Errorf("not a Veilon database (no %s table)", tbl)
		}
	}
	schema := db.SchemaVersion(gdb)
	if schema > db.LatestSchema() {
		return "", fmt.Errorf("the backup is from a newer panel (schema %d, this panel knows %d); update the panel first", schema, db.LatestSchema())
	}
	var admins, users, inbounds int64
	gdb.Raw("SELECT COUNT(*) FROM admins").Scan(&admins)
	gdb.Raw("SELECT COUNT(*) FROM users").Scan(&users)
	gdb.Raw("SELECT COUNT(*) FROM inbounds").Scan(&inbounds)
	if admins == 0 {
		return "", errors.New("the backup has no admin account")
	}
	return fmt.Sprintf("SQLite, schema %d, %d users, %d inbounds, %d admins", schema, users, inbounds, admins), nil
}

func (s *Service) checkPG(data []byte) (string, error) {
	pgRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		return "", errors.New("pg_restore is not installed on this server")
	}
	tmp, err := os.CreateTemp(s.DataDir, ".check-*.pgdump")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	out, err := exec.Command(pgRestore, "--list", tmp.Name()).Output()
	if err != nil {
		return "", errors.New("the dump file cannot be read")
	}
	list := string(out)
	for _, tbl := range []string{"schema_migrations", "admins", "users", "inbounds"} {
		if !strings.Contains(list, " TABLE public "+tbl+" ") && !strings.Contains(list, " TABLE DATA public "+tbl+" ") {
			return "", fmt.Errorf("not a Veilon database (no %s table)", tbl)
		}
	}
	return "PostgreSQL dump", nil
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// pendingInfo describes a staged restore.
type pendingInfo struct {
	Name     string    `json:"name"`
	File     string    `json:"file"`
	Safety   string    `json:"safety"`
	Driver   string    `json:"driver"`
	StagedAt time.Time `json:"staged_at"`
}

// Result is the outcome of the last applied restore.
type Result struct {
	Name      string    `json:"name"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	Safety    string    `json:"safety"`
	AppliedAt time.Time `json:"applied_at"`
}

// Restore checks and stages a backup, keeps a safety copy of the current
// database, and then asks for a restart (Exit). It returns the path of the
// safety copy.
func (s *Service) Restore(name string, data []byte) (string, error) {
	if _, err := s.Check(name, data); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, cur, err := s.Create(ctx)
	if err != nil {
		return "", fmt.Errorf("safety copy: %w", err)
	}
	if err := os.MkdirAll(SafetyDir(s.DataDir), 0o700); err != nil {
		return "", err
	}
	safety := filepath.Join(SafetyDir(s.DataDir), "before-restore-"+s.now().Format("20060102-150405")+s.ext())
	if err := os.WriteFile(safety, cur, 0o600); err != nil {
		return "", fmt.Errorf("safety copy: %w", err)
	}
	pruneSafety(SafetyDir(s.DataDir), KeepSafety)
	dir := StageDir(s.DataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(dir, "pending"+s.ext())
	if err := writeAtomic(file, data); err != nil {
		return "", err
	}
	info := pendingInfo{Name: filepath.Base(name), File: file, Safety: safety, Driver: s.Driver, StagedAt: s.now()}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err := writeAtomic(filepath.Join(dir, "pending.json"), b); err != nil {
		return "", err
	}
	slog.Warn("database restore staged; restarting the panel to apply it", "file", info.Name, "safety_copy", safety)
	if s.Exit != nil {
		s.exitOnce.Do(func() {
			d := s.ExitDelay
			if d == 0 {
				d = 3 * time.Second
			}
			time.AfterFunc(d, s.Exit)
		})
	}
	return safety, nil
}

// LastResult returns the outcome of the last applied restore (nil: none).
func (s *Service) LastResult() *Result {
	b, err := os.ReadFile(filepath.Join(StageDir(s.DataDir), "result.json"))
	if err != nil {
		return nil
	}
	var r Result
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	return &r
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pruneSafety(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "before-restore-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// ApplyPending puts a staged restore in place. It runs at start, before
// the database is opened. A failed restore is recorded and dropped, so the
// panel starts with the database it had (the safety copy stays).
func ApplyPending(driver, dsn, dataDir string) (applied bool, err error) {
	dir := StageDir(dataDir)
	b, err := os.ReadFile(filepath.Join(dir, "pending.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var p pendingInfo
	if err := json.Unmarshal(b, &p); err != nil {
		_ = os.Remove(filepath.Join(dir, "pending.json"))
		return false, fmt.Errorf("staged restore unreadable: %w", err)
	}
	res := Result{Name: p.Name, Safety: p.Safety, AppliedAt: time.Now().UTC()}
	defer func() {
		res.OK = err == nil
		if err != nil {
			res.Error = err.Error()
		}
		rb, _ := json.MarshalIndent(res, "", "  ")
		_ = writeAtomic(filepath.Join(dir, "result.json"), rb)
		_ = os.Remove(filepath.Join(dir, "pending.json"))
		_ = os.Remove(p.File)
	}()
	if p.Driver != driver {
		return false, fmt.Errorf("staged restore is for %s, the panel uses %s", p.Driver, driver)
	}
	switch driver {
	case "sqlite":
		err = applySQLite(dsn, p.File)
	case "postgres":
		err = applyPG(dsn, p.File)
	default:
		err = fmt.Errorf("unknown database driver %q", driver)
	}
	return err == nil, err
}

func sqlitePath(dsn string) string {
	path, _, _ := strings.Cut(strings.TrimPrefix(dsn, "file:"), "?")
	return path
}

func applySQLite(dsn, file string) error {
	path := sqlitePath(dsn)
	if path == "" || path == ":memory:" {
		return errors.New("no database file")
	}
	// Stale WAL files of the old database would be replayed into the new
	// one: they go with the old file (the safety copy has their content).
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(path + sfx)
	}
	tmp := path + ".restore"
	if err := copyFile(file, tmp); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp, fi.Mode().Perm())
	}
	return os.Rename(tmp, path)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func applyPG(dsn, file string) error {
	pgRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		return errors.New("pg_restore is not installed")
	}
	// One transaction: a failure leaves the database as it was.
	out, err := exec.Command(pgRestore, "--clean", "--if-exists", "--no-owner", "--no-privileges", "--single-transaction",
		"--exit-on-error", "--dbname="+dsn, file).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_restore: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
