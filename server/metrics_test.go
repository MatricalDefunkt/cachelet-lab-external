package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// metricsText scrapes /metrics and returns the exposition text.
func metricsText(t *testing.T, srv *Server) string {
	t.Helper()
	resp := do(t, srv, http.MethodGet, "/metrics", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d; want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestMetricsExposed(t *testing.T) {
	srv := newTestServer(t)

	// Drive one of each request so the series exist.
	do(t, srv, http.MethodPut, "/cache/foo", "bar")
	do(t, srv, http.MethodGet, "/cache/foo", "")     // hit
	do(t, srv, http.MethodGet, "/cache/missing", "") // miss

	body := metricsText(t, srv)

	for _, want := range []string{
		"cachelet_http_requests_total",
		"cachelet_http_request_duration_seconds",
		"cachelet_cache_hits_total",
		"cachelet_cache_misses_total",
		"cachelet_cache_entries",
		"go_goroutines", // default Go collector is registered
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestRequestMetricsLabelByRouteNotKey(t *testing.T) {
	srv := newTestServer(t)

	// Two distinct keys must collapse onto the same route series, otherwise
	// the cache key would blow up label cardinality.
	do(t, srv, http.MethodGet, "/cache/alpha", "")
	do(t, srv, http.MethodGet, "/cache/beta", "")

	body := metricsText(t, srv)
	if strings.Contains(body, `route="/cache/alpha"`) || strings.Contains(body, `route="/cache/beta"`) {
		t.Fatalf("metrics leaked the cache key into the route label:\n%s", body)
	}
	if !strings.Contains(body, `route="/cache/{key}"`) {
		t.Fatalf("expected route label %q in metrics:\n%s", "/cache/{key}", body)
	}
}

func TestRequestMetricsRecordStatus(t *testing.T) {
	srv := newTestServer(t)
	do(t, srv, http.MethodGet, "/cache/missing", "") // 404

	body := metricsText(t, srv)
	want := `cachelet_http_requests_total{method="GET",route="/cache/{key}",status="404"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in metrics:\n%s", want, body)
	}
}

func TestCacheHitMissCounters(t *testing.T) {
	srv := newTestServer(t)
	do(t, srv, http.MethodPut, "/cache/foo", "bar")
	do(t, srv, http.MethodGet, "/cache/foo", "")   // hit
	do(t, srv, http.MethodGet, "/cache/nope", "")  // miss
	do(t, srv, http.MethodGet, "/cache/nope2", "") // miss

	body := metricsText(t, srv)
	if !strings.Contains(body, "cachelet_cache_hits_total 1") {
		t.Errorf("expected 1 hit:\n%s", body)
	}
	if !strings.Contains(body, "cachelet_cache_misses_total 2") {
		t.Errorf("expected 2 misses:\n%s", body)
	}
}

func TestOperationalEndpointsNotInRequestMetrics(t *testing.T) {
	srv := newTestServer(t)
	do(t, srv, http.MethodGet, "/healthz", "")
	do(t, srv, http.MethodGet, "/readyz", "")

	body := metricsText(t, srv)
	if strings.Contains(body, `route="/healthz"`) || strings.Contains(body, `route="/readyz"`) {
		t.Fatalf("probe endpoints should not appear in HTTP request metrics:\n%s", body)
	}
}
