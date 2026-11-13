package xray

import (
	"strconv"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// This file renders an inbound's advanced settings (inbounds.Advanced).
// Every helper adds nothing when its setting is unset, so inbounds without
// advanced settings keep the exact config older panels wrote.

func adv(ib *inbounds.View) *inbounds.Advanced {
	if ib.Advanced == nil {
		return &inbounds.Advanced{}
	}
	return ib.Advanced
}

// sniffObj renders the inbound "sniffing" block (nil when off).
func sniffObj(ib *inbounds.View) obj {
	if !ib.Sniffing {
		return nil
	}
	o := obj{"enabled": true, "destOverride": inbounds.DefaultSniffDest}
	if s := adv(ib).Sniffing; s != nil {
		if len(s.DestOverride) > 0 {
			o["destOverride"] = s.DestOverride
		}
		if s.RouteOnly {
			o["routeOnly"] = true
		}
		if s.MetadataOnly {
			o["metadataOnly"] = true
		}
		if len(s.DomainsExcluded) > 0 {
			o["domainsExcluded"] = s.DomainsExcluded
		}
	}
	return o
}

// fallbacksJSON renders VLESS/Trojan "fallbacks" (nil when none).
func fallbacksJSON(ib *inbounds.View) []any {
	fs := adv(ib).Fallbacks
	if len(fs) == 0 {
		return nil
	}
	out := make([]any, 0, len(fs))
	for _, f := range fs {
		o := obj{}
		if n, err := strconv.Atoi(f.Dest); err == nil {
			o["dest"] = n
		} else {
			o["dest"] = f.Dest
		}
		if f.Name != "" {
			o["name"] = f.Name
		}
		if f.ALPN != "" {
			o["alpn"] = f.ALPN
		}
		if f.Path != "" {
			o["path"] = f.Path
		}
		if f.Xver > 0 {
			o["xver"] = f.Xver
		}
		out = append(out, o)
	}
	return out
}

// reverseClient is the VLESS client a reverse-proxy bridge connects as;
// its connection creates the outbound inbounds.ReverseTag on this Xray.
func reverseClient(ib *inbounds.View) obj {
	if !ib.ReverseOn() {
		return nil
	}
	c := obj{"id": ib.Advanced.Reverse.UUID, "email": inbounds.ReverseEmail(ib.ID), "reverse": obj{"tag": inbounds.ReverseTag(ib.ID)}}
	if ib.Flow != "" {
		c["flow"] = ib.Flow
	}
	return c
}

// reverseRules route the users of the inbounds a reverse proxy serves to
// its bridge. While no bridge is connected their traffic is dropped (Xray
// never falls back to another outbound for a reverse tag).
func reverseRules(ibs []inbounds.View) []any {
	present := map[int64]bool{}
	for i := range ibs {
		present[ibs[i].ID] = true
	}
	var rules []any
	for i := range ibs {
		ib := &ibs[i]
		if !ib.ReverseOn() {
			continue
		}
		r := ib.Advanced.Reverse
		var tags []string
		if r.RouteSelf {
			tags = append(tags, ib.Tag())
		}
		for _, id := range r.RouteInboundIDs {
			if present[id] {
				tags = append(tags, (&inbounds.View{ID: id}).Tag())
			}
		}
		if len(tags) == 0 {
			continue
		}
		rules = append(rules, obj{"type": "field", "inboundTag": tags, "outboundTag": inbounds.ReverseTag(ib.ID)})
	}
	return rules
}

// tlsExtra adds TLS extras to server tlsSettings.
func tlsExtra(ib *inbounds.View, ts obj) {
	t := adv(ib).TLS
	if t == nil {
		return
	}
	if t.MinVersion != "" {
		ts["minVersion"] = t.MinVersion
	}
	if t.MaxVersion != "" {
		ts["maxVersion"] = t.MaxVersion
	}
	if t.CipherSuites != "" {
		ts["cipherSuites"] = t.CipherSuites
	}
	if t.RejectUnknownSNI {
		ts["rejectUnknownSni"] = true
	}
	if t.ECHServerKeys != "" {
		ts["echServerKeys"] = t.ECHServerKeys
	}
}

// realityExtra adds Reality extras to server realitySettings.
func realityExtra(ib *inbounds.View, rs obj) {
	r := adv(ib).Reality
	if r == nil {
		return
	}
	if r.MLDSA65Seed != "" {
		rs["mldsa65Seed"] = r.MLDSA65Seed
	}
	if r.MinClientVer != "" {
		rs["minClientVer"] = r.MinClientVer
	}
	if r.MaxClientVer != "" {
		rs["maxClientVer"] = r.MaxClientVer
	}
	if r.MaxTimeDiff > 0 {
		rs["maxTimeDiff"] = r.MaxTimeDiff
	}
	lim := func(l *inbounds.LimitFallback) obj {
		return obj{"afterBytes": l.AfterBytes, "bytesPerSec": l.BytesPerSec, "burstBytesPerSec": l.BurstBytesPerSec}
	}
	if r.LimitUpload != nil {
		rs["limitFallbackUpload"] = lim(r.LimitUpload)
	}
	if r.LimitDownload != nil {
		rs["limitFallbackDownload"] = lim(r.LimitDownload)
	}
}

// TCPHeaderJSON renders the RAW "header" of HTTP obfuscation for the
// server and for clients (request and response are the same).
func TCPHeaderJSON(h *inbounds.TCPHeader) obj {
	req := obj{"version": "1.1", "method": "GET", "path": []string{"/"},
		"headers": obj{"User-Agent": []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"},
			"Accept-Encoding": []string{"gzip, deflate"}, "Connection": []string{"keep-alive"}, "Pragma": []string{"no-cache"}}}
	if len(h.Path) > 0 {
		req["path"] = h.Path
	}
	if len(h.Host) > 0 {
		req["headers"].(obj)["Host"] = h.Host
	}
	resp := obj{"version": "1.1", "status": "200", "reason": "OK",
		"headers": obj{"Content-Type": []string{"application/octet-stream", "video/mpeg"}, "Transfer-Encoding": []string{"chunked"},
			"Connection": []string{"keep-alive"}, "Pragma": []string{"no-cache"}}}
	return obj{"type": "http", "request": req, "response": resp}
}

// KCPSettings renders "kcpSettings" (nil when every value is the default).
func KCPSettings(k *inbounds.KCPExtra) obj {
	if k == nil {
		return nil
	}
	o := obj{}
	set := func(key string, v int) {
		if v > 0 {
			o[key] = v
		}
	}
	set("mtu", k.MTU)
	set("tti", k.TTI)
	set("uplinkCapacity", k.UplinkCapacity)
	set("downlinkCapacity", k.DownlinkCapacity)
	set("cwndMultiplier", k.CwndMultiplier)
	set("maxSendingWindow", k.MaxSendingWindow)
	if len(o) == 0 {
		return nil
	}
	return o
}

// KCPMasks renders the mKCP header and seed as "mkcp-legacy" UDP masks:
// the header next to the wire, then the seed (AES-128-GCM).
func KCPMasks(k *inbounds.KCPExtra) []any {
	if k == nil {
		return nil
	}
	var out []any
	if h := inbounds.KCPXrayHeader(k.Header); h != "" {
		s := obj{"header": h}
		if h == "dns" && k.HeaderDomain != "" {
			s["value"] = k.HeaderDomain
		}
		out = append(out, obj{"type": "mkcp-legacy", "settings": s})
	}
	if k.Seed != "" {
		out = append(out, obj{"type": "mkcp-legacy", "settings": obj{"value": k.Seed}})
	}
	return out
}

// XHTTPServerExtra adds the server-side XHTTP options.
func XHTTPServerExtra(x *inbounds.XHTTPExtra, h obj) {
	if x == nil {
		return
	}
	if x.XPaddingBytes != "" {
		h["xPaddingBytes"] = x.XPaddingBytes
	}
	if x.NoSSEHeader {
		h["noSSEHeader"] = true
	}
	if x.ScMaxEachPostBytes != "" {
		h["scMaxEachPostBytes"] = x.ScMaxEachPostBytes
	}
	if x.ScMaxBufferedPosts > 0 {
		h["scMaxBufferedPosts"] = x.ScMaxBufferedPosts
	}
	if x.ScStreamUpServerSecs != "" {
		h["scStreamUpServerSecs"] = x.ScStreamUpServerSecs
	}
}

// streamAdvanced adds transport, socket and mask options to a V2 inbound's
// streamSettings.
func streamAdvanced(ib *inbounds.View, ss obj) {
	a := adv(ib)
	switch ib.Network {
	case "tcp":
		if a.TCP != nil {
			ss["rawSettings"] = obj{"header": TCPHeaderJSON(a.TCP)}
		}
	case "ws":
		if a.WS != nil && a.WS.HeartbeatPeriod > 0 {
			ss["wsSettings"].(obj)["heartbeatPeriod"] = a.WS.HeartbeatPeriod
		}
	case "grpc":
		if g := a.GRPC; g != nil {
			gs := ss["grpcSettings"].(obj)
			if g.IdleTimeout > 0 {
				gs["idle_timeout"] = g.IdleTimeout
			}
			if g.HealthCheckTimeout > 0 {
				gs["health_check_timeout"] = g.HealthCheckTimeout
			}
			if g.PermitWithoutStream {
				gs["permit_without_stream"] = true
			}
			if g.InitialWindowsSize > 0 {
				gs["initial_windows_size"] = g.InitialWindowsSize
			}
		}
	case "xhttp":
		XHTTPServerExtra(a.XHTTP, ss["xhttpSettings"].(obj))
	case "kcp":
		if k := KCPSettings(a.KCP); k != nil {
			ss["kcpSettings"] = k
		}
	}
	if s := a.Sockopt; s != nil {
		so := obj{}
		if s.AcceptProxyProtocol {
			so["acceptProxyProtocol"] = true
		}
		if s.TCPFastOpen {
			so["tcpFastOpen"] = true
		}
		if s.KeepAliveInterval > 0 {
			so["tcpKeepAliveInterval"] = s.KeepAliveInterval
		}
		if s.KeepAliveIdle > 0 {
			so["tcpKeepAliveIdle"] = s.KeepAliveIdle
		}
		if s.Mark > 0 {
			so["mark"] = s.Mark
		}
		if s.V6Only {
			so["v6only"] = true
		}
		if s.Congestion != "" {
			so["tcpCongestion"] = s.Congestion
		}
		if len(so) > 0 {
			ss["sockopt"] = so
		}
	}
	var udp []any
	if ib.Network == "kcp" {
		udp = KCPMasks(a.KCP)
	}
	addMasks(ib, ss, udp)
}

// addMasks writes "finalmask": server TCP fragment, UDP noise (next to the
// wire, ahead of any other UDP mask) and the given UDP masks.
func addMasks(ib *inbounds.View, ss obj, udp []any) {
	var tcp []any
	if m := adv(ib).Masks; m != nil {
		if f := m.Fragment; f != nil {
			s := obj{"packets": f.Packets, "length": f.Length}
			if f.Delay != "" {
				s["delay"] = f.Delay
			}
			tcp = append(tcp, obj{"type": "fragment", "settings": s})
		}
		if n := m.Noise; n != nil {
			item := obj{"rand": n.Rand}
			if n.Delay != "" {
				item["delay"] = n.Delay
			}
			udp = append([]any{obj{"type": "noise", "settings": obj{"noise": []any{item}}}}, udp...)
		}
	}
	if len(tcp) == 0 && len(udp) == 0 {
		return
	}
	fm, _ := ss["finalmask"].(obj)
	if fm == nil {
		fm = obj{}
	}
	if len(tcp) > 0 {
		fm["tcp"] = tcp
	}
	if len(udp) > 0 {
		old, _ := fm["udp"].([]any)
		fm["udp"] = append(udp, old...)
	}
	ss["finalmask"] = fm
}

// hysteriaExtra adds Hysteria 2 server options to its stream settings.
func hysteriaExtra(ib *inbounds.View, ss obj) {
	h := adv(ib).Hysteria
	if h != nil {
		hs := ss["hysteriaSettings"].(obj)
		if h.UDPIdleTimeout > 0 {
			hs["udpIdleTimeout"] = h.UDPIdleTimeout
		}
		if m := h.Masquerade; m != nil {
			mo := obj{"type": m.Type}
			switch m.Type {
			case "proxy":
				mo["url"] = m.URL
				if m.RewriteHost {
					mo["rewriteHost"] = true
				}
			case "string":
				mo["content"] = m.Content
				if m.StatusCode > 0 {
					mo["statusCode"] = m.StatusCode
				}
				if len(m.Headers) > 0 {
					mo["headers"] = m.Headers
				}
			}
			hs["masquerade"] = mo
		}
		if h.Congestion != "" {
			q := obj{"congestion": h.Congestion}
			if h.BrutalUpMbps > 0 {
				q["brutalUp"] = strconv.Itoa(h.BrutalUpMbps) + " mbps"
			}
			if h.BrutalDownMbps > 0 {
				q["brutalDown"] = strconv.Itoa(h.BrutalDownMbps) + " mbps"
			}
			fm, _ := ss["finalmask"].(obj)
			if fm == nil {
				fm = obj{}
			}
			fm["quicParams"] = q
			ss["finalmask"] = fm
		}
	}
	addMasks(ib, ss, nil)
}
