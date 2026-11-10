package inbounds

import (
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestSetHosts(t *testing.T) {
	gdb := testutil.DB(t)
	s := &Service{DB: gdb}
	v := Defaults()
	v.Remark, v.Protocol, v.Port = "vm", "vmess", 20001
	ib, err := s.Create(&v)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	hs, err := s.SetHosts(ib.ID, []HostInput{
		{Remark: "a", Address: "a.example.com", Port: 443, ALPN: "h2, http/1.1"},
		{Address: "1.2.3.4", Path: "/x", Enabled: &off},
	})
	if err != nil || len(hs) != 2 || hs[0].Sort != 0 || hs[1].Sort != 1 || hs[0].ALPN != "h2,http/1.1" || hs[1].Enabled {
		t.Fatalf("set: %+v %v", hs, err)
	}
	if m := EnabledHosts(gdb, []int64{ib.ID}); len(m[ib.ID]) != 1 {
		t.Fatalf("enabled hosts %+v", m)
	}
	for _, bad := range []HostInput{{Address: "bad host"}, {Port: 70000}, {Path: "nope"}, {Fingerprint: "netscape"}, {ALPN: "h9"}, {SNI: "a b"}} {
		var ve *ValidationError
		if _, err := s.SetHosts(ib.ID, []HostInput{bad}); !errors.As(err, &ve) {
			t.Fatalf("%+v: want validation error, got %v", bad, err)
		}
	}
	// a rejected list leaves the saved hosts alone
	if hs, _ := s.Hosts(ib.ID); len(hs) != 2 {
		t.Fatal("hosts lost after a rejected update")
	}
	got, _ := s.Get(ib.ID)
	if got.HostCount != 2 {
		t.Fatalf("host count %d", got.HostCount)
	}
	if err := s.Delete(ib.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	gdb.Table("inbound_hosts").Count(&n)
	if n != 0 {
		t.Fatal("hosts of a deleted inbound remain")
	}
	if _, err := s.SetHosts(999, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}
