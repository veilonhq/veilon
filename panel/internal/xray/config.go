// Package xray generates Xray-core configuration, supervises the Xray
// process and collects per-user traffic via its gRPC StatsService.
package xray

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/certs"
	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/inbounds"
	"github.com/veilonhq/veilon/panel/internal/userkey"
	"github.com/veilonhq/veilon/panel/internal/users"
)

type obj = map[string]any

// Email returns the identifier used for a user inside Xray (and its stats).
func Email(u *db.User) string { return fmt.Sprintf("%d.%s", u.ID, u.Username) }

// ParseEmail extracts the user id from an Xray email ("<id>.<username>")
// or from a custom proxy username (see package userkey).
func ParseEmail(e string) (int64, bool) { return userkey.ID(e) }

// Input is everything needed to render a config.
type Input struct {
	Inbounds []inbounds.View
	// Users per inbound id (only users that must be served).
	Users map[int64][]db.User
	// Peers holds WireGuard/AmneziaWG peers per inbound id and user id.
	Peers    map[int64]map[int64]db.WGPeer
	APIPort  int
	DataDir  string
	AssetDir string
	// BridgePort is the loopback port where sing-box hands authenticated
	// TUIC traffic to Xray, so Xray routes and counts it per user (0 = off).
	BridgePort int
	// TunOK reports whether this process may create TUN interfaces.
	TunOK bool
	// Skipped collects inbounds left out of the config, with the reason.
	Skipped []string
	// XrayVersion is the Xray-core version that runs the config ("" =
	// unknown); inbounds whose settings need a newer one are left out.
	XrayVersion string
	// Policy holds the per-user routing policy (blocked IPs, torrent and
	// ad blocking).
	Policy Policy
	// Template is the admin's outbounds / routing / DNS template (nil =
	// default).
	Template *Template
	// Usernames maps every username to its Xray email, so template rules
	// can name users by username.
	Usernames map[string]string
	// ProxyAliases maps the Xray email of every user with a custom proxy
	// username to that name: on HTTP, Mixed and SOCKS inbounds Xray knows
	// the user by it, so routing rules that name the user list both.
	ProxyAliases map[string]string
}

// AdsGeosite is the geosite category blocked by ad blocking.
const AdsGeosite = "geosite:category-ads-all"

// Policy is the routing policy derived from users, groups and settings.
type Policy struct {
	// BlockedIPs maps a user email to client IPs refused for that user.
	BlockedIPs map[string][]string
	// TorrentAll blocks BitTorrent for everyone; otherwise TorrentUsers.
	TorrentAll   bool
	TorrentUsers []string
	// AdBlockAll blocks ads for everyone; otherwise AdBlockUsers (groups).
	AdBlockAll   bool
	AdBlockUsers []string
}

// policyRules renders the policy as routing rules (before the defaults).
func policyRules(p Policy, geosite bool, aliases map[string]string) ([]any, []string) {
	var rules []any
	users := func(emails ...string) []string { return withAliases(emails, aliases) }
	var warn []string
	emails := make([]string, 0, len(p.BlockedIPs))
	for e := range p.BlockedIPs {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	for _, e := range emails {
		rules = append(rules, obj{"type": "field", "user": users(e), "source": p.BlockedIPs[e], "outboundTag": "blocked"})
	}
	if p.TorrentAll {
		rules = append(rules, obj{"type": "field", "protocol": []string{"bittorrent"}, "outboundTag": "blocked"})
	} else if len(p.TorrentUsers) > 0 {
		rules = append(rules, obj{"type": "field", "user": users(p.TorrentUsers...), "protocol": []string{"bittorrent"}, "outboundTag": "blocked"})
	}
	if p.AdBlockAll || len(p.AdBlockUsers) > 0 {
		if !geosite {
			warn = append(warn, "ad blocking: geosite.dat is missing (run: veilon geo update)")
		} else if p.AdBlockAll {
			rules = append(rules, obj{"type": "field", "domain": []string{AdsGeosite}, "outboundTag": "blocked"})
		} else {
			rules = append(rules, obj{"type": "field", "user": users(p.AdBlockUsers...), "domain": []string{AdsGeosite}, "outboundTag": "blocked"})
		}
	}
	return rules, warn
}

// withAliases adds the custom proxy username of every listed email that
// has one, right after it.
func withAliases(emails []string, aliases map[string]string) []string {
	out := make([]string, 0, len(emails))
	for _, e := range emails {
		out = append(out, e)
		if a, ok := aliases[e]; ok {
			out = append(out, a)
		}
	}
	return out
}

// TunAvailable reports whether TUN interfaces can be created (CAP_NET_ADMIN
// and an openable /dev/net/tun).
func TunAvailable() bool {
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	if os.Geteuid() == 0 {
		return true
	}
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "CapEff:"); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			return err == nil && n&(1<<12) != 0 // CAP_NET_ADMIN
		}
	}
	return false
}

// LoadInput reads enabled inbounds and served users from the database.
func LoadInput(gdb *gorm.DB, dataDir string, apiPort int) (*Input, error) {
	var rows []db.Inbound
	if err := gdb.Where("enabled = ?", true).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	in := &Input{Users: map[int64][]db.User{}, APIPort: apiPort, DataDir: dataDir, AssetDir: filepath.Join(dataDir, "xray"), TunOK: TunAvailable()}
	for i := range rows {
		in.Inbounds = append(in.Inbounds, inbounds.FromModel(&rows[i]))
	}
	var us []db.User
	if err := gdb.Where("status IN ?", []string{db.StatusActive, db.StatusOnHold}).Order("id").Find(&us).Error; err != nil {
		return nil, err
	}
	byID := map[int64]db.User{}
	for _, u := range us {
		if users.Served(&u) {
			byID[u.ID] = u
		}
	}
	in.Policy = loadPolicy(gdb, byID)
	in.Usernames = map[string]string{}
	in.ProxyAliases = map[string]string{}
	var names []db.User
	gdb.Select("id", "username", "proxy_username").Find(&names)
	keys := map[string]int64{}
	for i := range names {
		in.Usernames[names[i].Username] = Email(&names[i])
		if pu := names[i].ProxyUsername; pu != "" {
			in.ProxyAliases[Email(&names[i])] = pu
			keys[pu] = names[i].ID
		}
	}
	// Statistics, the online list and the access log name these users by
	// their custom proxy username.
	userkey.Set(keys)
	if raw := db.GetSetting(gdb, KeyTemplate); raw != "" {
		t, err := ParseTemplate([]byte(raw))
		if err != nil {
			in.Skipped = append(in.Skipped, "xray template ignored: "+err.Error())
		} else {
			in.Template = t
		}
	}
	var links []db.UserInbound
	if err := gdb.Order("user_id").Find(&links).Error; err != nil {
		return nil, err
	}
	for _, l := range links {
		if u, ok := byID[l.UserID]; ok {
			in.Users[l.InboundID] = append(in.Users[l.InboundID], u)
		}
	}
	in.Peers = map[int64]map[int64]db.WGPeer{}
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if !inbounds.IsWG(ib.Protocol) {
			continue
		}
		ids := make([]int64, 0, len(in.Users[ib.ID]))
		for _, u := range in.Users[ib.ID] {
			ids = append(ids, u.ID)
		}
		ps, err := inbounds.EnsurePeers(gdb, ib, ids)
		if err != nil {
			slog.Warn("wireguard peers", "inbound", ib.Remark, "err", err)
			continue
		}
		in.Peers[ib.ID] = ps
	}
	return in, nil
}

// LoadMainInput is LoadInput for the main server: inbounds that run on
// nodes only are left out, so the main server does not listen on them.
func LoadMainInput(gdb *gorm.DB, dataDir string, apiPort int) (*Input, error) {
	in, err := LoadInput(gdb, dataDir, apiPort)
	if err != nil {
		return nil, err
	}
	if off := inbounds.NodeOnly(gdb); len(off) > 0 {
		kept := in.Inbounds[:0]
		for _, ib := range in.Inbounds {
			if !off[ib.ID] {
				kept = append(kept, ib)
			}
		}
		in.Inbounds = kept
	}
	return in, nil
}

func loadPolicy(gdb *gorm.DB, served map[int64]db.User) Policy {
	st := db.LoadSettings(gdb)
	p := Policy{BlockedIPs: map[string][]string{}, TorrentAll: st.BlockTorrent, AdBlockAll: st.AdBlock}
	var blocked []db.UserIP
	gdb.Where("blocked = ?", true).Order("user_id, ip").Find(&blocked)
	for _, b := range blocked {
		if u, ok := served[b.UserID]; ok {
			e := Email(&u)
			p.BlockedIPs[e] = append(p.BlockedIPs[e], b.IP)
		}
	}
	adGroups := map[int64]bool{}
	if !p.AdBlockAll {
		var gs []db.Group
		gdb.Where("ad_block = ?", true).Find(&gs)
		for _, g := range gs {
			adGroups[g.ID] = true
		}
	}
	ids := make([]int64, 0, len(served))
	for id := range served {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		u := served[id]
		if !p.TorrentAll && u.BlockTorrent {
			p.TorrentUsers = append(p.TorrentUsers, Email(&u))
		}
		if u.GroupID != nil && adGroups[*u.GroupID] {
			p.AdBlockUsers = append(p.AdBlockUsers, Email(&u))
		}
	}
	return p
}

// Build renders the Xray JSON config.
func Build(in *Input) ([]byte, error) {
	ibs := []any{
		obj{
			"tag": "api", "listen": "127.0.0.1", "port": in.APIPort, "protocol": "dokodemo-door",
			"settings": obj{"address": "127.0.0.1"},
		},
	}
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if inbounds.Engine(ib.Protocol) != inbounds.EngineXray {
			continue
		}
		if miss := inbounds.XrayMissing(ib, in.XrayVersion); len(miss) > 0 {
			slog.Warn("skipping inbound: Xray-core too old", "inbound", ib.Remark, "id", ib.ID, "xray", in.XrayVersion, "needs", inbounds.XrayMissingText(miss))
			in.Skipped = append(in.Skipped, fmt.Sprintf("%s: needs Xray-core %s (this server has %s)", ib.Remark, inbounds.XrayMissingText(miss), in.XrayVersion))
			continue
		}
		x, err := buildInbound(ib, in.Users[ib.ID], in.Peers[ib.ID], in.DataDir, in.TunOK)
		if err != nil {
			slog.Warn("skipping inbound", "inbound", ib.Remark, "id", ib.ID, "reason", err)
			in.Skipped = append(in.Skipped, fmt.Sprintf("%s: %v", ib.Remark, err))
			continue
		}
		ibs = append(ibs, x)
	}
	if b := bridgeInbound(in); b != nil {
		ibs = append(ibs, b)
	}
	rules := []any{
		obj{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
	}
	pr, warn := policyRules(in.Policy, fileExists(filepath.Join(in.AssetDir, "geosite.dat")), in.ProxyAliases)
	rules = append(rules, pr...)
	in.Skipped = append(in.Skipped, warn...)
	t := in.Template
	if t == nil {
		t = DefaultTemplate()
	}
	if t.BlocksPrivate() && fileExists(filepath.Join(in.AssetDir, "geoip.dat")) {
		rules = append(rules, obj{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": TagBlocked})
	}
	// Reverse proxies: after the private-network block, before the
	// admin's rules (their users always leave through the bridge).
	rules = append(rules, reverseRules(in.Inbounds)...)
	cfg := obj{
		"api":   obj{"tag": "api", "services": []string{"StatsService"}},
		"stats": obj{},
		"policy": obj{
			"levels": obj{"0": obj{"statsUserUplink": true, "statsUserDownlink": true, "statsUserOnline": true}},
			"system": obj{
				"statsInboundUplink": true, "statsInboundDownlink": true,
				"statsOutboundUplink": true, "statsOutboundDownlink": true,
			},
		},
		"inbounds": ibs,
	}
	renderTemplate(cfg, t, rules, in.Usernames, in.ProxyAliases)
	return json.MarshalIndent(cfg, "", "  ")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func buildInbound(ib *inbounds.View, us []db.User, peers map[int64]db.WGPeer, dataDir string, tunOK bool) (obj, error) {
	if !inbounds.IsV2(ib.Protocol) {
		return buildExtraInbound(ib, us, peers, dataDir, tunOK)
	}
	x := obj{"tag": ib.Tag(), "port": ib.Port, "protocol": ib.Protocol}
	if ib.Listen != "" {
		x["listen"] = ib.Listen
	} else {
		x["listen"] = "0.0.0.0"
	}
	clients := []any{}
	switch ib.Protocol {
	case "vless":
		for i := range us {
			c := obj{"id": us[i].UUID, "email": Email(&us[i])}
			if ib.Flow != "" {
				c["flow"] = ib.Flow
			}
			clients = append(clients, c)
		}
		if rc := reverseClient(ib); rc != nil {
			clients = append(clients, rc)
		}
		x["settings"] = obj{"clients": clients, "decryption": ib.VLESSEnc.Decryption()}
		if fb := fallbacksJSON(ib); fb != nil {
			x["settings"].(obj)["fallbacks"] = fb
		}
	case "vmess":
		for i := range us {
			clients = append(clients, obj{"id": us[i].UUID, "email": Email(&us[i])})
		}
		x["settings"] = obj{"clients": clients}
	case "trojan":
		for i := range us {
			clients = append(clients, obj{"password": us[i].Password, "email": Email(&us[i])})
		}
		x["settings"] = obj{"clients": clients}
		if fb := fallbacksJSON(ib); fb != nil {
			x["settings"].(obj)["fallbacks"] = fb
		}
	case "shadowsocks":
		if len(us) == 0 {
			return nil, fmt.Errorf("shadowsocks inbound has no users")
		}
		s := obj{"method": ib.SSMethod, "network": "tcp,udp"}
		// Shadowsocks itself listens on UDP over RAW only (unless switched
		// off); other transports carry TCP and UDP in their own stream.
		if ib.Network != "tcp" || (ib.Advanced != nil && ib.Advanced.SS != nil && ib.Advanced.SS.NoUDP) {
			s["network"] = "tcp"
		}
		if inbounds.IsSS2022(ib.SSMethod) {
			s["password"] = ib.SSServerKey
			for i := range us {
				clients = append(clients, obj{"password": inbounds.SS2022UserKey(ib.SSMethod, us[i].Password), "email": Email(&us[i])})
			}
		} else {
			for i := range us {
				clients = append(clients, obj{"method": ib.SSMethod, "password": us[i].Password, "email": Email(&us[i])})
			}
		}
		s["clients"] = clients
		x["settings"] = s
	default:
		return nil, fmt.Errorf("unknown protocol %q", ib.Protocol)
	}

	ss := obj{"network": ib.Network}
	t := ib.Transport
	switch ib.Network {
	case "ws":
		w := obj{"path": t.Path}
		if t.Host != "" {
			w["host"] = t.Host
		}
		ss["wsSettings"] = w
	case "grpc":
		ss["grpcSettings"] = obj{"serviceName": t.ServiceName, "multiMode": t.Mode == "multi"}
	case "httpupgrade":
		h := obj{"path": t.Path}
		if t.Host != "" {
			h["host"] = t.Host
		}
		ss["httpupgradeSettings"] = h
	case "xhttp":
		h := obj{"path": t.Path, "mode": t.Mode}
		if t.Host != "" {
			h["host"] = t.Host
		}
		ss["xhttpSettings"] = h
	}
	switch ib.Security {
	case "tls":
		ts, err := tlsSettings(ib, dataDir)
		if err != nil {
			return nil, err
		}
		ss["security"] = "tls"
		ss["tlsSettings"] = ts
	case "reality":
		r := ib.Reality
		if r == nil {
			return nil, fmt.Errorf("reality settings missing")
		}
		ss["security"] = "reality"
		rs := obj{
			"show": false, "dest": r.Dest, "xver": 0,
			"serverNames": r.ServerNames, "privateKey": r.PrivateKey, "shortIds": r.ShortIDs,
		}
		realityExtra(ib, rs)
		ss["realitySettings"] = rs
	default:
		ss["security"] = "none"
	}
	streamAdvanced(ib, ss)
	x["streamSettings"] = ss
	if sn := sniffObj(ib); sn != nil {
		x["sniffing"] = sn
	}
	return x, nil
}

// tlsSettings renders Xray server tlsSettings for an inbound.
func tlsSettings(ib *inbounds.View, dataDir string) (obj, error) {
	if ib.TLS == nil || ib.TLS.CertID == nil {
		return nil, fmt.Errorf("tls without certificate")
	}
	cf, kf := certs.Paths(dataDir, *ib.TLS.CertID)
	if !fileExists(cf) || !fileExists(kf) {
		return nil, fmt.Errorf("certificate %d files missing", *ib.TLS.CertID)
	}
	ts := obj{"certificates": []any{obj{"certificateFile": cf, "keyFile": kf}}}
	if len(ib.TLS.ALPN) > 0 {
		ts["alpn"] = ib.TLS.ALPN
	}
	if ib.TLS.SNI != "" {
		ts["serverName"] = ib.TLS.SNI
	}
	tlsExtra(ib, ts)
	return ts, nil
}

// buildExtraInbound renders the protocols that do not use the shared
// VLESS/VMess/Trojan/Shadowsocks stream settings.
func buildExtraInbound(ib *inbounds.View, us []db.User, peers map[int64]db.WGPeer, dataDir string, tunOK bool) (obj, error) {
	st := ib.Settings
	x := obj{"tag": ib.Tag(), "port": ib.Port, "listen": "0.0.0.0"}
	if ib.Listen != "" {
		x["listen"] = ib.Listen
	}
	tlsStream := func(network string, extra obj) (obj, error) {
		ts, err := tlsSettings(ib, dataDir)
		if err != nil {
			return nil, err
		}
		ss := obj{"network": network, "security": "tls", "tlsSettings": ts}
		for k, v := range extra {
			ss[k] = v
		}
		return ss, nil
	}
	accounts := func() []any {
		out := []any{}
		for i := range us {
			// Xray reports the authenticated user name as the stats email:
			// "<id>.<username>", or the user's custom proxy username, which
			// userkey maps back to the user.
			user, pass := users.ProxyLogin(&us[i])
			out = append(out, obj{"user": user, "pass": pass})
		}
		return out
	}
	switch ib.Protocol {
	case inbounds.Hysteria:
		cl := []any{}
		for i := range us {
			cl = append(cl, obj{"auth": us[i].Password, "email": Email(&us[i])})
		}
		x["protocol"] = "hysteria"
		x["settings"] = obj{"version": 2, "clients": cl}
		extra := obj{"hysteriaSettings": obj{"version": 2}}
		if st.ObfsPassword != "" {
			extra["finalmask"] = obj{"udp": []any{obj{"type": "salamander", "settings": obj{"password": st.ObfsPassword}}}}
		}
		ss, err := tlsStream("hysteria", extra)
		if err != nil {
			return nil, err
		}
		hysteriaExtra(ib, ss)
		x["streamSettings"] = ss
	case inbounds.Masque:
		cl := []any{}
		for i := range us {
			cl = append(cl, obj{"email": Email(&us[i]), "pass": us[i].Password})
		}
		x["protocol"] = "masque"
		x["settings"] = obj{"clients": cl, "address": st.Pool, "mtu": st.MTU}
		ss, err := tlsStream("masque", obj{"masqueSettings": obj{"path": st.Path}})
		if err != nil {
			return nil, err
		}
		x["streamSettings"] = ss
	case inbounds.WireGuard:
		ps := []any{}
		for i := range us {
			p, ok := peers[us[i].ID]
			if !ok {
				continue
			}
			ps = append(ps, obj{"publicKey": p.PublicKey, "preSharedKey": p.PresharedKey, "allowedIPs": []string{p.Address}, "email": Email(&us[i])})
		}
		x["protocol"] = "wireguard"
		x["settings"] = obj{"secretKey": st.PrivateKey, "peers": ps, "mtu": st.MTU}
	case inbounds.HTTP, inbounds.Mixed:
		// Xray's mixed (socks) inbound also serves HTTP proxy requests and,
		// unlike the plain http inbound, records the user for statistics.
		x["protocol"] = "mixed"
		x["settings"] = obj{"auth": "password", "accounts": accounts(), "udp": ib.Protocol == inbounds.Mixed && st.UDP}
		if ib.Protocol == inbounds.HTTP && ib.Security == "tls" {
			ss, err := tlsStream("raw", nil)
			if err != nil {
				return nil, err
			}
			x["streamSettings"] = ss
		}
	case inbounds.Socks:
		x["protocol"] = "socks"
		if st.NoAuth {
			// No accounts: anyone who reaches the port may use it, and
			// Xray cannot attribute the traffic to a user.
			x["settings"] = obj{"auth": "noauth", "udp": st.UDP}
		} else {
			x["settings"] = obj{"auth": "password", "accounts": accounts(), "udp": st.UDP}
		}
	case inbounds.Tunnel:
		x["protocol"] = "tunnel"
		x["settings"] = obj{"rewriteAddress": st.TargetAddress, "rewritePort": st.TargetPort, "allowedNetwork": st.TunnelNetwork}
	case inbounds.TUN:
		if !tunOK {
			return nil, fmt.Errorf("TUN needs CAP_NET_ADMIN and /dev/net/tun (see the installer's systemd unit)")
		}
		delete(x, "port")
		delete(x, "listen")
		t := obj{"name": st.TunName, "mtu": st.MTU, "gateway": st.Gateway}
		if len(st.Routes) > 0 {
			t["autoSystemRoutingTable"] = st.Routes
			t["autoOutboundsInterface"] = "auto"
		}
		x["protocol"] = "tun"
		x["settings"] = t
	default:
		return nil, fmt.Errorf("protocol %q is not served by xray", ib.Protocol)
	}
	if sn := sniffObj(ib); sn != nil {
		x["sniffing"] = sn
	}
	return x, nil
}

// BridgeTag is the tag of the loopback inbound fed by sing-box.
const BridgeTag = "bridge-singbox"

// bridgeInbound renders the loopback SOCKS inbound that receives the TUIC
// users' traffic from sing-box (one account per user, named like the stats
// email so traffic is attributed per user).
func bridgeInbound(in *Input) obj {
	if in.BridgePort == 0 {
		return nil
	}
	seen := map[int64]bool{}
	acc := []any{}
	for i := range in.Inbounds {
		ib := &in.Inbounds[i]
		if inbounds.Engine(ib.Protocol) != inbounds.EngineSingbox {
			continue
		}
		for _, u := range in.Users[ib.ID] {
			if !seen[u.ID] {
				seen[u.ID] = true
				acc = append(acc, obj{"user": Email(&u), "pass": u.Password})
			}
		}
	}
	if len(acc) == 0 {
		return nil
	}
	return obj{"tag": BridgeTag, "listen": "127.0.0.1", "port": in.BridgePort, "protocol": "mixed",
		"settings": obj{"auth": "password", "accounts": acc, "udp": true, "ip": "127.0.0.1"},
		"sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls", "quic"}}}
}
