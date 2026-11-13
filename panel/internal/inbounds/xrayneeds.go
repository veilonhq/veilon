package inbounds

import (
	"fmt"
	"sort"
	"strings"
)

// XrayNeed is a feature of an inbound that needs a minimum Xray-core
// version on the server that runs it.
type XrayNeed struct {
	Feature string `json:"feature"`
	Min     string `json:"min"`
}

// Minimum Xray-core versions (checked against the Xray-core release notes
// and tags; see docs/en/xray-advanced.md). Settings Xray had long before
// (fallbacks, TLS versions and ciphers, sockopt, sniffing, gRPC, RAW
// header, xmux and download split, which are client-only) need nothing.
const (
	MinXrayStreamUpSecs   = "25.1.30"  // XHTTP scStreamUpServerSecs
	MinXrayRealityLimit   = "25.6.8"   // REALITY limitFallbackUpload/Download
	MinXrayMLDSA65        = "25.7.26"  // REALITY mldsa65Seed
	MinXrayECH            = "25.7.26"  // TLS echServerKeys
	MinXrayReverse        = "25.9.11"  // VLESS reverse proxy (users[].reverse)
	MinXrayHysteria       = "26.3.27"  // Hysteria 2 inbound, finalmask quicParams
	MinXrayMasks          = "26.3.27"  // finalmask fragment (TCP) / noise (UDP)
	MinXrayKCP            = "26.6.1"   // mKCP as written here (finalmask mkcp-legacy)
	MinXrayWSHeartbeat    = "24.11.30" // WebSocket heartbeatPeriod
	MinXrayVLESSEncNeeded = MinVLESSEncXray
)

// XrayNeeds lists what an inbound needs from the Xray-core that runs it,
// highest version first ("" protocol engines other than Xray need nothing).
func XrayNeeds(v *View) []XrayNeed {
	if v == nil || Engine(v.Protocol) != EngineXray {
		return nil
	}
	var out []XrayNeed
	add := func(f, min string) { out = append(out, XrayNeed{Feature: f, Min: min}) }
	if v.Protocol == "hysteria" {
		add("Hysteria 2", MinXrayHysteria)
	}
	if v.Network == "kcp" && v.Protocol != "hysteria" {
		add("mKCP", MinXrayKCP)
	}
	if v.Protocol == "vless" && v.VLESSEnc != nil && v.VLESSEnc.Enabled {
		add("VLESS Encryption", MinXrayVLESSEncNeeded)
	}
	if a := v.Advanced; a != nil {
		if a.Reality != nil && v.Security == "reality" {
			if a.Reality.MLDSA65Seed != "" {
				add("Reality ML-DSA-65", MinXrayMLDSA65)
			}
			if limitOn(a.Reality.LimitUpload) || limitOn(a.Reality.LimitDownload) {
				add("Reality fallback rate limit", MinXrayRealityLimit)
			}
		}
		if a.TLS != nil && v.Security == "tls" && a.TLS.ECHServerKeys != "" {
			add("TLS ECH", MinXrayECH)
		}
		if a.Masks != nil && (a.Masks.Fragment != nil || a.Masks.Noise != nil) {
			add("fragment / noise masks", MinXrayMasks)
		}
		if a.Reverse != nil && a.Reverse.Enabled && v.Protocol == "vless" {
			add("VLESS reverse proxy", MinXrayReverse)
		}
		if a.XHTTP != nil && v.Network == "xhttp" && a.XHTTP.ScStreamUpServerSecs != "" {
			add("XHTTP stream-up seconds", MinXrayStreamUpSecs)
		}
		if a.WS != nil && v.Network == "ws" && a.WS.HeartbeatPeriod > 0 {
			add("WebSocket heartbeat", MinXrayWSHeartbeat)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Min != out[j].Min && VersionAtLeast(out[i].Min, out[j].Min) })
	return out
}

func limitOn(l *LimitFallback) bool {
	return l != nil && (l.AfterBytes > 0 || l.BytesPerSec > 0 || l.BurstBytesPerSec > 0)
}

// XrayMissing returns what an inbound needs that Xray-core version ver
// lacks (nil when ver is unknown or new enough).
func XrayMissing(v *View, ver string) []XrayNeed {
	var miss []XrayNeed
	for _, n := range XrayNeeds(v) {
		if !VersionAtLeast(ver, n.Min) {
			miss = append(miss, n)
		}
	}
	return miss
}

// XrayMissingText is a short reason: "Reality ML-DSA-65 (25.7.26+), TLS ECH (25.7.26+)".
func XrayMissingText(miss []XrayNeed) string {
	parts := make([]string, 0, len(miss))
	for _, n := range miss {
		parts = append(parts, fmt.Sprintf("%s (%s+)", n.Feature, n.Min))
	}
	return strings.Join(parts, ", ")
}
