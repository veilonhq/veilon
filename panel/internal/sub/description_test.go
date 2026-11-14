package sub

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// The description for the user is shown at the bottom of the subscription
// page as data (never markup): line breaks kept, nothing that could close
// the script or inject HTML. The admin's private note never reaches the
// page, the status JSON or any subscription response.
func TestPageDescriptionNotNote(t *testing.T) {
	const desc = "کانال ما\n</script><img src=x onerror=alert(1)> & <b>bold</b> https://t.me/x"
	const note = "PRIVATE-ADMIN-NOTE پرداخت نشده"
	s := negFixture(t, db.StatusActive)
	set := func(d, n string) {
		t.Helper()
		if err := s.DB.Model(&db.User{}).Where("sub_token = ?", "tokCarol").
			Updates(map[string]any{"description": d, "note": n}).Error; err != nil {
			t.Fatal(err)
		}
	}
	noNote := func(where, body string) {
		t.Helper()
		if strings.Contains(body, "PRIVATE-ADMIN-NOTE") || strings.Contains(body, base64.StdEncoding.EncodeToString([]byte(note))) {
			t.Fatalf("%s: private note leaked", where)
		}
	}

	set(desc, note)
	w := negRequest(s, negCase{"b", chromeUA, htmlAccept, ""})
	body, cfg := pageOf(t, w)
	d := cfg["data"].(map[string]any)
	if d["description"] != desc {
		t.Fatalf("description on page = %q", d["description"])
	}
	if _, ok := d["note"]; ok {
		t.Fatal("page data has a note field")
	}
	noNote("page", body)
	for _, bad := range []string{"</script><img", "<img src=x", "<b>bold</b>"} {
		if strings.Contains(body, bad) {
			t.Fatalf("description not escaped: found %q", bad)
		}
	}
	if !strings.Contains(body, `\n\u003c/script\u003e\u003cimg`) {
		t.Fatalf("escaped description with its line break missing: %.400s", body)
	}
	st := negRequest(s, negCase{"s", chromeUA, "", "?format=status"}).Body.String()
	if !strings.Contains(st, `"description":"کانال ما\n`) {
		t.Fatalf("status json lacks description: %s", st)
	}
	noNote("status", st)

	// VPN clients: announce header with the description, never the note.
	want := "base64:" + base64.StdEncoding.EncodeToString([]byte(desc))
	for _, c := range rawCases {
		r := negRequest(s, c)
		noNote(c.name, dumpResponse(c.name, r))
		if r.Header().Get("subscription-userinfo") != "" && r.Header().Get("announce") != want {
			t.Fatalf("%s: announce = %q", c.name, r.Header().Get("announce"))
		}
	}

	// Empty (or blank) description: no card, no announce header.
	for _, empty := range []string{"", "  \n "} {
		set(empty, note)
		body, cfg = pageOf(t, negRequest(s, negCase{"b", chromeUA, htmlAccept, ""}))
		if _, ok := cfg["data"].(map[string]any)["description"]; ok {
			t.Fatalf("blank description %q shown", empty)
		}
		noNote("page", body)
		if a := negRequest(s, rawCases[0]).Header().Get("announce"); a != "" {
			t.Fatalf("announce without description: %q", a)
		}
	}
}
