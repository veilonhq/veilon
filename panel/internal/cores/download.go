package cores

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Minute}

// ResolveLatest returns the tag of a GitHub repository's latest release by
// following the /releases/latest redirect (no API rate limits).
func ResolveLatest(ctx context.Context, repo string) (string, error) {
	c := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://github.com/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", fmt.Errorf("cannot resolve latest release of %s", repo)
	}
	return loc[i+5:], nil
}

func fetchTo(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, 512<<20))
	return err
}

// downloadTarBinary downloads a .tar.gz release asset, optionally checks it
// against a checksums file (lines "<sha256>  <name>"), and extracts the file
// whose base name is binName to dst.
func downloadTarBinary(ctx context.Context, url, checksumsURL, binName, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "dl-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	slog.Info("downloading engine", "url", url)
	h := sha256.New()
	if err := fetchTo(ctx, url, io.MultiWriter(tmp, h)); err != nil {
		return err
	}
	if checksumsURL != "" {
		var sb strings.Builder
		if err := fetchTo(ctx, checksumsURL, &sb); err != nil {
			return fmt.Errorf("checksums: %w", err)
		}
		want := ""
		base := url[strings.LastIndex(url, "/")+1:]
		sc := bufio.NewScanner(strings.NewReader(sb.String()))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) >= 2 && strings.TrimPrefix(f[1], "*") == base {
				want = f[0]
			}
		}
		got := hex.EncodeToString(h.Sum(nil))
		if want == "" {
			return fmt.Errorf("no checksum listed for %s", base)
		}
		if !strings.EqualFold(want, got) {
			return fmt.Errorf("checksum mismatch for %s", base)
		}
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(tmp)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hd.Typeflag != tar.TypeReg || filepath.Base(hd.Name) != binName {
			continue
		}
		part := dst + ".tmp"
		out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, 512<<20)); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Rename(part, dst)
	}
	return fmt.Errorf("%s not found in %s", binName, url)
}

// goArchName maps GOARCH to the asset names used by sing-box and mtg.
func goArchName(goarch string) (string, error) {
	switch goarch {
	case "amd64", "arm64", "386":
		return goarch, nil
	case "arm":
		return "armv7", nil
	}
	return "", fmt.Errorf("unsupported arch %s", goarch)
}
