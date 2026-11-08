package inbounds

import (
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func mk(remark, proto string, port int) *View {
	v := Defaults()
	v.Remark, v.Protocol, v.Port = remark, proto, port
	return &v
}

func TestUniquePortAndName(t *testing.T) {
	s := &Service{DB: testutil.DB(t), ReservedPorts: func() []int { return []int{2053} }}
	a, err := s.Create(mk("Main", VLESS, 443))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(mk("Second", VLESS, 8443))
	if err != nil {
		t.Fatal(err)
	}
	conflict := func(err error, field string) {
		t.Helper()
		var ce *ConflictError
		if !errors.As(err, &ce) || ce.Field != field {
			t.Fatalf("want %s conflict, got %v", field, err)
		}
	}
	// Same port, different protocol (shadowsocks) still conflicts.
	_, err = s.Create(mk("Other", Shadowsocks, 443))
	conflict(err, "port")
	// Same name, different case and spaces.
	_, err = s.Create(mk("  main ", VLESS, 9000))
	conflict(err, "remark")
	// Panel port is reserved.
	_, err = s.Create(mk("P", VLESS, 2053))
	conflict(err, "port")
	// Update to another inbound's port or name is rejected.
	b.Port = 443
	_, err = s.Update(b)
	conflict(err, "port")
	b.Port, b.Remark = 8443, "MAIN"
	_, err = s.Update(b)
	conflict(err, "remark")
	// Updating an inbound with its own port and name is fine.
	a.Remark = "Main"
	if _, err := s.Update(a); err != nil {
		t.Fatalf("self update: %v", err)
	}
}
