package guard

import (
	"net"
	"os"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// Countries looks up the country codes of ips in an Xray geoip.dat file
// (GeoIPList{entry=1: GeoIP{country_code=1, cidr=2: CIDR{ip=1, prefix=2}}}).
// Only two-letter codes are returned; the file is read once per call and
// not kept in memory, so call it with a batch of new IPs.
func Countries(path string, ips []string) map[string]string {
	out := map[string]string{}
	if len(ips) == 0 {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	return countriesIn(b, ips)
}

func countriesIn(b []byte, ips []string) map[string]string {
	out := map[string]string{}
	parsed := make([]net.IP, 0, len(ips))
	names := make([]string, 0, len(ips))
	for _, s := range ips {
		if p := net.ParseIP(s); p != nil {
			parsed = append(parsed, p)
			names = append(names, s)
		}
	}
	for len(b) > 0 && len(out) < len(parsed) {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return out
		}
		b = b[n:]
		if num != 1 || typ != protowire.BytesType {
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				return out
			}
			b = b[m:]
			continue
		}
		entry, m := protowire.ConsumeBytes(b)
		if m < 0 {
			return out
		}
		b = b[m:]
		matchEntry(entry, parsed, names, out)
	}
	return out
}

func matchEntry(e []byte, ips []net.IP, names []string, out map[string]string) {
	code := ""
	var cidrs [][]byte
	reverse := false
	for len(e) > 0 {
		num, typ, n := protowire.ConsumeTag(e)
		if n < 0 {
			return
		}
		e = e[n:]
		switch {
		case num == 1 && typ == protowire.BytesType:
			v, m := protowire.ConsumeString(e)
			if m < 0 {
				return
			}
			code, e = v, e[m:]
		case num == 2 && typ == protowire.BytesType:
			v, m := protowire.ConsumeBytes(e)
			if m < 0 {
				return
			}
			cidrs, e = append(cidrs, v), e[m:]
		case num == 3 && typ == protowire.VarintType:
			v, m := protowire.ConsumeVarint(e)
			if m < 0 {
				return
			}
			reverse, e = v != 0, e[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, e)
			if m < 0 {
				return
			}
			e = e[m:]
		}
	}
	if len(code) != 2 || reverse {
		return
	}
	code = strings.ToUpper(code)
	for _, c := range cidrs {
		var ip []byte
		var prefix uint64
		for len(c) > 0 {
			num, typ, n := protowire.ConsumeTag(c)
			if n < 0 {
				break
			}
			c = c[n:]
			switch {
			case num == 1 && typ == protowire.BytesType:
				v, m := protowire.ConsumeBytes(c)
				if m < 0 {
					c = nil
					continue
				}
				ip, c = v, c[m:]
			case num == 2 && typ == protowire.VarintType:
				v, m := protowire.ConsumeVarint(c)
				if m < 0 {
					c = nil
					continue
				}
				prefix, c = v, c[m:]
			default:
				m := protowire.ConsumeFieldValue(num, typ, c)
				if m < 0 {
					c = nil
					continue
				}
				c = c[m:]
			}
		}
		if len(ip) != 4 && len(ip) != 16 {
			continue
		}
		nw := &net.IPNet{IP: net.IP(ip), Mask: net.CIDRMask(int(prefix), len(ip)*8)}
		for i, p := range ips {
			if _, done := out[names[i]]; done {
				continue
			}
			q := p
			if len(ip) == 4 {
				if q = p.To4(); q == nil {
					continue
				}
			} else if p.To4() != nil {
				continue
			}
			if nw.Contains(q) {
				out[names[i]] = code
			}
		}
	}
}

// EncodeGeoIP builds a geoip.dat body (used by tests).
func EncodeGeoIP(entries map[string][]string) []byte {
	var b []byte
	for code, nets := range entries {
		var e []byte
		e = protowire.AppendTag(e, 1, protowire.BytesType)
		e = protowire.AppendString(e, code)
		for _, s := range nets {
			_, n, err := net.ParseCIDR(s)
			if err != nil {
				continue
			}
			ip := n.IP
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			ones, _ := n.Mask.Size()
			var c []byte
			c = protowire.AppendTag(c, 1, protowire.BytesType)
			c = protowire.AppendBytes(c, ip)
			c = protowire.AppendTag(c, 2, protowire.VarintType)
			c = protowire.AppendVarint(c, uint64(ones))
			e = protowire.AppendTag(e, 2, protowire.BytesType)
			e = protowire.AppendBytes(e, c)
		}
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, e)
	}
	return b
}
