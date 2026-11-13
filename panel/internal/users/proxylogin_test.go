package users

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestValidateProxyLogin(t *testing.T) {
	ok := [][2]string{{"", ""}, {"ali_px", ""}, {"", "Secret-123"}, {"a.b-c_d", "abcdefgh"}, {"x1.y", "P@ss#word+1"}}
	for _, c := range ok {
		if err := ValidateProxyLogin(c[0], c[1]); err != nil {
			t.Errorf("%q/%q refused: %v", c[0], c[1], err)
		}
	}
	bad := [][2]string{{"ab", ""}, {"has space", ""}, {"ali@px", ""}, {"7.alice", ""}, {"12.x", ""},
		{"abcdefghijklmnopqrstuvwxyz0123456", ""}, {"", "short"}, {"", "has space 123"}, {"", "pass:word1"}, {"", "pässwörd12"}}
	for _, c := range bad {
		if err := ValidateProxyLogin(c[0], c[1]); err == nil {
			t.Errorf("%q/%q accepted", c[0], c[1])
		}
	}
}

func TestProxyLoginFallback(t *testing.T) {
	u := &db.User{ID: 7, Username: "alice", Password: "gen"}
	if a, b := ProxyLogin(u); a != "7.alice" || b != "gen" {
		t.Fatal(a, b)
	}
	u.ProxyUsername = "ali_px"
	if a, b := ProxyLogin(u); a != "ali_px" || b != "gen" {
		t.Fatal(a, b)
	}
	u.ProxyPassword = "Secret-123"
	if a, b := ProxyLogin(u); a != "ali_px" || b != "Secret-123" {
		t.Fatal(a, b)
	}
}

// Create/update store the custom login, keep it unique (case-insensitive)
// and clear it with "".
func TestServiceProxyLogin(t *testing.T) {
	gdb := testutil.DB(t)
	s := &Service{DB: gdb, SubURL: func(tok, host string) string { return tok }}
	mk := func(js string) (*View, error) {
		var in Input
		if err := json.Unmarshal([]byte(js), &in); err != nil {
			t.Fatal(err)
		}
		return s.Create(&in, "")
	}
	a, err := mk(`{"username":"alice","proxy_username":" ali_px ","proxy_password":"Secret-123"}`)
	if err != nil || a.ProxyUsername != "ali_px" || a.ProxyPassword != "Secret-123" {
		t.Fatalf("create: %v %+v", err, a)
	}
	b, err := mk(`{"username":"bob"}`)
	if err != nil || b.ProxyUsername != "" {
		t.Fatalf("auto user: %v %+v", err, b)
	}
	if _, err := mk(`{"username":"carol"}`); err != nil {
		t.Fatal("two automatic logins must coexist:", err)
	}
	var pt *ProxyTakenError
	if _, err := mk(`{"username":"dave","proxy_username":"ALI_PX"}`); !errors.As(err, &pt) {
		t.Fatalf("duplicate (case) accepted: %v", err)
	}
	patch := func(id int64, js string) (*View, error) {
		var in Input
		_ = json.Unmarshal([]byte(js), &in)
		return s.Update(id, &in, "")
	}
	if _, err := patch(b.ID, `{"proxy_username":"ali_px"}`); !errors.As(err, &pt) {
		t.Fatalf("duplicate on update accepted: %v", err)
	}
	if _, err := patch(b.ID, `{"proxy_username":"7.bob"}`); err == nil {
		t.Fatal("automatic-looking name accepted")
	}
	if _, err := patch(b.ID, `{"proxy_password":"short"}`); err == nil {
		t.Fatal("short password accepted")
	}
	// Saving alice with its own name again is fine; other edits keep it.
	if v, err := patch(a.ID, `{"proxy_username":"ali_px","note":"x"}`); err != nil || v.ProxyUsername != "ali_px" {
		t.Fatalf("self update: %v", err)
	}
	if v, err := patch(a.ID, `{"note":"y"}`); err != nil || v.ProxyUsername != "ali_px" || v.ProxyPassword != "Secret-123" {
		t.Fatalf("untouched fields lost: %v %+v", err, v)
	}
	v, err := patch(a.ID, `{"proxy_username":"","proxy_password":""}`)
	if err != nil || v.ProxyUsername != "" || v.ProxyPassword != "" {
		t.Fatalf("clear: %v %+v", err, v)
	}
	if _, err := patch(b.ID, `{"proxy_username":"ali_px"}`); err != nil {
		t.Fatal("freed name not reusable:", err)
	}
}
