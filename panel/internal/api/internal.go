package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/db"
)

// actorKey carries the admin of an in-process call (Server.Do). Only code in
// this process can put it in a request context, so no HTTP client can.
type actorKey struct{}

func actorFrom(r *http.Request) *db.Admin {
	a, _ := r.Context().Value(actorKey{}).(*db.Admin)
	return a
}

// Do runs one API request in process, acting as actor, through the same
// handlers, permission checks and audit log as the web UI. path is relative
// to /api/v1 ("/users?page=1"); body is JSON-encoded unless it is nil, a
// []byte or a string. The audit log names the actor's Username (the
// Telegram bot passes "telegram:<id>"). It returns the status and the body.
func (s *Server) Do(ctx context.Context, actor *db.Admin, method, path string, body any) (int, []byte) {
	var rd io.Reader
	switch v := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(v)
	case string:
		rd = strings.NewReader(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return http.StatusBadRequest, []byte(`{"error":{"code":"bad_request","message":"cannot encode body"}}`)
		}
		rd = bytes.NewReader(b)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// The actor's own account (name, password) is never changed in process.
	if strings.HasPrefix(path, "/auth/") {
		return http.StatusForbidden, []byte(`{"error":{"code":"forbidden","message":"not available in process"}}`)
	}
	req := httptest.NewRequest(method, path, rd).WithContext(context.WithValue(ctx, actorKey{}, actor))
	req.RemoteAddr = "telegram"
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	req.Body = http.MaxBytesReader(rec, req.Body, 2<<20)
	s.api.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}
