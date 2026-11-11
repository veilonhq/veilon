// Package webui serves the embedded single-page web UI.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// FS returns the UI file system rooted at dist/.
func FS() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}

// Handler serves the UI below basePath (e.g. "/abc123/"). Requests for files
// that do not exist fall back to index.html (SPA routing). index.html gets a
// <base href> and a window.__VEILON__ runtime config injected so the UI can
// work under the random panel path. lang is the panel's default UI language
// ("fa" or "en"); the UI uses it until the browser has its own choice.
func Handler(basePath, lang string) http.Handler {
	root := FS()
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(basePath, "/"))
		rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
		if rel != "" && rel != "index.html" {
			if st, err := fs.Stat(root, rel); err == nil && !st.IsDir() {
				if strings.HasPrefix(rel, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/" + rel
				files.ServeHTTP(w, r2)
				return
			}
			// Missing asset-like paths should 404 rather than return HTML.
			if strings.HasPrefix(rel, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		serveIndex(w, root, basePath, lang)
	})
}

func serveIndex(w http.ResponseWriter, root fs.FS, basePath, lang string) {
	b, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	if lang != "en" {
		lang = "fa"
	}
	cfg, _ := json.Marshal(map[string]string{
		"lang":     lang,
		"basePath": basePath,
		"apiBase":  strings.TrimSuffix(basePath, "/") + "/api/v1",
	})
	inject := []byte(`<base href="` + basePath + `"><script>window.__VEILON__=` + string(cfg) + `;</script>`)
	if i := bytes.Index(b, []byte("<head>")); i >= 0 {
		i += len("<head>")
		b = append(b[:i:i], append(inject, b[i:]...)...)
	} else {
		b = append(inject, b...)
	}
	if lang == "en" {
		b = bytes.Replace(b, []byte(`<html lang="fa" dir="rtl"`), []byte(`<html lang="en" dir="ltr"`), 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

// SubPageFile is the subscription page entry of the web build.
const SubPageFile = "sub.html"

// SubPage renders the subscription page (sub.html of root, or of the
// embedded build when root is nil) for a user's browser. basePath is the
// subscription path ("/sub/"); the page's assets load from below it (see
// ServeSubAsset). data is inlined as window.__VEILON_SUB__ so the page
// paints its final layout on the first frame. ok is false when the build
// has no subscription page (a placeholder UI).
func SubPage(root fs.FS, basePath, lang string, data any) (page []byte, ok bool) {
	if root == nil {
		root = FS()
	}
	b, err := fs.ReadFile(root, SubPageFile)
	if err != nil {
		return nil, false
	}
	if lang != "en" {
		lang = "fa"
	}
	// json.Marshal escapes <, > and &, so the payload cannot end the script.
	cfg, err := json.Marshal(map[string]any{"lang": lang, "data": data})
	if err != nil {
		return nil, false
	}
	inject := []byte(`<base href="` + basePath + `"><script>window.__VEILON_SUB__=` + string(cfg) + `;</script>`)
	if i := bytes.Index(b, []byte("<head>")); i >= 0 {
		i += len("<head>")
		b = append(b[:i:i], append(inject, b[i:]...)...)
	} else {
		b = append(inject, b...)
	}
	if lang == "en" {
		b = bytes.Replace(b, []byte(`<html lang="fa" dir="rtl"`), []byte(`<html lang="en" dir="ltr"`), 1)
	}
	return b, true
}

// ServeSubAsset serves a static file of the web build (rel is "assets/…"
// or "favicon.svg") below the subscription path, so the subscription page
// never reveals the panel path. Only the assets directory and the favicon
// are reachable.
func ServeSubAsset(w http.ResponseWriter, r *http.Request, root fs.FS, rel string) {
	if root == nil {
		root = FS()
	}
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if !strings.HasPrefix(rel, "assets/") && rel != "favicon.svg" {
		http.NotFound(w, r)
		return
	}
	st, err := fs.Stat(root, rel)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(rel, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + rel
	http.FileServer(http.FS(root)).ServeHTTP(w, r2)
}
