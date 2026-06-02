package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/signadot/cachelet-lab/cache"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store := cache.New(0)
	t.Cleanup(store.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(store, logger)
}

func do(t *testing.T, srv *Server, method, target string, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Result()
}

func TestSetThenGet(t *testing.T) {
	srv := newTestServer(t)

	if resp := do(t, srv, http.MethodPut, "/cache/foo", "bar"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d; want %d", resp.StatusCode, http.StatusNoContent)
	}

	resp := do(t, srv, http.MethodGet, "/cache/foo", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d; want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "bar" {
		t.Fatalf("GET body = %q; want %q", body, "bar")
	}
}

func TestGetMissing(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/cache/missing", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing status = %d; want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestInvalidTTL(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPut, "/cache/foo?ttl=abc", "bar")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid ttl status = %d; want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestNegativeTTL(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPut, "/cache/foo?ttl=-1", "bar")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT negative ttl status = %d; want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBodyTooLarge(t *testing.T) {
	srv := newTestServer(t)
	big := strings.Repeat("x", maxBodyBytes+1)
	resp := do(t, srv, http.MethodPut, "/cache/foo", big)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT oversize body status = %d; want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestDeleteThenGet(t *testing.T) {
	srv := newTestServer(t)

	do(t, srv, http.MethodPut, "/cache/foo", "bar")
	if resp := do(t, srv, http.MethodDelete, "/cache/foo", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d; want %d", resp.StatusCode, http.StatusNoContent)
	}
	if resp := do(t, srv, http.MethodGet, "/cache/foo", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete = %d; want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestStats(t *testing.T) {
	srv := newTestServer(t)
	do(t, srv, http.MethodPut, "/cache/a", "1")
	do(t, srv, http.MethodPut, "/cache/b", "2")

	resp := do(t, srv, http.MethodGet, "/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats status = %d; want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := strings.TrimSpace(string(body)); got != `{"entries":2}` {
		t.Fatalf("GET /stats body = %q; want %q", got, `{"entries":2}`)
	}
}
