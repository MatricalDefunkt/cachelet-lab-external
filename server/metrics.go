package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/signadot/cachelet-lab/cache"
)

// metrics holds the Prometheus collectors for the service and the handler that
// exposes them at /metrics. Each Server owns its own metrics with a private
// registry, so constructing multiple Servers (as the tests do) never collides
// on the global default registry.
type metrics struct {
	registry *prometheus.Registry
	handler  http.Handler

	// requests counts completed HTTP requests, labelled by the matched route
	// pattern (not the raw path) to keep cardinality bounded. The cache key is
	// deliberately not a label: it is unbounded user input.
	requests *prometheus.CounterVec
	// duration is the request latency distribution per route.
	duration *prometheus.HistogramVec

	// hits and misses track cache GET outcomes. hits/(hits+misses) is the hit
	// ratio, the headline health signal for a cache.
	hits   prometheus.Counter
	misses prometheus.Counter
}

// newMetrics builds the collectors and registers them, including the standard
// Go runtime and process collectors. The entries gauge is sampled from the
// store at scrape time, so it needs no bookkeeping on the write path.
func newMetrics(store *cache.Store) *metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &metrics{
		registry: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cachelet_http_requests_total",
			Help: "Total HTTP requests handled, by method, route and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cachelet_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		hits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cachelet_cache_hits_total",
			Help: "Cache GETs that found a live entry.",
		}),
		misses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cachelet_cache_misses_total",
			Help: "Cache GETs that found no live entry.",
		}),
	}

	entries := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "cachelet_cache_entries",
		Help: "Entries currently held, including expired ones not yet evicted.",
	}, func() float64 { return float64(store.Len()) })

	reg.MustRegister(m.requests, m.duration, m.hits, m.misses, entries)
	m.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	return m
}

// instrument wraps an http.Handler to record request count and latency under
// the given route pattern (e.g. "GET /cache/{key}"). It sits at the outermost
// layer so it observes the final status code, including errors translated by
// adapt after the inner handler returns.
func (m *metrics) instrument(pattern string, next http.Handler) http.Handler {
	method, route := splitPattern(pattern)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		m.requests.WithLabelValues(method, route, strconv.Itoa(rec.status)).Inc()
		m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	})
}

// splitPattern splits a ServeMux pattern like "GET /cache/{key}" into its
// method and route. A pattern without a method yields an empty method label.
func splitPattern(pattern string) (method, route string) {
	if before, after, ok := strings.Cut(pattern, " "); ok {
		return before, after
	}
	return "", pattern
}

// statusRecorder captures the status code written to an http.ResponseWriter so
// the metrics middleware can label by it. A response with no explicit
// WriteHeader defaults to 200, matching net/http.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}
