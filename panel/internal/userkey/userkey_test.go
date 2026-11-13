package userkey

import "testing"

func TestID(t *testing.T) {
	t.Cleanup(func() { Set(nil) })
	Set(map[string]int64{"bob_px": 8, "": 3, "zero": 0})
	cases := []struct {
		in string
		id int64
		ok bool
	}{
		{"7.alice", 7, true}, {"12.a.b", 12, true}, {"bob_px", 8, true},
		{"BOB_PX", 0, false}, {"", 0, false}, {"zero", 0, false}, {"x.y", 0, false}, {"0.root", 0, false},
	}
	for _, c := range cases {
		id, ok := ID(c.in)
		if id != c.id || ok != c.ok {
			t.Errorf("ID(%q) = %d %v, want %d %v", c.in, id, ok, c.id, c.ok)
		}
	}
	if len(Snapshot()) != 1 {
		t.Fatalf("snapshot %v", Snapshot())
	}
	Set(nil)
	if _, ok := ID("bob_px"); ok {
		t.Fatal("cleared table still maps")
	}
}

func TestLooksAuto(t *testing.T) {
	for in, want := range map[string]bool{"7.alice": true, "12.": true, "a7.x": false, "bob": false, ".x": false, "77": false} {
		if LooksAuto(in) != want {
			t.Errorf("LooksAuto(%q) != %v", in, want)
		}
	}
}
