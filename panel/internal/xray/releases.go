package xray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Release is one published Xray-core release usable on this host.
type Release struct {
	Tag         string    `json:"tag"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

// ReleaseList is the cached list returned to the API.
type ReleaseList struct {
	Releases  []Release `json:"releases"`
	FetchedAt time.Time `json:"fetched_at"`
	// Stale is true when the list could not be refreshed (rate limit or
	// network error) and an older copy is returned.
	Stale bool `json:"stale"`
	// RateLimitedUntil is set while GitHub's API rate limit is exhausted.
	RateLimitedUntil *time.Time `json:"rate_limited_until,omitempty"`
	Error            string     `json:"error,omitempty"`
}

// ErrRateLimited is returned when GitHub rate-limits us and nothing is cached.
var ErrRateLimited = errors.New("GitHub API rate limit reached; try again later")

const releasesTTL = time.Hour

// ReleasesURL is the GitHub API endpoint listing Xray-core releases.
var ReleasesURL = "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=100"

type releaseCache struct {
	mu        sync.Mutex
	list      []Release
	fetchedAt time.Time
	limitedTo time.Time
	lastErr   string
}

// Releases returns recent Xray-core releases, cached for an hour. refresh
// forces a fetch unless GitHub's rate limit is in effect.
func (m *Manager) Releases(ctx context.Context, refresh bool) (ReleaseList, error) {
	return m.releases.get(ctx, http.DefaultClient, ReleasesURL, refresh, time.Now())
}

func (c *releaseCache) get(ctx context.Context, client *http.Client, url string, refresh bool, now time.Time) (ReleaseList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fresh := len(c.list) > 0 && now.Sub(c.fetchedAt) < releasesTTL
	limited := now.Before(c.limitedTo)
	if (fresh && !refresh) || limited {
		return c.result(now, limited)
	}
	list, resetAt, err := fetchReleases(ctx, client, url)
	if err != nil {
		if !resetAt.IsZero() {
			c.limitedTo = resetAt
		}
		c.lastErr = err.Error()
		return c.result(now, now.Before(c.limitedTo))
	}
	c.list, c.fetchedAt, c.lastErr, c.limitedTo = list, now.UTC(), "", time.Time{}
	return c.result(now, false)
}

func (c *releaseCache) result(now time.Time, limited bool) (ReleaseList, error) {
	out := ReleaseList{Releases: append([]Release{}, c.list...), FetchedAt: c.fetchedAt}
	if limited {
		t := c.limitedTo.UTC()
		out.RateLimitedUntil = &t
	}
	if c.lastErr != "" || limited {
		out.Stale = true
		out.Error = c.lastErr
	}
	if len(c.list) == 0 {
		if limited {
			return out, ErrRateLimited
		}
		if c.lastErr != "" {
			return out, errors.New(c.lastErr)
		}
	}
	return out, nil
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// stableTagRe matches plain release tags (v1.2.3); any suffix such as
// -rc1, -beta, -alpha or -pre marks a test build.
var stableTagRe = regexp.MustCompile(`^v\d+\.\d+(\.\d+)?$`)

// stableReleases keeps only stable releases: not drafts, not marked
// prerelease on GitHub, a plain version tag, and (when asset is set) a build
// for this host.
func stableReleases(raw []ghRelease, asset string) []Release {
	out := []Release{}
	for _, r := range raw {
		if r.Draft || r.Prerelease || !stableTagRe.MatchString(r.TagName) {
			continue
		}
		if asset != "" {
			has := false
			for _, a := range r.Assets {
				if a.Name == asset {
					has = true
					break
				}
			}
			if !has {
				continue
			}
		}
		out = append(out, Release{Tag: r.TagName, PublishedAt: r.PublishedAt})
	}
	return out
}

// fetchReleases reads the GitHub releases API. On a rate limit it returns
// the reset time.
func fetchReleases(ctx context.Context, client *http.Client, url string) ([]Release, time.Time, error) {
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veilon-panel")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if resp.StatusCode == http.StatusTooManyRequests || resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
			return nil, rateLimitReset(resp.Header, time.Now()), ErrRateLimited
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("fetch releases: %s", resp.Status)
	}
	var raw []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&raw); err != nil {
		return nil, time.Time{}, fmt.Errorf("parse releases: %w", err)
	}
	asset, _ := AssetName(runtime.GOOS, runtime.GOARCH)
	out := stableReleases(raw, asset)
	return out, time.Time{}, nil
}

func rateLimitReset(h http.Header, now time.Time) time.Time {
	if s := h.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return now.Add(time.Duration(n) * time.Second)
		}
	}
	if s := h.Get("X-RateLimit-Reset"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0)
		}
	}
	return now.Add(10 * time.Minute)
}

// GeoBaseURL hosts geoip.dat / geosite.dat with .sha256sum files.
var GeoBaseURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/"

// GeoFiles are the routing data files Xray loads.
var GeoFiles = []string{"geoip.dat", "geosite.dat"}

// GeoInfo describes an installed geo data file.
type GeoInfo struct {
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// GeoStatus lists the installed geo files.
func (m *Manager) GeoStatus() []GeoInfo {
	dir := filepath.Dir(m.opt.BinPath)
	out := make([]GeoInfo, 0, len(GeoFiles))
	for _, n := range GeoFiles {
		g := GeoInfo{Name: n}
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t := st.ModTime().UTC()
			g.Size, g.UpdatedAt = st.Size(), &t
		}
		out = append(out, g)
	}
	return out
}

func (m *Manager) updateGeo(ctx context.Context) (bool, error) {
	dir := filepath.Dir(m.opt.BinPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return false, err
	}
	stage, err := os.MkdirTemp(dir, ".geo-update-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stage)
	client := httpClient()
	var swaps []swap
	for _, n := range GeoFiles {
		m.logs.add("[veilon] downloading " + n)
		dst := filepath.Join(stage, n)
		if err := downloadVerified(ctx, client, GeoBaseURL+n, dst); err != nil {
			return false, err
		}
		swaps = append(swaps, swap{src: dst, dst: filepath.Join(dir, n)})
	}
	// Check the new data with the current binary before swapping it in.
	if _, err := os.Stat(m.opt.BinPath); err == nil {
		if err := m.testConfig(ctx, m.opt.BinPath, stage); err != nil {
			return false, fmt.Errorf("the current config is rejected with the new geo files: %w", err)
		}
	}
	return m.swapAndVerify(ctx, swaps)
}

// downloadVerified fetches url into dst, requiring url+".sha256sum" to match.
func downloadVerified(ctx context.Context, client *http.Client, url, dst string) error {
	var sumFile strings.Builder
	if err := fetch(ctx, client, url+".sha256sum", &sumFile); err != nil {
		return fmt.Errorf("checksum unavailable: %w", err)
	}
	want := strings.Fields(sumFile.String())
	if len(want) == 0 || len(want[0]) != 64 {
		return fmt.Errorf("bad checksum file for %s", filepath.Base(dst))
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	ferr := fetch(ctx, client, url, io.MultiWriter(f, h))
	if cerr := f.Close(); ferr == nil {
		ferr = cerr
	}
	if ferr != nil {
		return ferr
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want[0]) {
		return fmt.Errorf("%s checksum mismatch: got %s want %s", filepath.Base(dst), got, want[0])
	}
	return nil
}
