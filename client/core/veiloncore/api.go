// Package veiloncore is the Veilon Android client core, bound into the app
// with gomobile. All structured values cross the boundary as JSON strings.
package veiloncore

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common/control"
	xcore "github.com/xtls/xray-core/core"
)

// Version of the core library.
const Version = "1.0.8"

func decodeProfile(s string) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, errors.New("invalid profile: " + err.Error())
	}
	p.normalize()
	if removedTypes[p.Type] {
		return nil, ErrTypeRemoved
	}
	return &p, nil
}

// CoreVersions returns versions of the bundled cores as JSON.
func CoreVersions() (out string) {
	defer apiGuard("CoreVersions", func(m string) { out = panicJSON("CoreVersions", m) })
	return encodeJSON(map[string]string{"veilon": Version, "singbox": C.Version, "xray": xcore.Version()})
}

// ParseText parses pasted text, a QR payload or a subscription body.
// Returns ParseResult JSON.
func ParseText(text string) (out string) {
	defer apiGuard("ParseText", func(m string) { out = panicJSON("ParseText", m) })
	return encodeJSON(ParseContent(text))
}

// ParseConf parses a WireGuard / AmneziaWG .conf file. Returns a profile JSON
// or {"error": "..."}.
func ParseConf(text, name string) (out string) {
	defer apiGuard("ParseConf", func(m string) { out = panicJSON("ParseConf", m) })
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".conf"), ".vpn")
	var p *Profile
	var err error
	if t := strings.TrimSpace(text); strings.HasPrefix(strings.ToLower(t), "vpn://") {
		p, err = ParseLink(t) // Amnezia .vpn file
	} else {
		p, err = ParseWGConf(text, name)
	}
	if err != nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	return encodeJSON(p)
}

// FetchSubscription downloads and parses a subscription. deviceJSON is a
// Device; proxyAddr ("127.0.0.1:port" or "") routes the request through the
// running tunnel. Returns SubResult JSON.
func FetchSubscription(url, deviceJSON, proxyAddr string, timeoutMs int32) (out string) {
	defer apiGuard("FetchSubscription", func(m string) { out = panicJSON("FetchSubscription", m) })
	var d Device
	_ = json.Unmarshal([]byte(deviceJSON), &d)
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	return encodeJSON(fetchSubscription(strings.TrimSpace(url), d, proxyAddr, time.Duration(timeoutMs)*time.Millisecond))
}

// SetSubPins sets the subscription server certificates the user approved
// (1.0.8): a JSON object {"ip:port": "<hex sha256 of the leaf cert>"}. Only
// literal-IP https subscription hosts are ever pinned (see subtls.go).
func SetSubPins(pinsJSON string) (err error) {
	defer apiGuard("SetSubPins", func(m string) { err = panicErr("SetSubPins", m) })
	return setSubPins(pinsJSON)
}

// ParseUserInfoHeader parses a subscription-userinfo header (JSON UserInfo).
func ParseUserInfoHeader(h string) (out string) {
	defer apiGuard("ParseUserInfoHeader", func(m string) { out = "{}" })
	return encodeJSON(ParseUserInfo(h))
}

// ParseProfileLink parses one share link into a profile JSON (the same
// shape ParseText returns per profile), or {"error": "..."}.
func ParseProfileLink(link string) (out string) {
	defer apiGuard("ParseProfileLink", func(m string) { out = panicJSON("ParseProfileLink", m) })
	p, err := ParseLink(link)
	if err != nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	return encodeJSON(p)
}

// NormalizeProfile is the edit round-trip: it decodes an (edited) profile
// JSON, normalizes it (type case, defaults, missing name / id) and returns
// the canonical profile JSON, or {"error": "..."}. Unknown keys are
// dropped. The id is kept when present; pass "id":"" to have it recomputed
// from the connection fields. The result is accepted by Start, URLTest,
// CheckProfile, BuildConfig and ProfileToLink.
func NormalizeProfile(profileJSON string) (out string) {
	defer apiGuard("NormalizeProfile", func(m string) { out = panicJSON("NormalizeProfile", m) })
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	return encodeJSON(p)
}

// ProfileToLink re-exports an (edited) profile as a share link. Returns
// {"link": "..."} or {"error": "..."} (e.g. MASQUE has no link format).
// AddressOverride is a local setting and are not included.
func ProfileToLink(profileJSON string) (out string) {
	defer apiGuard("ProfileToLink", func(m string) { out = panicJSON("ProfileToLink", m) })
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	l, err := ToLink(p)
	if err != nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	return encodeJSON(map[string]string{"link": l})
}

// CoreFor returns the engine name that will carry the profile.
func CoreFor(profileJSON string) (out string) {
	defer apiGuard("CoreFor", func(m string) { out = "" })
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return ""
	}
	return string(CoreOf(p))
}

// ResolveCoreFor returns {"core": "<engine>"} or {"core": "...", "error":
// "<key or text>"} for a profile under the given settings (Settings.Core and
// the profile's own "core" choice). Same rules as Start / URLTest.
func ResolveCoreFor(profileJSON, settingsJSON string) (out string) {
	defer apiGuard("ResolveCoreFor", func(m string) {
		out = encodeJSON(map[string]string{"core": "", "error": panicErr("ResolveCoreFor", m).Error()})
	})
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return encodeJSON(map[string]string{"core": "", "error": err.Error()})
	}
	st, _ := parseSettings(settingsJSON)
	c, err := ResolveCore(p.withAddressOverride(), st)
	if err != nil {
		return encodeJSON(map[string]string{"core": string(c), "error": err.Error()})
	}
	return encodeJSON(map[string]string{"core": string(c)})
}

// CheckProfile validates the configuration generated for a profile.
// Returns "" when valid, otherwise the error.
func CheckProfile(profileJSON, settingsJSON string) (out string) {
	defer apiGuard("CheckProfile", func(m string) { out = panicErr("CheckProfile", m).Error() })
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return err.Error()
	}
	st, err := parseSettings(settingsJSON)
	if err != nil {
		return err.Error()
	}
	if _, err := checkProfile(p, st, true); err != nil {
		return err.Error()
	}
	return ""
}

// BuildConfig returns the sing-box configuration for a profile (debug /
// export). Sidecar ports are placeholders.
func BuildConfig(profileJSON, settingsJSON string) (out string) {
	defer apiGuard("BuildConfig", func(m string) { out = panicJSON("BuildConfig", m) })
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	st, _ := parseSettings(settingsJSON)
	cfg, err := checkProfile(p, st, true)
	if err != nil && cfg == nil {
		return encodeJSON(map[string]string{"error": err.Error()})
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

// Start connects using a profile. The previous session (if any) is closed.
func Start(profileJSON, settingsJSON string, platform Platform) (err error) {
	defer apiGuard("Start", func(m string) { err = panicErr("Start", m) })
	if platform == nil {
		return errors.New("platform is required")
	}
	p, perr := decodeProfile(profileJSON)
	if perr != nil {
		return perr
	}
	st, serr := parseSettings(settingsJSON)
	if serr != nil {
		return serr
	}
	return startGlobal(p, st, platform)
}

// Stop disconnects. Idempotent: safe with no session, twice in a row, or
// concurrently with Start (both take the session lock).
func Stop() (err error) {
	defer apiGuard("Stop", func(m string) { err = panicErr("Stop", m) })
	return stopGlobal()
}

// IsRunning reports whether a session is active.
func IsRunning() (ok bool) {
	defer apiGuard("IsRunning", func(string) { ok = false })
	runMu.Lock()
	defer runMu.Unlock()
	return running != nil
}

// NetworkChanged resets connections after the device switched networks.
// SetDefaultInterface reports Android's default (underlying) network
// interface (LinkProperties.interfaceName and its index; index 0 if
// unknown). Empty name clears it.
func SetDefaultInterface(name string, index int32) {
	defer apiGuard("SetDefaultInterface", nil)
	if name == "" {
		defaultIface.Store(nil)
		return
	}
	defaultIface.Store(&control.Interface{Index: int(index), Name: name})
}

func NetworkChanged() {
	defer apiGuard("NetworkChanged", nil)
	runMu.Lock()
	s := running
	runMu.Unlock()
	if s != nil && s.platform != nil {
		s.platform.monitor.notifyNetworkChanged()
	}
}

// URLTest measures the real delay of a profile in milliseconds (-1 on
// failure). It runs an isolated instance; the active session is untouched.
func URLTest(profileJSON, settingsJSON, url string, timeoutMs int32) (out int32) {
	defer apiGuard("URLTest", func(string) { out = -1 })
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return -1
	}
	st, _ := parseSettings(settingsJSON)
	if url == "" {
		url = "https://www.gstatic.com/generate_204"
	}
	if timeoutMs <= 0 {
		timeoutMs = 8000
	}
	d, err := urlTest(p, st, url, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		pushLogLevel("debug", "ping "+p.Name+": "+err.Error())
		return -1
	}
	return int32(d)
}

// URLTestError is URLTest that also returns the failure reason as JSON
// {"delay": n, "error": "..."}.
func URLTestError(profileJSON, settingsJSON, url string, timeoutMs int32) (out string) {
	defer apiGuard("URLTestError", func(m string) {
		out = encodeJSON(map[string]any{"delay": -1, "error": panicErr("URLTestError", m).Error()})
	})
	p, err := decodeProfile(profileJSON)
	if err != nil {
		return encodeJSON(map[string]any{"delay": -1, "error": err.Error()})
	}
	st, _ := parseSettings(settingsJSON)
	if url == "" {
		url = "https://www.gstatic.com/generate_204"
	}
	if timeoutMs <= 0 {
		timeoutMs = 8000
	}
	d, err := urlTest(p, st, url, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		return encodeJSON(map[string]any{"delay": -1, "error": err.Error()})
	}
	return encodeJSON(map[string]any{"delay": d})
}

// GetLogs returns log lines with seq > after (JSON array of LogLine).
func GetLogs(after int64) (out string) {
	defer apiGuard("GetLogs", func(string) { out = "[]" })
	return logsAfter(after)
}

// ClearLogs empties the log buffer.
func ClearLogs() {
	defer apiGuard("ClearLogs", nil)
	clearLogs()
}

// WriteLog adds an app-side line to the log buffer.
func WriteLog(level, msg string) {
	defer func() { _ = recover() }() // never log from a failing logger
	pushLogLevel(level, msg)
}
