package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHealthzAlwaysOK(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/healthz", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d; want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestReadyzReadyByDefault(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/readyz", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /readyz status = %d; want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestReadyzFailsAfterSetNotReady(t *testing.T) {
	srv := newTestServer(t)
	srv.SetReady(false)

	resp := do(t, srv, http.MethodGet, "/readyz", "")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz after SetReady(false) status = %d; want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	// Liveness must stay healthy during drain so the pod is not restarted.
	if live := do(t, srv, http.MethodGet, "/healthz", ""); live.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz during drain status = %d; want %d", live.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "shutting down") {
		t.Errorf("readyz body = %q; want mention of shutting down", body)
	}
}
