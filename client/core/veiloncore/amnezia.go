package veiloncore

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Amnezia client export ("vpn://…", also saved as .vpn files):
//
//	"vpn://" + base64url( qCompress(JSON) )
//
// qCompress = 4-byte big-endian uncompressed length + zlib stream (older
// exports: plain JSON or zlib without the length). The JSON is
//
//	{"containers":[{"container":"amnezia-awg","awg":{"last_config":"<json>",
//	   "port":"…","Jc":"…",…}}], "defaultContainer":"amnezia-awg",
//	 "description":"…","hostName":"1.2.3.4","dns1":"…","dns2":"…"}
//
// last_config is itself JSON with "config" (a wg-quick text, sometimes with
// $PRIMARY_DNS / $SECONDARY_DNS placeholders) and/or the raw fields
// (client_priv_key, server_pub_key, psk_key, client_ip, hostName, port, mtu,
// Jc…H4, I1…I5). Xray containers carry an Xray JSON config in last_config.

var errAmneziaNoConfig = errors.New("vpn://: no supported container (AmneziaWG / WireGuard / Xray)")

func decodeAmneziaPayload(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "vpn://"), "VPN://")
	raw, err := decodeB64(s)
	if err != nil {
		return nil, fmt.Errorf("vpn://: base64: %w", err)
	}
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '{' {
		return t, nil
	}
	try := func(b []byte) ([]byte, error) {
		zr, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(io.LimitReader(zr, 16<<20))
	}
	if len(raw) > 4 {
		if out, err := try(raw[4:]); err == nil {
			return out, nil
		}
	}
	out, err := try(raw)
	if err != nil {
		return nil, fmt.Errorf("vpn://: decompress: %w", err)
	}
	return out, nil
}

// ParseAmneziaVPN parses an Amnezia "vpn://" export into profiles (the
// default container first).
func ParseAmneziaVPN(link string) ([]*Profile, error) {
	b, err := decodeAmneziaPayload(link)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("vpn://: json: %w", err)
	}
	host := jstr(root, "hostName")
	dns1, dns2 := jstr(root, "dns1"), jstr(root, "dns2")
	name := firstNonEmpty(jstr(root, "description"), jstr(root, "name"), host)
	def := jstr(root, "defaultContainer")
	conts, _ := root["containers"].([]any)
	// Default container first.
	for i, c := range conts {
		if m, ok := c.(map[string]any); ok && jstr(m, "container") == def && i > 0 {
			conts[0], conts[i] = conts[i], conts[0]
			break
		}
	}
	var out []*Profile
	var errs []string
	for _, c := range conts {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		cname := jstr(m, "container")
		for key, v := range m {
			proto, ok := v.(map[string]any)
			if !ok || key == "container" {
				continue
			}
			ps, err := amneziaProtocol(key, proto, host, dns1, dns2, name)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", cname, err))
				continue
			}
			out = append(out, ps...)
		}
	}
	if len(out) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("vpn://: %s", strings.Join(errs, "; "))
		}
		return nil, errAmneziaNoConfig
	}
	return out, nil
}

func amneziaProtocol(key string, proto map[string]any, host, dns1, dns2, name string) ([]*Profile, error) {
	lc := jstr(proto, "last_config")
	var last map[string]any
	if lc != "" {
		if err := json.Unmarshal([]byte(lc), &last); err != nil {
			return nil, fmt.Errorf("last_config: %w", err)
		}
	}
	if last == nil {
		last = map[string]any{}
	}
	switch {
	case key == "awg" || key == "wireguard" || strings.HasPrefix(key, "awg"):
		conf := jstr(last, "config")
		if !strings.Contains(conf, "[Interface]") {
			conf = amneziaBuildConf(last, proto, host)
		}
		r := strings.NewReplacer("$PRIMARY_DNS", firstNonEmpty(dns1, "1.1.1.1"), "$SECONDARY_DNS", firstNonEmpty(dns2, "1.0.0.1"))
		conf = r.Replace(conf)
		p, err := ParseWGConf(conf, name)
		if err != nil {
			return nil, err
		}
		if host != "" && p.Server == "" {
			p.Server = host
		}
		if m := jint(last, "mtu"); m > 0 && p.WG.MTU == 0 {
			p.WG.MTU = m
		}
		p.ID = ""
		p.normalize()
		return []*Profile{p}, nil
	case key == "xray" || key == "ssxray":
		if _, ok := last["outbounds"]; !ok {
			return nil, errors.New("xray: no outbounds")
		}
		ps, err := parseJSONConfig([]byte(lc))
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			if p.Name == "" || p.Name == p.Server {
				p.Name = name
			}
		}
		return ps, nil
	}
	return nil, fmt.Errorf("%s: unsupported", key)
}

// amneziaBuildConf renders a wg-quick text from last_config fields.
func amneziaBuildConf(last, proto map[string]any, host string) string {
	get := func(k string) string { return firstNonEmpty(jstr(last, k), jstr(proto, k)) }
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", get("client_priv_key"))
	if ip := get("client_ip"); ip != "" {
		fmt.Fprintf(&b, "Address = %s\n", ip)
	}
	if m := get("mtu"); m != "" {
		fmt.Fprintf(&b, "MTU = %s\n", m)
	}
	b.WriteString("DNS = $PRIMARY_DNS, $SECONDARY_DNS\n")
	for _, k := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4", "I1", "I2", "I3", "I4", "I5",
		"HeaderProtectionKey", "ContentPaddingAddition", "RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts", "RandomTrailers", "DisableCookies"} {
		if v := get(k); v != "" {
			fmt.Fprintf(&b, "%s = %s\n", k, v)
		}
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", get("server_pub_key"))
	if psk := get("psk_key"); psk != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", psk)
	}
	h := firstNonEmpty(jstr(last, "hostName"), host)
	if port := get("port"); h != "" && port != "" {
		fmt.Fprintf(&b, "Endpoint = %s:%s\n", h, port)
	}
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n")
	return b.String()
}

func jstr(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return ""
}

func jint(m map[string]any, k string) int {
	n, _ := strconv.Atoi(jstr(m, k))
	return n
}
