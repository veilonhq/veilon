package inbounds

import (
	"errors"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// Hop ranges are locked per server like ports: a second Hysteria 2 inbound
// on the same server may not take (part of) a range another one hops over,
// on create or on edit, and a UDP port may not sit inside one.
func TestHopRangeLockedPerServer(t *testing.T) {
	s, n1, _ := portSvc(t)
	s.SelfSigned = func(d string) (int64, error) {
		c := db.Cert{Domain: d, Source: "self", CertPEM: "x", KeyPEM: "y"}
		return c.ID, s.DB.Create(&c).Error
	}
	hy := func(remark string, port int, hop string, main bool, nodes ...int64) *View {
		v := onNodes(remark, Hysteria, port, main, nodes...)
		v.Settings.HopPorts = hop
		return v
	}
	wantHop := func(err error, part string) {
		t.Helper()
		var ce *ConflictError
		if !errors.As(err, &ce) || ce.Field != "hop_ports" || !strings.Contains(ce.Msg, part) {
			t.Fatalf("want hop conflict %q, got %v", part, err)
		}
	}
	if _, err := s.Create(hy("a", 8443, "20000-30000", true)); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(hy("b", 9443, "25000-26000", true))
	wantHop(err, `overlaps hop range 20000-30000 of inbound "a"`)
	_, err = s.Create(hy("b", 9443, "30000-31000", true))
	wantHop(err, "overlaps")
	_, err = s.Create(hy("b", 21000, "", true))
	wantHop(err, "UDP port 21000 lies in hop range 20000-30000")
	// Edit: an inbound without hopping may not take the range later.
	c, err := s.Create(hy("c", 9444, "", true))
	if err != nil {
		t.Fatal(err)
	}
	c.Settings.HopPorts = "29000-29100"
	_, err = s.Update(c)
	wantHop(err, "overlaps")
	// Next to it, or on another server, is fine.
	c.Settings.HopPorts = "30001-31000"
	if _, err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(hy("d", 9445, "20000-30000", false, n1)); err != nil {
		t.Fatal(err)
	}
	// Different listen addresses do not share packets.
	a2 := hy("e", 9446, "40000-41000", true)
	a2.Listen = "10.0.0.1"
	if _, err := s.Create(a2); err != nil {
		t.Fatal(err)
	}
	b2 := hy("f", 9447, "40500-40600", true)
	b2.Listen = "10.0.0.2"
	if _, err := s.Create(b2); err != nil {
		t.Fatal(err)
	}
}
