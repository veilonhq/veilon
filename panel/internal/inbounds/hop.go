package inbounds

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Hysteria 2 port hopping.
//
// Clients of a Hysteria 2 inbound with hop ports send to a random port of
// the ranges and switch to another one every few seconds; the server
// redirects the ranges (UDP only) to the inbound's listen port with an
// nftables DNAT rule (package porthop on the main server and on nodes).

// HopRange is one inclusive UDP port range.
type HopRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// MaxHopRanges is how many ranges one inbound may hop over.
const MaxHopRanges = 4

// ParseHopPorts parses "20000-30000", "20000-30000,40000-40100" or single
// ports ("5000"). Ranges are sorted and may not overlap. "" is no hopping.
func ParseHopPorts(s string) ([]HopRange, error) {
	s = strings.TrimSpace(strings.NewReplacer("،", ",", " ", "", "\u00a0", "", ":", "-").Replace(toLatinDigits(s)))
	if s == "" {
		return nil, nil
	}
	var out []HopRange
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			continue
		}
		a, b, isRange := strings.Cut(part, "-")
		from, err1 := strconv.Atoi(a)
		to := from
		var err2 error
		if isRange {
			to, err2 = strconv.Atoi(b)
		}
		if err1 != nil || err2 != nil {
			return nil, verr("settings.hop_ports: %q is not a port or a range like 20000-30000", part)
		}
		if from < 1024 || to > 65535 || from > to {
			return nil, verr("settings.hop_ports: %q must be inside 1024-65535 with the smaller port first", part)
		}
		out = append(out, HopRange{from, to})
	}
	if len(out) > MaxHopRanges {
		return nil, verr("settings.hop_ports: at most %d ranges", MaxHopRanges)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	for i := 1; i < len(out); i++ {
		if out[i].From <= out[i-1].To {
			return nil, verr("settings.hop_ports: ranges %s and %s overlap", out[i-1], out[i])
		}
	}
	return out, nil
}

func (r HopRange) String() string {
	if r.From == r.To {
		return strconv.Itoa(r.From)
	}
	return fmt.Sprintf("%d-%d", r.From, r.To)
}

// Contains reports whether port lies in the range.
func (r HopRange) Contains(port int) bool { return port >= r.From && port <= r.To }

// FormatHopPorts renders ranges as stored and shown ("20000-30000,40000").
func FormatHopPorts(rs []HopRange) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = r.String()
	}
	return strings.Join(parts, ",")
}

// HopRanges returns the inbound's hop ranges (nil when it does not hop).
func (v *View) HopRanges() []HopRange {
	if v.Protocol != Hysteria || v.Settings.HopPorts == "" {
		return nil
	}
	rs, err := ParseHopPorts(v.Settings.HopPorts)
	if err != nil {
		return nil
	}
	return rs
}

func toLatinDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '۰' && r <= '۹':
			return '0' + (r - '۰')
		case r >= '٠' && r <= '٩':
			return '0' + (r - '٠')
		}
		return r
	}, s)
}

// checkHop rejects UDP port clashes with hop ranges on a shared server:
// v's own ranges may not cover another UDP inbound's port or overlap
// another inbound's ranges, and v's UDP port may not lie in another
// inbound's ranges (the redirect would take its packets).
func (s *Service) checkHop(v *View) error {
	if v.Protocol == TUN {
		return nil
	}
	mine := v.HopRanges()
	_, udp := Transports(v)
	if len(mine) == 0 && !udp {
		return nil
	}
	var rows []db.Inbound
	if err := s.DB.Where("id <> ? AND protocol <> ?", v.ID, TUN).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	want := serversOf(v.ServeMain, v.NodeIDs)
	placed := Placements(s.DB)
	for i := range rows {
		o := FromModel(&rows[i])
		main, node, ok := want.shared(serversOf(o.ServeMain, placed[o.ID]))
		if !ok || !listenOverlap(o.Listen, v.Listen) {
			continue
		}
		theirs := o.HopRanges()
		_, ou := Transports(&o)
		for _, r := range mine {
			if ou && r.Contains(o.Port) {
				return s.hopConflict(v, main, node, o.Remark, fmt.Sprintf("hop range %s covers UDP port %d of inbound %q", r, o.Port, o.Remark))
			}
			for _, t := range theirs {
				if r.From <= t.To && t.From <= r.To {
					return s.hopConflict(v, main, node, o.Remark, fmt.Sprintf("hop range %s overlaps hop range %s of inbound %q", r, t, o.Remark))
				}
			}
		}
		if udp {
			for _, t := range theirs {
				if t.Contains(v.Port) {
					return s.hopConflict(v, main, node, o.Remark, fmt.Sprintf("UDP port %d lies in hop range %s of inbound %q", v.Port, t, o.Remark))
				}
			}
		}
	}
	return nil
}

func (s *Service) hopConflict(v *View, main bool, node int64, other, msg string) error {
	ce := s.portConflict(v.Port, main, node, other)
	ce.Field = "hop_ports"
	where := "the main server"
	if !main {
		where = "node " + ce.NodeName
	}
	ce.Msg = fmt.Sprintf("Port hopping clash on %s: %s", where, msg)
	return ce
}
