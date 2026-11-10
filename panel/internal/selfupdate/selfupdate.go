// Package selfupdate keeps the panel up to date: a background checker
// looks for newer stable releases, and a privileged updater (run by a
// separate systemd unit, or detached in development) downloads the release,
// verifies its checksum, backs up the binary and the database, swaps the
// binary atomically, restarts the service and rolls back when the new
// version does not come up healthy.
package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAPI lists the panel's releases.
const DefaultAPI = "https://api.github.com/repos/veilonhq/veilon/releases?per_page=20"

// APIURL is the release list the checker and the updater read (overridable
// with VEILON_UPDATE_API, for tests against a local release source).
func APIURL() string {
	if u := os.Getenv("VEILON_UPDATE_API"); u != "" {
		return u
	}
	return DefaultAPI
}

// Release is a published stable release newer than the running version.
type Release struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	Notes       string    `json:"notes"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
	// Assets maps asset names to download URLs.
	Assets map[string]string `json:"assets,omitempty"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var semverRx = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// ParseVersion parses a stable "vX.Y.Z" tag; pre-releases and other strings
// ("dev", "v1.2.3-rc1", a commit) are not stable versions.
func ParseVersion(v string) ([3]int, bool) {
	m := semverRx.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, true
}

// Newer reports whether a is a newer stable version than b.
func Newer(a, b string) bool {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// MaxNotes caps the changelog shown in the panel.
const MaxNotes = 4000

// FetchLatest returns the newest stable release newer than current, or nil.
func FetchLatest(ctx context.Context, client *http.Client, url, current string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veilon/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
			return nil, fmt.Errorf("%w (%s)", ErrRateLimited, resp.Status)
		}
		return nil, fmt.Errorf("release list: %s", resp.Status)
	}
	var list []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("release list: %w", err)
	}
	var cands []ghRelease
	for _, r := range list {
		if r.Draft || r.Prerelease {
			continue
		}
		if _, ok := ParseVersion(r.TagName); !ok {
			continue
		}
		if Newer(r.TagName, current) {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}
	sort.Slice(cands, func(i, j int) bool { return Newer(cands[i].TagName, cands[j].TagName) })
	r := cands[0]
	notes := strings.TrimSpace(r.Body)
	if len(notes) > MaxNotes {
		notes = notes[:MaxNotes] + "…"
	}
	out := &Release{Tag: r.TagName, Name: r.Name, Notes: notes, URL: r.HTMLURL, PublishedAt: r.PublishedAt, Assets: map[string]string{}}
	for _, a := range r.Assets {
		out.Assets[a.Name] = a.URL
	}
	return out, nil
}

// ErrRateLimited means the release source refused the request because of
// its rate limit (GitHub allows 60 unauthenticated requests per hour).
var ErrRateLimited = errors.New("the release source rate limit was reached")

// ManualGap is the shortest time between two release-source requests
// started by an admin; a check inside it answers from the last result.
const ManualGap = 30 * time.Second

// Checker polls the release list in the background. It never fails loudly:
// an unreachable release source keeps the last answer and is retried later.
type Checker struct {
	Current  string
	URL      string
	Client   *http.Client
	Interval time.Duration
	// FirstDelay is the wait before the first check after start.
	FirstDelay time.Duration

	mu        sync.Mutex
	latest    *Release
	checkedAt time.Time
	failures  int

	// manual serializes admin-started checks; lastTry/lastErr remember the
	// last one for the rate limit.
	manual  sync.Mutex
	lastTry time.Time
	lastErr error
	now     func() time.Time
	// gap overrides ManualGap (tests).
	gap time.Duration

	// fetch serializes requests: a manual check and the automatic loop
	// never query the release source at the same time.
	fetch sync.Mutex
	// nextAt is when the automatic loop checks next (zero when off).
	nextAt time.Time
}

func (c *Checker) manualGap() time.Duration {
	if c.gap > 0 {
		return c.gap
	}
	return ManualGap
}

// NextCheck returns when the automatic loop checks next (zero when the
// loop is off, e.g. in a development build).
func (c *Checker) NextCheck() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nextAt
}

// recentManual reports whether an admin-started check ran within the gap
// (the automatic loop then skips its own request: no double checks).
func (c *Checker) recentManual(now time.Time) bool {
	if !c.manual.TryLock() {
		return true // a manual check is running right now
	}
	defer c.manual.Unlock()
	return !c.lastTry.IsZero() && now.Sub(c.lastTry) < c.manualGap()
}

func (c *Checker) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// CheckNow is an admin-started check: it queries the release source right
// away unless another manual check ran less than ManualGap ago, in which
// case it returns that check's outcome (cached=true) without a request.
// Concurrent calls wait for the one in flight and share its result.
func (c *Checker) CheckNow(ctx context.Context) (cached bool, retryAfter time.Duration, err error) {
	c.manual.Lock()
	defer c.manual.Unlock()
	now := c.clock()
	if !c.lastTry.IsZero() {
		if since := now.Sub(c.lastTry); since >= 0 && since < c.manualGap() {
			return true, c.manualGap() - since, c.lastErr
		}
	}
	err = c.CheckOnce(ctx)
	c.lastTry, c.lastErr = c.clock(), err
	return false, 0, err
}

// Latest returns the newest stable release newer than the running version
// (nil when up to date or not known yet).
func (c *Checker) Latest() (*Release, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latest, c.checkedAt
}

// CheckOnce queries the release source once. The automatic loop and
// manual checks both go through it and share the cached answer.
func (c *Checker) CheckOnce(ctx context.Context) error {
	c.fetch.Lock()
	defer c.fetch.Unlock()
	cl := c.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	url := c.URL
	if url == "" {
		url = APIURL()
	}
	r, err := FetchLatest(ctx, cl, url, c.Current)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.failures++
		if c.failures == 1 {
			slog.Info("update check failed; will retry later", "err", err)
		}
		return err
	}
	if c.failures > 0 {
		slog.Info("update check works again")
	}
	c.failures = 0
	c.latest, c.checkedAt = r, time.Now().UTC()
	if r != nil {
		slog.Info("a newer Veilon release is available", "current", c.Current, "latest", r.Tag)
	}
	return nil
}

// Run checks after FirstDelay and then every Interval (default 6h). After
// failures it backs off (30 min, doubling, capped at the interval). Manual
// checks (CheckNow) do not move this schedule; when one ran moments before
// a scheduled check, the scheduled check reuses its answer instead of
// querying the release source again.
func (c *Checker) Run(ctx context.Context) {
	if _, ok := ParseVersion(c.Current); !ok {
		slog.Info("update checks off: not a release build", "version", c.Current)
		return
	}
	every := c.Interval
	if every <= 0 {
		every = 6 * time.Hour
	}
	defer c.setNext(time.Time{})
	wait := c.FirstDelay
	for {
		c.setNext(c.clock().Add(wait))
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		wait = every
		if c.recentManual(c.clock()) {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		err := c.CheckOnce(cctx)
		cancel()
		if err != nil {
			c.mu.Lock()
			n := c.failures
			c.mu.Unlock()
			wait = 30 * time.Minute << min(n-1, 4)
			if wait > every {
				wait = every
			}
		}
	}
}

func (c *Checker) setNext(t time.Time) {
	c.mu.Lock()
	c.nextAt = t
	c.mu.Unlock()
}

// --- request / status files shared by the panel and the updater ---------

// Phases of an update, in order.
const (
	PhaseQueued     = "queued"
	PhaseDownload   = "download"
	PhaseVerify     = "verify"
	PhaseBackup     = "backup"
	PhaseInstall    = "install"
	PhaseRestart    = "restart"
	PhaseHealth     = "health"
	PhaseDone       = "done"
	PhaseRolledBack = "rolled_back"
	PhaseFailed     = "failed"
)

// Request is written by the panel and read by the updater.
type Request struct {
	Tag         string    `json:"tag"`
	From        string    `json:"from"`
	HealthURL   string    `json:"health_url"`
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
}

// Status is written by the updater and read by the panel (also after the
// restart, so the UI can show the outcome).
type Status struct {
	Tag        string     `json:"tag"`
	From       string     `json:"from"`
	Phase      string     `json:"phase"`
	Running    bool       `json:"running"`
	Message    string     `json:"message,omitempty"`
	Error      string     `json:"error,omitempty"`
	BackupDir  string     `json:"backup_dir,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Dir is where the request and status files live (inside the data dir, so
// the unprivileged panel can write the request).
func Dir(dataDir string) string { return filepath.Join(dataDir, "update") }

// RequestPath and StatusPath are the two files of an update.
func RequestPath(dataDir string) string { return filepath.Join(Dir(dataDir), "request.json") }
func StatusPath(dataDir string) string  { return filepath.Join(Dir(dataDir), "status.json") }

func writeJSONAtomic(path string, v any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadStatus returns the last update status (nil when none ran).
func ReadStatus(dataDir string) *Status {
	b, err := os.ReadFile(StatusPath(dataDir))
	if err != nil {
		return nil
	}
	var s Status
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// ErrBusy means an update is already running.
var ErrBusy = errors.New("an update is already running")

// staleAfter: a "running" status older than this is a crashed updater.
const staleAfter = 15 * time.Minute

// Active reports whether st is an update still in progress.
func Active(st *Status, now time.Time) bool {
	return st != nil && st.Running && now.Sub(st.UpdatedAt) < staleAfter
}
