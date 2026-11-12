package veiloncore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service"
)

const vlessTLSWS = "vless://b831381d-6324-4d53-ad4f-8cda48b30811@198.51.100.2:443?security=tls&sni=cdn.example.com&fp=chrome&type=ws&path=%2Fws&host=cdn.example.com#vless-ws"

// collectPaths returns every string value under a key that looks like a
// file path in a generated config ("path" of rule-sets and cache_file …).
func collectPaths(v any, key string, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			collectPaths(e, k, out)
		}
	case []any:
		for _, e := range x {
			collectPaths(e, key, out)
		}
	case string:
		switch key {
		case "path", "cache_file", "output", "file", "directory", "working_directory", "external_ui":
			*out = append(*out, x)
		}
	}
}

// bundledRules is the app's rule-set directory (resolved before any test
// changes the working directory).
var bundledRules = func() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..", "app", "assets", "rules")
}()

func appSettings(t *testing.T) Settings {
	t.Helper()
	files := t.TempDir()
	rules := filepath.Join(files, "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		t.Fatal(err)
	}
	// The real bundled rule-sets (sing-box parses them at start).
	for _, f := range []string{rsGeoIPIR, rsGeositeIR, rsGeositeAds} {
		b, err := os.ReadFile(filepath.Join(bundledRules, f))
		if err != nil {
			t.Fatalf("rule-set %s: %v", f, err)
		}
		_ = os.WriteFile(filepath.Join(rules, f), b, 0o644)
	}
	b, _ := json.Marshal(map[string]any{"assetDir": rules, "dataDir": filepath.Join(files, "core"),
		"tempDir": filepath.Join(files, "cache"), "bypassIran": true, "blockAds": true})
	st, err := parseSettings(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// The v1.0.0 bug: sing-box opened "cache.db" relative to cwd ("/" on
// Android). The generated config must name only absolute paths, and the
// cache file must live in DataDir.
func TestConfigNoRelativePaths(t *testing.T) {
	p, err := ParseLink(vlessTLSWS)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := ResolveCore(p, DefaultSettings()); c != CoreSingbox {
		t.Fatalf("vless tls ws core = %s, want sing-box", c)
	}
	for _, withTun := range []bool{true, false} {
		for name, st := range map[string]Settings{"app": appSettings(t), "empty": func() Settings { s, _ := parseSettings(""); return s }()} {
			plan, _, err := prepare(context.Background(), p, st, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg := buildSingboxConfig(plan, st, withTun)
			b, _ := json.Marshal(cfg)
			var generic any
			_ = json.Unmarshal(b, &generic)
			var paths []string
			collectPaths(generic, "", &paths)
			if len(paths) == 0 {
				t.Fatalf("%s: no paths found (cache_file missing?)", name)
			}
			for _, x := range paths {
				if !filepath.IsAbs(x) {
					t.Errorf("%s tun=%v: relative path %q in config", name, withTun, x)
				}
			}
			cf := cfg["experimental"].(obj)["cache_file"].(obj)["path"].(string)
			if name == "app" && cf != filepath.Join(st.DataDir, "sing-box", "cache.db") {
				t.Errorf("cache_file path = %s, want under %s", cf, st.DataDir)
			}
			if !strings.HasSuffix(cf, "cache.db") {
				t.Errorf("cache path %s", cf)
			}
		}
	}
	// Relative inputs are made absolute.
	st, _ := parseSettings(`{"assetDir":"rules","dataDir":"core"}`)
	for _, d := range []string{st.AssetDir, st.DataDir, st.TempDir} {
		if !filepath.IsAbs(d) {
			t.Errorf("not absolute: %q", d)
		}
	}
}

// Runtime proof: start a real sing-box instance (with a PlatformLogWriter,
// which forces the cache file exactly like the Android VPN session) while
// the process cwd is a read-only directory.
func TestSingboxStartReadOnlyCwd(t *testing.T) {
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ro, 0o755)
	if f, err := os.Create(filepath.Join(ro, "probe")); err == nil {
		f.Close()
		t.Skip("cannot make a read-only dir here (running as root?)")
	}
	t.Setenv("TMPDIR", os.Getenv("TMPDIR"))
	t.Setenv("XRAY_LOCATION_ASSET", "")
	t.Setenv("xray.location.asset", "")
	wd, _ := os.Getwd()
	if err := os.Chdir(ro); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	p, _ := ParseLink(vlessTLSWS)
	st := appSettings(t)
	start := func(cfg obj) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bctx := withFileManager(boxContext(ctx), st)
		opts, err := parseBoxOptions(bctx, cfg)
		if err != nil {
			return err
		}
		bctx = service.ContextWith[adapter.PlatformInterface](bctx, newBoxPlatform(nil, ""))
		inst, err := box.New(box.Options{Context: bctx, Options: opts, PlatformLogWriter: platformLog{}})
		if err != nil {
			return err
		}
		defer inst.Close()
		return inst.Start()
	}
	plan, _, err := prepare(context.Background(), p, st, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Negative control: the v1.0.0 config (no cache_file path) without the
	// file-manager base path fails exactly like on the phone.
	old := buildSingboxConfig(plan, st, false)
	delete(old, "experimental")
	ctxOld := func() error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bctx := boxContext(ctx)
		opts, err := parseBoxOptions(bctx, old)
		if err != nil {
			return err
		}
		bctx = service.ContextWith[adapter.PlatformInterface](bctx, newBoxPlatform(nil, ""))
		inst, err := box.New(box.Options{Context: bctx, Options: opts, PlatformLogWriter: platformLog{}})
		if err != nil {
			return err
		}
		defer inst.Close()
		return inst.Start()
	}
	if err := ctxOld(); err == nil || !strings.Contains(err.Error(), "cache") {
		t.Fatalf("negative control: want cache-file error, got %v", err)
	} else {
		t.Logf("v1.0.0 behaviour reproduced: %v", err)
	}
	// Only the state directory (newSession's preparePaths creates it); the
	// cwd stays read-only so this proves the absolute cache path alone.
	if err := os.MkdirAll(filepath.Dir(singboxCachePath(st)), 0o700); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.Getwd(); now != ro {
		t.Fatalf("cwd = %s, want read-only %s", now, ro)
	}
	if err := start(buildSingboxConfig(plan, st, false)); err != nil {
		t.Fatalf("start with read-only cwd: %v", err)
	}
	if _, err := os.Stat(singboxCachePath(st)); err != nil {
		t.Fatalf("cache file not in DataDir: %v", err)
	}
	t.Logf("v1.0.1: started with read-only cwd, cache file at %s", singboxCachePath(st))
	// preparePaths moves cwd away from a read-only directory.
	preparePaths(st)
	if now, _ := os.Getwd(); now != st.DataDir {
		t.Errorf("cwd = %s, want %s", now, st.DataDir)
	}
}
