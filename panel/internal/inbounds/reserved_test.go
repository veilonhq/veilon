package inbounds

import "testing"

func TestReservedTunName(t *testing.T) {
	for _, n := range []string{"vt-x", "ve-a", "vb-1", "vz-tun", "host0", "VE-A", "HOST0"} {
		if !reservedTunName(n) {
			t.Errorf("%q should be reserved", n)
		}
	}
	for _, n := range []string{"veilon-tun", "vtun", "host1", "tun0", "vex"} {
		if reservedTunName(n) {
			t.Errorf("%q should be allowed", n)
		}
	}
}

func TestRealityFingerprints(t *testing.T) {
	for _, f := range []string{"chrome", "firefox", "safari"} {
		if !in(f, RealityFingerprints) {
			t.Errorf("%s should be allowed", f)
		}
	}
	for _, f := range []string{"edge", "ios", "android", "random", "randomized", "360"} {
		if in(f, RealityFingerprints) {
			t.Errorf("%s should be rejected", f)
		}
	}
}
