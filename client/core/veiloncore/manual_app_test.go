package veiloncore

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// TestManualProfilesFromApp checks the profiles the app's "Add manually"
// form saved (written by app/test/v104_test.dart) with the real core: the
// JSON decodes and normalizes, CheckProfile accepts it (sing-box / Xray
// configs build), and every type that has a share-link format survives
// ToLink → ParseLink unchanged.
func TestManualProfilesFromApp(t *testing.T) {
	b, err := os.ReadFile("testdata/manual_profiles.json")
	if err != nil {
		t.Skip("testdata/manual_profiles.json not generated (run the Flutter v104 test first)")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) < 10 {
		t.Fatalf("only %d manual profiles", len(m))
	}
	// Generic SOCKS5 is offered by "Add manually" (plain → sing-box,
	// TLS → Xray).
	for name, core := range map[string]Core{"socks": CoreSingbox, "socks-tls": CoreXray} {
		raw, ok := m[name]
		if !ok {
			t.Errorf("manual profile %q missing (regenerate with the Flutter v104 test)", name)
			continue
		}
		p, err := decodeProfile(string(raw))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if p.Type != TypeSOCKS || CoreOf(p) != core {
			t.Errorf("%s: type %q core %s, want socks on %s", name, p.Type, CoreOf(p), core)
		}
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := m[name]
		t.Run(name, func(t *testing.T) {
			p, err := decodeProfile(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != name {
				t.Errorf("name %q", p.Name)
			}
			if removedTypes[p.Type] || p.Type == TypeAmneziaWG {
				t.Fatalf("type %q must not be creatable by hand", p.Type)
			}
			if p.Type == TypeMTProto {
				// Opened in Telegram: no core config, only the tg:// link.
				if l, err := ToLink(p); err != nil || l == "" {
					t.Fatalf("mtproto link: %v", err)
				}
				return
			}
			if msg := CheckProfile(string(raw), ""); msg != "" {
				t.Fatalf("CheckProfile: %s (core %s)", msg, CoreOf(p))
			}
			if p.Type == TypeMasque {
				return
			}
			l, err := ToLink(p)
			if err != nil {
				t.Fatalf("ToLink: %v", err)
			}
			p2, err := ParseLink(l)
			if err != nil {
				t.Fatalf("ParseLink(%s): %v", l, err)
			}
			if a, b := connView(p), connView(p2); !reflect.DeepEqual(a, b) {
				ja, _ := json.Marshal(a)
				jb, _ := json.Marshal(b)
				t.Errorf("round trip differs\n  in:  %s\n  out: %s\n  link: %s", ja, jb, l)
			}
		})
	}
}
