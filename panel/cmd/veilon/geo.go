package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// geoSource publishes geoip.dat / geosite.dat with .sha256sum files.
const geoSource = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/"

// updateGeoFiles downloads fresh geoip.dat and geosite.dat into dir,
// verifying each against its published SHA-256 sum, and replaces the old
// files atomically.
func updateGeoFiles(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	cl := &http.Client{Timeout: 5 * time.Minute}
	get := func(url string, w io.Writer) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("GET %s: %s", url, resp.Status)
		}
		_, err = io.Copy(w, resp.Body)
		return err
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		var sum strings.Builder
		if err := get(geoSource+name+".sha256sum", &sum); err != nil {
			return err
		}
		want, _, _ := strings.Cut(strings.TrimSpace(sum.String()), " ")
		tmp, err := os.CreateTemp(dir, name+".*")
		if err != nil {
			return err
		}
		h := sha256.New()
		err = get(geoSource+name, io.MultiWriter(tmp, h))
		tmp.Close()
		if err == nil && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
			err = fmt.Errorf("%s: checksum mismatch", name)
		}
		if err == nil {
			_ = os.Chmod(tmp.Name(), 0o644)
			err = os.Rename(tmp.Name(), filepath.Join(dir, name))
		}
		if err != nil {
			os.Remove(tmp.Name())
			return err
		}
	}
	return nil
}
