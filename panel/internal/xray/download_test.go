package xray

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// DownloadBinary takes a release from the mirror in VEILON_XRAY_RELEASE_BASE,
// requires its .dgst to match and extracts only the binary.
func TestDownloadBinaryFromMirror(t *testing.T) {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, body := range map[string]string{"xray": "#!/bin/sh\necho Xray 26.10.31\n", "geoip.dat": "geo", "README.md": "x"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	sum := sha256.Sum256(zb.Bytes())
	dgst := "MD5= 00\nSHA2-256= " + hex.EncodeToString(sum[:]) + "\n"
	bad := false
	noDgst := false
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/mirror/v26.10.31/" + asset:
			_, _ = w.Write(zb.Bytes())
		case "/mirror/v26.10.31/" + asset + ".dgst":
			if noDgst {
				http.NotFound(w, r)
				return
			}
			if bad {
				_, _ = w.Write([]byte(strings.Replace(dgst, hex.EncodeToString(sum[:])[:4], "ffff", 1)))
				return
			}
			_, _ = w.Write([]byte(dgst))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv(ReleaseBaseEnv, srv.URL+"/mirror/")
	ctx := context.Background()

	if err := DownloadBinary(ctx, t.TempDir(), "latest"); err != ErrBadVersion {
		t.Fatalf("latest accepted: %v", err)
	}
	dir := t.TempDir()
	if err := DownloadBinary(ctx, dir, "v26.10.31"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "xray")); err != nil {
		t.Fatal("binary not extracted")
	}
	if _, err := os.Stat(filepath.Join(dir, "geoip.dat")); err == nil {
		t.Fatal("geo file extracted with the binary")
	}
	if paths[0] != "/mirror/v26.10.31/"+asset {
		t.Fatalf("paths %v", paths)
	}
	bad = true
	if err := DownloadBinary(ctx, t.TempDir(), "v26.10.31"); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("bad digest: %v", err)
	}
	bad, noDgst = false, true
	if err := DownloadBinary(ctx, t.TempDir(), "v26.10.31"); err == nil || !strings.Contains(err.Error(), "digest unavailable") {
		t.Fatalf("missing digest: %v", err)
	}
}

func TestReleaseBaseDefault(t *testing.T) {
	t.Setenv(ReleaseBaseEnv, "")
	if got := releaseBase("v26.10.31"); got != "https://github.com/XTLS/Xray-core/releases/download/v26.10.31/" {
		t.Fatal(got)
	}
}
