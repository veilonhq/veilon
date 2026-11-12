package veiloncore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sagernet/sing/service/filemanager"
)

// On Android the process working directory is "/" (read-only), and
// os.TempDir() is /data/local/tmp (not writable by apps). Every file a core
// may create must therefore live under an absolute, app-owned directory:
//
//   - Settings.DataDir  (filesDir/core from Kotlin): sing-box cache.db and
//     any other state; also the working directory and sing-box base path.
//   - Settings.TempDir  (cacheDir from Kotlin): $TMPDIR for os.CreateTemp.
//   - Settings.AssetDir (filesDir/rules): read-only rule-set / geo files.
//
// sing-box opens its cache file ("cache.db", relative by default) whenever a
// PlatformLogWriter is set (box.go: needCacheFile), even without
// experimental.cache_file — that was the v1.0.0 "initialize cache-file: open
// cache.db: read-only file system" failure.

// dataDirOf returns the absolute directory for core state.
func dataDirOf(st Settings) string {
	for _, d := range []string{st.DataDir, parentIfAbs(st.AssetDir), st.TempDir} {
		if d != "" && filepath.IsAbs(d) {
			return filepath.Clean(d)
		}
	}
	return filepath.Join(os.TempDir(), "veilon")
}

// tempDirOf returns the absolute temp directory.
func tempDirOf(st Settings) string {
	if st.TempDir != "" && filepath.IsAbs(st.TempDir) {
		return filepath.Clean(st.TempDir)
	}
	return filepath.Join(dataDirOf(st), "tmp")
}

func parentIfAbs(d string) string {
	if d == "" || !filepath.IsAbs(d) {
		return ""
	}
	return filepath.Dir(filepath.Clean(d))
}

// assetDirOf returns the absolute asset directory ("" if unset).
func assetDirOf(st Settings) string {
	if st.AssetDir == "" {
		return ""
	}
	if filepath.IsAbs(st.AssetDir) {
		return filepath.Clean(st.AssetDir)
	}
	return filepath.Join(dataDirOf(st), st.AssetDir)
}

// singboxCachePath is the absolute path of sing-box's cache file.
func singboxCachePath(st Settings) string {
	return filepath.Join(dataDirOf(st), "sing-box", "cache.db")
}

// normalizePaths makes every directory in st absolute.
func (st *Settings) normalizePaths() {
	st.AssetDir = assetDirOf(*st)
	st.DataDir = dataDirOf(*st)
	st.TempDir = tempDirOf(*st)
}

var envMu sync.Mutex

// preparePaths creates the state directories and points process-wide
// defaults (cwd, $TMPDIR, Xray asset location) at app storage, so no core can
// fall back to a relative path under "/". Best effort: errors are logged.
func preparePaths(st Settings) {
	envMu.Lock()
	defer envMu.Unlock()
	data, tmp := dataDirOf(st), tempDirOf(st)
	if st.DataDir != "" {
		defer InitCrashLog(data)
	}
	for _, d := range []string{data, filepath.Dir(singboxCachePath(st)), tmp} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			pushLogLevel("warn", fmt.Sprintf("paths: mkdir %s: %v", d, err))
		}
	}
	// os.TempDir() is $TMPDIR, or /data/local/tmp on Android (not
	// writable by apps) / /tmp elsewhere.
	if !dirWritable(os.TempDir()) {
		_ = os.Setenv("TMPDIR", tmp)
	}
	if a := assetDirOf(st); a != "" {
		_ = os.Setenv("XRAY_LOCATION_ASSET", a)
		_ = os.Setenv("xray.location.asset", a)
	}
	if wd, err := os.Getwd(); err != nil || !dirWritable(wd) {
		if err := os.Chdir(data); err != nil {
			pushLogLevel("warn", fmt.Sprintf("paths: chdir %s: %v", data, err))
		}
	}
}

func dirWritable(d string) bool {
	if d == "" || !filepath.IsAbs(d) {
		return false
	}
	f, err := os.CreateTemp(d, ".w*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// withFileManager makes sing-box resolve relative names against DataDir.
func withFileManager(ctx context.Context, st Settings) context.Context {
	return filemanager.WithDefault(ctx, dataDirOf(st), tempDirOf(st), os.Getuid(), os.Getgid())
}
