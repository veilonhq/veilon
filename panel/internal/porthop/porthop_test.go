package porthop

import (
	"errors"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/inbounds"
)

func TestScript(t *testing.T) {
	if Script(nil) != "" {
		t.Fatal("no rules must give no script")
	}
	s := Script([]Rule{
		{InboundID: 4, Port: 443, Ranges: []inbounds.HopRange{{From: 20000, To: 30000}}},
		{InboundID: 5, Listen: "1.2.3.4", Port: 8443, Ranges: []inbounds.HopRange{{From: 40000, To: 40100}, {From: 41000, To: 41000}}},
		{InboundID: 6, Listen: "0.0.0.0", Port: 9443, Ranges: []inbounds.HopRange{{From: 50000, To: 50010}}},
		{InboundID: 7, Listen: "2001:db8::1", Port: 9444, Ranges: []inbounds.HopRange{{From: 51000, To: 51010}}},
	})
	for _, want := range []string{
		"table inet veilon_hop\ndelete table inet veilon_hop\ntable inet veilon_hop {",
		"type nat hook prerouting priority dstnat; policy accept;",
		`udp dport 20000-30000 redirect to :443 comment "inbound-4"`,
		`ip daddr 1.2.3.4 udp dport { 40000-40100, 41000 } dnat ip to 1.2.3.4:8443 comment "inbound-5"`,
		`meta nfproto ipv4 udp dport 50000-50010 redirect to :9443 comment "inbound-6"`,
		`ip6 daddr 2001:db8::1 udp dport 51000-51010 dnat ip6 to [2001:db8::1]:9444 comment "inbound-7"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "output") {
		t.Fatal("the server's own traffic must never be redirected")
	}
}

func TestFromViews(t *testing.T) {
	vs := []inbounds.View{
		{ID: 2, Protocol: inbounds.Hysteria, Port: 443, Settings: inbounds.Settings{HopPorts: "20000-20010"}},
		{ID: 1, Protocol: inbounds.Hysteria, Port: 444},
		{ID: 3, Protocol: inbounds.TUIC, Port: 445, Settings: inbounds.Settings{HopPorts: "30000-30010"}},
	}
	r := FromViews(vs)
	if len(r) != 1 || r[0].InboundID != 2 || r[0].Ranges[0] != (inbounds.HopRange{From: 20000, To: 20010}) {
		t.Fatalf("rules %+v", r)
	}
	if err := Validate(r); err != nil {
		t.Fatal(err)
	}
	if Validate([]Rule{{InboundID: 1, Port: 443, Ranges: []inbounds.HopRange{{From: 80, To: 90}}}}) == nil {
		t.Fatal("a range below 1024 must be refused")
	}
}

func TestApplier(t *testing.T) {
	var runs []string
	deletes, exists := 0, true
	fail := false
	a := &Applier{
		Run: func(s string) error {
			if fail {
				return errors.New("nft missing")
			}
			runs = append(runs, s)
			return nil
		},
		Delete: func() error { deletes++; return nil },
		Exists: func() bool { return exists },
	}
	// First call always acts (a table left by a crash goes away).
	_ = a.Apply(nil)
	if deletes != 1 {
		t.Fatal("first apply must clear a stale table")
	}
	rules := []Rule{{InboundID: 1, Port: 443, Ranges: []inbounds.HopRange{{From: 20000, To: 20010}}}}
	_ = a.Apply(rules)
	_ = a.Apply(rules)
	if len(runs) != 1 {
		t.Fatalf("unchanged rules applied again: %d", len(runs))
	}
	fail = true
	rules2 := []Rule{{InboundID: 1, Port: 443, Ranges: []inbounds.HopRange{{From: 20000, To: 20020}}}}
	if a.Apply(rules2) == nil || a.Err() == "" {
		t.Fatal("error not reported")
	}
	fail = false
	_ = a.Apply(rules2)
	if a.Err() != "" || len(runs) != 2 {
		t.Fatal("retry after an error")
	}
	_ = a.Clear()
	if deletes != 2 {
		t.Fatal("clear must delete the table")
	}
}
