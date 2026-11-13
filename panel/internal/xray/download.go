package xray

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AssetName returns the official Xray-core release asset for this host.
func AssetName(goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("unsupported OS %s", goos)
	}
	switch goarch {
	case "amd64":
		return "Xray-linux-64.zip", nil
	case "arm64":
		return "Xray-linux-arm64-v8a.zip", nil
	case "386":
		return "Xray-linux-32.zip", nil
	case "arm":
		return "Xray-linux-arm32-v7a.zip", nil
	}
	return "", fmt.Errorf("unsupported arch %s", goarch)
}

// Download fetches the Xray release (version "latest" or a tag) into dir,
// verifies the published SHA-256 digest when available and extracts the
// binary and geo data files.
func Download(ctx context.Context, dir, version string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return downloadRelease(ctx, &http.Client{Timeout: 5 * time.Minute}, releaseBase(version), dir, false,
		map[string]os.FileMode{"xray": 0o755, "geoip.dat": 0o644, "geosite.dat": 0o644})
}

// ReleaseBaseEnv names an optional mirror of the official Xray-core
// releases: <base>/<tag>/<asset> (and <asset>.dgst) instead of GitHub. The
// digest is still required for installs from the panel.
const ReleaseBaseEnv = "VEILON_XRAY_RELEASE_BASE"

// releaseBase is the download URL prefix of a release ("" or "latest" =
// newest release).
func releaseBase(version string) string {
	if b := strings.TrimRight(strings.TrimSpace(os.Getenv(ReleaseBaseEnv)), "/"); b != "" {
		if version == "" {
			version = "latest"
		}
		return b + "/" + version + "/"
	}
	if version == "" || version == "latest" {
		return "https://github.com/XTLS/Xray-core/releases/latest/download/"
	}
	return "https://github.com/XTLS/Xray-core/releases/download/" + version + "/"
}

// ValidTag reports whether v looks like an Xray-core release tag (v26.10.31).
func ValidTag(v string) bool { return tagRe.MatchString(v) }

// DetectVersion runs "bin version" and returns the version it reports
// ("" when the binary does not run here).
func DetectVersion(bin string) string { return detectVersion(bin) }

// DownloadBinary downloads the release tag for this host into dir and
// extracts only the xray binary. The release's SHA-256 digest (.dgst) is
// required and must match. The geo files are left alone.
func DownloadBinary(ctx context.Context, dir, version string) error {
	if !tagRe.MatchString(version) {
		return ErrBadVersion
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return downloadRelease(ctx, httpClient(), releaseBase(version), dir, true, map[string]os.FileMode{"xray": 0o755})
}

func httpClient() *http.Client { return &http.Client{Timeout: 5 * time.Minute} }

// downloadRelease downloads this host's release zip from base, checks its
// digest (mandatory when requireDigest) and extracts the wanted files into
// dir. The xray binary must be among them.
func downloadRelease(ctx context.Context, client *http.Client, base, dir string, requireDigest bool, wanted map[string]os.FileMode) error {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "xray-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	slog.Info("downloading xray-core", "url", base+asset)
	h := sha256.New()
	if err := fetch(ctx, client, base+asset, io.MultiWriter(tmp, h)); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	var dg strings.Builder
	if err := fetch(ctx, client, base+asset+".dgst", &dg); err == nil {
		want := parseDigest(dg.String())
		if want == "" && requireDigest {
			return fmt.Errorf("xray digest file has no SHA-256 entry")
		}
		if want != "" && !strings.EqualFold(want, sum) {
			return fmt.Errorf("xray digest mismatch: got %s want %s", sum, want)
		}
	} else if requireDigest {
		return fmt.Errorf("xray digest unavailable: %w", err)
	} else {
		slog.Warn("xray digest unavailable, skipping verification", "err", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	st, err := tmp.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(tmp, st.Size())
	if err != nil {
		return err
	}
	found := false
	for _, f := range zr.File {
		mode, ok := wanted[filepath.Base(f.Name)]
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		if err := extract(f, filepath.Join(dir, filepath.Base(f.Name)), mode); err != nil {
			return err
		}
		if filepath.Base(f.Name) == "xray" {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("xray binary not found in %s", asset)
	}
	return nil
}

func parseDigest(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(strings.ToUpper(line), "SHA2-256=") || strings.HasPrefix(strings.ToUpper(line), "SHA256=") {
			_, v, _ := strings.Cut(line, "=")
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fetch(ctx context.Context, c *http.Client, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func extract(f *zip.File, dst string, mode os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, 512<<20)); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
