package tgbot

import (
	"reflect"
	"testing"
)

func TestXrayChoices(t *testing.T) {
	rels := []xrayRelease{{"v26.10.31"}, {"v26.9.6"}, {"v26.3.27"}, {"v25.12.8"}, {"v25.10.15"}, {"v25.9.11"}}
	if got := xrayChoices(rels, "v25.6.8"); !reflect.DeepEqual(got, []string{"v26.10.31", "v26.9.6", "v26.3.27", "v25.12.8", "v25.10.15", "v25.6.8"}) {
		t.Fatal(got)
	}
	if got := xrayChoices(rels[:3], "v26.9.6"); !reflect.DeepEqual(got, []string{"v26.10.31", "v26.9.6", "v26.3.27"}) {
		t.Fatal(got)
	}
	if got := xrayChoices(rels[1:3], "v27.1.1"); !reflect.DeepEqual(got, []string{"v27.1.1", "v26.9.6", "v26.3.27"}) {
		t.Fatal(got)
	}
	if cmpVer("v26.10.31", "v26.9.6") <= 0 || cmpVer("v25.6.8", "25.6.8") != 0 {
		t.Fatal("cmpVer")
	}
}
