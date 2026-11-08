package certs

import (
	"context"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// PublicIPResult is what DetectPublicIP found. External checkers can be
// wrong behind NAT or some cloud networks, so callers must always show the
// result to the admin and let them confirm or correct it.
type PublicIPResult struct {
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
	// Candidates lists every distinct public address seen, with where it came from.
	Candidates []IPCandidate `json:"candidates"`
}

// IPCandidate is one observed address.
type IPCandidate struct {
	IP      string   `json:"ip"`
	Family  string   `json:"family"` // ipv4 | ipv6
	Sources []string `json:"sources"`
}

// PublicIPCheckers are queried over IPv4 and IPv6 (each must return the
// bare address as text).
var PublicIPCheckers = []string{
	"https://api.ipify.org",
	"https://icanhazip.com",
	"https://ifconfig.me/ip",
	"https://checkip.amazonaws.com",
}

// IsPublicIP reports whether ip is a globally routable unicast address
// (what a public CA can validate).
func IsPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 carrier-grade NAT, 192.0.0.0/24, 198.18.0.0/15 benchmarking, 240/4.
		switch {
		case v4[0] == 100 && v4[1]&0xc0 == 64, v4[0] == 192 && v4[1] == 0 && v4[2] == 0,
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19), v4[0] >= 240:
			return false
		}
		return true
	}
	// 2001:db8::/32 documentation.
	return !(ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8)
}

func fetchIP(ctx context.Context, network, url string) net.IP {
	d := &net.Dialer{Timeout: 4 * time.Second}
	tr := &http.Transport{
		DialContext:         func(ctx context.Context, _, addr string) (net.Conn, error) { return d.DialContext(ctx, network, addr) },
		TLSHandshakeTimeout: 4 * time.Second,
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: 6 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "curl/8")
	resp, err := cl.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	ip := net.ParseIP(strings.TrimSpace(string(b)))
	if network == "tcp4" && (ip == nil || ip.To4() == nil) {
		return nil
	}
	if network == "tcp6" && (ip == nil || ip.To4() != nil) {
		return nil
	}
	return ip
}

// LocalPublicIPs returns public addresses configured on local interfaces.
func LocalPublicIPs() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && IsPublicIP(n.IP) {
			out = append(out, n.IP)
		}
	}
	return out
}

// DetectPublicIP asks several external checkers over IPv4 and IPv6 and looks
// at local interfaces. The most-reported address of each family wins.
func DetectPublicIP(ctx context.Context) PublicIPResult {
	type obs struct{ ip, src string }
	var mu sync.Mutex
	var seen []obs
	var wg sync.WaitGroup
	for _, u := range PublicIPCheckers {
		for _, nw := range []string{"tcp4", "tcp6"} {
			wg.Add(1)
			go func(u, nw string) {
				defer wg.Done()
				defer func() { _ = recover() }() // one bad checker must not crash the panel
				if ip := fetchIP(ctx, nw, u); ip != nil && IsPublicIP(ip) {
					host := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
					host, _, _ = strings.Cut(host, "/")
					mu.Lock()
					seen = append(seen, obs{ip.String(), host})
					mu.Unlock()
				}
			}(u, nw)
		}
	}
	wg.Wait()
	for _, ip := range LocalPublicIPs() {
		seen = append(seen, obs{ip.String(), "interface"})
	}
	return summarizeIPs(func(yield func(ip, src string)) {
		for _, o := range seen {
			yield(o.ip, o.src)
		}
	})
}

// summarizeIPs groups observations and picks the best per family: the
// address with most external confirmations (ties: one bound locally).
func summarizeIPs(each func(func(ip, src string))) PublicIPResult {
	idx := map[string]*IPCandidate{}
	var order []string
	each(func(ip, src string) {
		c := idx[ip]
		if c == nil {
			fam := "ipv6"
			if p := net.ParseIP(ip); p != nil && p.To4() != nil {
				fam = "ipv4"
			}
			c = &IPCandidate{IP: ip, Family: fam}
			idx[ip] = c
			order = append(order, ip)
		}
		c.Sources = append(c.Sources, src)
	})
	res := PublicIPResult{Candidates: []IPCandidate{}}
	for _, ip := range order {
		res.Candidates = append(res.Candidates, *idx[ip])
	}
	score := func(c IPCandidate) int {
		s := 0
		for _, src := range c.Sources {
			if src == "interface" {
				s++
			} else {
				s += 2
			}
		}
		return s
	}
	sort.SliceStable(res.Candidates, func(i, j int) bool { return score(res.Candidates[i]) > score(res.Candidates[j]) })
	for _, c := range res.Candidates {
		if c.Family == "ipv4" && res.IPv4 == "" {
			res.IPv4 = c.IP
		}
		if c.Family == "ipv6" && res.IPv6 == "" {
			res.IPv6 = c.IP
		}
	}
	return res
}
