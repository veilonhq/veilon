package xray

import (
	"strconv"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

// The main server's config leaves out an inbound its Xray-core is too old
// for (with a warning) and keeps the others; an unknown version keeps all.
func TestBuildSkipsInboundForOldXray(t *testing.T) {
	gg, dir, rid, wid := nodeFixture(t)
	g := &inbounds.Service{DB: gg}
	ml, _ := inbounds.GenerateMLDSA65("")
	v, _ := g.Get(rid)
	v.Advanced = &inbounds.Advanced{Reality: &inbounds.RealityExtra{MLDSA65Seed: ml.Server}}
	if _, err := g.Update(v); err != nil {
		t.Fatal(err)
	}
	build := func(ver string) (string, []string) {
		in, err := LoadMainInput(gg, dir, 62051)
		if err != nil {
			t.Fatal(err)
		}
		in.XrayVersion = ver
		b, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		return string(b), in.Skipped
	}
	tag := func(id int64) string { return `"inbound-` + strconv.FormatInt(id, 10) + `"` }
	cfg, skipped := build("25.6.8")
	if strings.Contains(cfg, tag(rid)) || !strings.Contains(cfg, tag(wid)) {
		t.Fatalf("old xray config:\n%s", cfg)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "Reality ML-DSA-65 (25.7.26+)") {
		t.Fatalf("skipped %v", skipped)
	}
	for _, ver := range []string{"", "25.7.26", "26.10.31"} {
		if cfg, skipped := build(ver); !strings.Contains(cfg, tag(rid)) || len(skipped) != 0 {
			t.Fatalf("version %q: skipped %v", ver, skipped)
		}
	}
}
