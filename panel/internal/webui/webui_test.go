package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAFallbackAndInjection(t *testing.T) {
	h := Handler("/p4th/", "")
	for _, path := range []string{"/p4th/", "/p4th/users/12"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, `<base href="/p4th/">`) || !strings.Contains(body, `"apiBase":"/p4th/api/v1"`) || !strings.Contains(body, `"lang":"fa"`) {
			t.Fatalf("%s: %d %s", path, rec.Code, body[:120])
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/p4th/assets/missing.js", nil))
	if rec.Code != 404 {
		t.Fatalf("missing asset returned %d", rec.Code)
	}
}

func TestDefaultLanguageInjected(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler("/p/", "en").ServeHTTP(rec, httptest.NewRequest("GET", "/p/", nil))
	if !strings.Contains(rec.Body.String(), `"lang":"en"`) {
		t.Fatalf("lang not injected: %.200s", rec.Body.String())
	}
}

func TestSubPageInjection(t *testing.T) {
	root := fstest.MapFS{"sub.html": {Data: []byte(`<!doctype html><html lang="fa" dir="rtl"><head><title>x</title></head></html>`)}}
	b, ok := SubPage(root, "/sub/", "en", map[string]string{"username": "</script>"})
	s := string(b)
	if !ok || !strings.Contains(s, `<head><base href="/sub/"><script>window.__VEILON_SUB__={`) || !strings.Contains(s, `<html lang="en" dir="ltr"`) ||
		strings.Contains(s, `"</script>"`) || !strings.Contains(s, `"lang":"en"`) {
		t.Fatalf("%s", s)
	}
	if _, ok := SubPage(fstest.MapFS{"index.html": {Data: []byte("x")}}, "/sub/", "fa", nil); ok {
		t.Fatal("a build without sub.html has no page")
	}
}
