package inbounds

import (
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/testutil"
)

// No protocol gets a port on create: a request without one is refused, and
// Defaults (the base of every create request) carries none.
func TestCreateRequiresPort(t *testing.T) {
	if Defaults().Port != 0 {
		t.Fatalf("Defaults pre-fills port %d", Defaults().Port)
	}
	s := &Service{DB: testutil.DB(t)}
	for _, p := range Protocols {
		if p == TUN {
			continue // TUN has no port at all
		}
		v := Defaults()
		v.Remark, v.Protocol = "x-"+p, p
		_, err := s.Create(&v)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("%s: create without a port: err = %v, want a validation error", p, err)
		}
	}
	var n int64
	s.DB.Table("inbounds").Count(&n)
	if n != 0 {
		t.Fatalf("%d inbounds stored without a port", n)
	}
}
