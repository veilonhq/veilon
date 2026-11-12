package veiloncore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/xtls/xray-core/common/geodata"
	xcore "github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
)

func xrayTLS(p *Profile, alpn []string) obj {
	t := obj{}
	sni := p.SNI
	if sni == "" && p.Host != "" {
		sni = p.Host
	}
	if sni == "" && net.ParseIP(p.Server) == nil {
		sni = p.Server
	}
	if sni != "" {
		t["serverName"] = sni
	}
	if len(p.ALPN) > 0 {
		t["alpn"] = p.ALPN
	} else if len(alpn) > 0 {
		t["alpn"] = alpn
	}
	if p.Fingerprint != "" && p.Fingerprint != "none" {
		t["fingerprint"] = p.Fingerprint
	}
	if p.ECH != "" {
		t["echConfigList"] = p.ECH
	}
	if p.VerifyName != "" {
		t["verifyPeerCertByName"] = p.VerifyName
	}
	if p.CipherSuites != "" {
		t["cipherSuites"] = p.CipherSuites
	}
	if p.Insecure && p.PinSHA256 != "" {
		// Xray removed allowInsecure: pin the leaf certificate instead.
		t["pinnedPeerCertSha256"] = p.PinSHA256
	}
	return t
}

// xrayOutbound renders the Xray outbound for p (tag "proxy").
func xrayOutbound(p *Profile) (obj, error) {
	o := obj{"tag": "proxy"}
	switch p.Type {
	case TypeVLESS:
		enc := p.Encryption
		if enc == "" {
			enc = "none"
		}
		u := obj{"id": p.UUID, "encryption": enc}
		if p.Flow != "" {
			u["flow"] = p.Flow
		}
		o["protocol"] = "vless"
		o["settings"] = obj{"vnext": []any{obj{"address": p.Server, "port": p.Port, "users": []any{u}}}}
	case TypeVMess:
		o["protocol"] = "vmess"
		o["settings"] = obj{"vnext": []any{obj{"address": p.Server, "port": p.Port,
			"users": []any{obj{"id": p.UUID, "security": firstNonEmpty(p.VMessSecurity, "auto")}}}}}
	case TypeTrojan:
		o["protocol"] = "trojan"
		o["settings"] = obj{"servers": []any{obj{"address": p.Server, "port": p.Port, "password": p.Password}}}
	case TypeShadowsocks:
		o["protocol"] = "shadowsocks"
		o["settings"] = obj{"servers": []any{obj{"address": p.Server, "port": p.Port, "method": p.Method, "password": p.Password}}}
	case TypeSOCKS:
		o["protocol"] = "socks"
		s := obj{"address": p.Server, "port": p.Port}
		if p.Username != "" || p.Password != "" {
			s["user"], s["pass"] = p.Username, p.Password
		}
		o["settings"] = s
	case TypeMasque:
		o["protocol"] = "masque"
		o["settings"] = obj{"address": p.Server, "port": p.Port}
		alpn := p.ALPN
		if len(alpn) == 0 {
			alpn = []string{"h3"}
		}
		o["streamSettings"] = obj{"network": "masque",
			"masqueSettings": obj{"path": p.MasquePath, "user": p.Username, "pass": p.Password},
			"security":       "tls", "tlsSettings": xrayTLS(p, alpn)}
		return o, nil
	default:
		return nil, fmt.Errorf("xray: unsupported type %q", p.Type)
	}
	network := p.Network
	if network == "" {
		network = "tcp"
	}
	if network == "splithttp" {
		network = "xhttp"
	}
	ss := obj{"network": network}
	var udpMasks []any // finalmask UDP masks generated from legacy fields
	switch network {
	case "tcp", "raw":
		ss["network"] = "tcp"
		if p.HeaderType == "http" {
			paths := splitList(p.Path)
			if len(paths) == 0 {
				paths = []string{"/"}
			}
			req := obj{"path": paths}
			if p.Host != "" {
				req["headers"] = obj{"Host": splitList(p.Host)}
			}
			ss["tcpSettings"] = obj{"header": obj{"type": "http", "request": req}}
		}
	case "ws":
		ss["wsSettings"] = obj{"path": firstNonEmpty(p.Path, "/"), "host": p.Host}
	case "httpupgrade":
		ss["httpupgradeSettings"] = obj{"path": firstNonEmpty(p.Path, "/"), "host": p.Host}
	case "xhttp":
		x := obj{"path": firstNonEmpty(p.Path, "/"), "host": p.Host, "mode": firstNonEmpty(p.Mode, "auto")}
		if p.XHTTPExtra != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(p.XHTTPExtra), &extra); err != nil {
				return nil, fmt.Errorf("xhttp extra: %w", err)
			}
			// "mode"/"host"/"path" belong to xhttpSettings itself.
			for _, k := range []string{"mode", "host", "path"} {
				if v, ok := extra[k].(string); ok && v != "" && (k != "mode" || p.Mode == "") {
					x[k] = v
				}
				delete(extra, k)
			}
			if len(extra) > 0 {
				x["extra"] = extra
			}
		}
		if p.XPaddingBytes != "" {
			x["xPaddingBytes"] = p.XPaddingBytes
		}
		ss["xhttpSettings"] = x
	case "grpc":
		g := obj{"serviceName": p.ServiceName, "multiMode": p.Mode == "multi"}
		if p.Authority != "" {
			g["authority"] = p.Authority
		}
		ss["grpcSettings"] = g
	case "kcp", "mkcp":
		ss["network"] = "kcp"
		k := obj{}
		if c := p.KCP; c != nil {
			for key, v := range map[string]int{"mtu": c.MTU, "tti": c.TTI, "uplinkCapacity": c.UplinkCapacity,
				"downlinkCapacity": c.DownlinkCapacity, "cwndMultiplier": c.CwndMultiplier, "maxSendingWindow": c.MaxSendingWindow} {
				if v > 0 {
					k[key] = v
				}
			}
		}
		ss["kcpSettings"] = k
		// Xray dropped kcpSettings.header/seed: the same obfuscation is
		// the "mkcp-legacy" UDP mask (header = type, value = dns domain,
		// or the seed for the aes-128-gcm form without header).
		ht := strings.ToLower(p.HeaderType)
		if ht == "none" {
			ht = ""
		}
		if ht == "wechat-video" {
			ht = "wechat"
		}
		if ht != "" || p.Seed != "" {
			ms := obj{}
			if ht != "" {
				ms["header"] = ht
				if ht == "dns" && p.Host != "" {
					ms["value"] = p.Host
				}
			} else {
				ms["value"] = p.Seed
			}
			udpMasks = append(udpMasks, obj{"type": "mkcp-legacy", "settings": ms})
		}
	case "xdrive":
		if p.Type != TypeVLESS {
			return nil, errors.New("xdrive: only VLESS (with encryption) is supported")
		}
		var x map[string]any
		if p.XDrive == "" {
			return nil, errors.New("xdrive: settings missing (import the JSON subscription)")
		}
		if err := json.Unmarshal([]byte(p.XDrive), &x); err != nil {
			return nil, fmt.Errorf("xdrive settings: %w", err)
		}
		ss["xdriveSettings"] = x
	default:
		return nil, fmt.Errorf("xray: unsupported transport %q", network)
	}
	if p.FinalMask != "" || len(udpMasks) > 0 {
		fm := map[string]any{}
		if p.FinalMask != "" {
			if err := json.Unmarshal([]byte(p.FinalMask), &fm); err != nil {
				return nil, fmt.Errorf("finalmask: %w", err)
			}
		}
		if len(udpMasks) > 0 {
			hasLegacy := false
			if arr, ok := fm["udp"].([]any); ok {
				for _, m := range arr {
					if mm, ok := m.(map[string]any); ok && mm["type"] == "mkcp-legacy" {
						hasLegacy = true
					}
				}
				if !hasLegacy {
					fm["udp"] = append(arr, udpMasks...)
				}
			} else {
				fm["udp"] = udpMasks
			}
		}
		ss["finalmask"] = fm
	}
	if p.Sockopt != "" {
		var so map[string]any
		if err := json.Unmarshal([]byte(p.Sockopt), &so); err != nil {
			return nil, fmt.Errorf("sockopt: %w", err)
		}
		ss["sockopt"] = so
	}
	if network == "xdrive" {
		mux := obj{"enabled": true, "concurrency": 8}
		if p.Mux != "" {
			var m map[string]any
			if json.Unmarshal([]byte(p.Mux), &m) == nil {
				for k, v := range m {
					mux[k] = v
				}
				mux["enabled"] = true
			}
		}
		o["mux"] = mux
	} else if p.Mux != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(p.Mux), &m); err != nil {
			return nil, fmt.Errorf("mux: %w", err)
		}
		o["mux"] = m
	}
	switch p.Security {
	case "tls":
		ss["security"] = "tls"
		ss["tlsSettings"] = xrayTLS(p, nil)
	case "reality":
		fp := p.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		ss["security"] = "reality"
		r := obj{"serverName": p.SNI, "fingerprint": fp, "publicKey": p.PublicKey, "shortId": p.ShortID, "spiderX": p.SpiderX}
		if p.PQV != "" {
			r["mldsa65Verify"] = p.PQV
		}
		ss["realitySettings"] = r
	default:
		ss["security"] = "none"
	}
	o["streamSettings"] = ss
	return o, nil
}

// xrayConfig renders a full Xray config exposing a local SOCKS inbound.
func xrayConfig(p *Profile, port int, user, pass string, st Settings) (obj, error) {
	out, err := xrayOutbound(p)
	if err != nil {
		return nil, err
	}
	level := "warning"
	switch st.LogLevel {
	case "debug", "trace":
		level = "debug"
	case "info":
		level = "info"
	case "error":
		level = "error"
	}
	dnsSrv := st.DirectDNS
	if rest, ok := strings.CutPrefix(dnsSrv, "https://"); ok {
		// 1.0.8: DoH direct resolver by IP (the default) → Xray's local
		// (not proxied) DoH client.
		host := rest
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			host = rest[:j]
		}
		if h, _ := splitServerPort(host); net.ParseIP(h) != nil {
			dnsSrv = "https+local://" + rest
		} else {
			dnsSrv = "8.8.8.8"
		}
	} else if h, _ := splitServerPort(dnsSrv); net.ParseIP(h) == nil {
		dnsSrv = "8.8.8.8"
	}
	return obj{
		"log": obj{"loglevel": level},
		"dns": obj{"servers": []any{dnsSrv}},
		"inbounds": []any{obj{
			"tag": "socks-in", "listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": obj{"auth": "password", "accounts": []any{obj{"user": user, "pass": pass}}, "udp": true, "ip": "127.0.0.1"},
		}},
		"outbounds": []any{out, obj{"tag": "direct", "protocol": "freedom"}},
	}, nil
}

// xrayInstance is a running Xray core.
type xrayInstance struct{ inst *xcore.Instance }

func startXray(cfg obj) (x *xrayInstance, err error) {
	defer registerXrayLog()
	// Xray panics on some malformed values (e.g. a truncated VLESS
	// "encryption" string); never let that take the app down.
	defer func() {
		if r := recover(); r != nil {
			x, err = nil, fmt.Errorf("xray config: invalid settings (%v)", r)
		}
	}()
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	c, err := serial.LoadJSONConfig(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("xray config: %w", err)
	}
	inst, err := xcore.New(c)
	if err != nil {
		return nil, fmt.Errorf("xray: %w", err)
	}
	if err := inst.Start(); err != nil {
		inst.Close()
		return nil, fmt.Errorf("xray start: %w", err)
	}
	return &xrayInstance{inst: inst}, nil
}

func (x *xrayInstance) Close() error {
	if x == nil || x.inst == nil {
		return nil
	}
	err := x.inst.Close()
	registerXrayLog()
	return err
}

// checkXrayConfig validates an Xray config without starting it.
func checkXrayConfig(cfg obj) (err error) {
	defer registerXrayLog()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("xray config: invalid settings (%v)", r)
		}
	}()
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	c, err := serial.LoadJSONConfig(bytes.NewReader(b))
	if err != nil {
		return err
	}
	inst, err := xcore.New(c)
	if err != nil {
		return err
	}
	return inst.Close()
}

// ErrXrayCleartextPublicIP is returned (as an app string key, see API.md) for a
// VLESS/Trojan profile without TLS/REALITY (and, for VLESS, without VLESS
// encryption) to a public server address when it needs an Xray-only feature:
// Xray refuses such configs and sing-box cannot carry the feature.
var ErrXrayCleartextPublicIP = errors.New("err.xrayCleartextPublicIP")

// xrayRefusesCleartext mirrors Xray's validateOutboundTransportSecurity
// (infra/conf/xray.go): VLESS (encryption none) and Trojan without transport
// security are only built for a private/loopback/link-local IP or a private
// domain (lan, local, test, example, internal, localhost, dotless names…).
// Public IPs and public domain names are refused.
func xrayRefusesCleartext(p *Profile) bool {
	if p.Type != TypeVLESS && p.Type != TypeTrojan {
		return false
	}
	if p.Security != "" && p.Security != "none" {
		return false
	}
	if p.Type == TypeVLESS && p.Encryption != "" && p.Encryption != "none" {
		return false
	}
	host := strings.Trim(strings.TrimSpace(p.Server), "[]")
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
	}
	d := strings.TrimSuffix(strings.ToLower(host), ".")
	return !geodata.GetPrivateDomainMatcher().MatchAny(d)
}
