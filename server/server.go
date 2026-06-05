package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/signadot/cachelet-lab/cache"
)

// maxBodyBytes caps the size of a single cache entry written via PUT.
const maxBodyBytes = 1 << 20 // 1 MiB

// Server exposes a cache.Store over HTTP.
type Server struct {
	store   *cache.Store
	logger  *slog.Logger
	mux     *http.ServeMux
	metrics *metrics

	// ready gates the readiness probe. It starts true (the cache is usable as
	// soon as the process is up) and is flipped to false at the start of
	// shutdown so Kubernetes drains traffic before the process stops.
	ready atomic.Bool
}

// New returns a Server backed by store with its routes registered. If logger is
// nil, slog.Default is used.
func New(store *cache.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: store, logger: logger, mux: http.NewServeMux(), metrics: newMetrics(store)}
	s.ready.Store(true)
	s.routes()
	return s
}

func (s *Server) routes() {
	s.handle("GET /cache/{key}", s.handleGet)
	s.handle("PUT /cache/{key}", s.handleSet)
	s.handle("DELETE /cache/{key}", s.handleDelete)
	s.handle("GET /stats", s.handleStats)

	// Operational endpoints. They bypass the adapt/instrument chain on purpose:
	// probes and scrapes are high-frequency and would otherwise dominate the
	// HTTP request metrics, and /metrics must not recurse through itself.
	s.mux.Handle("GET /metrics", s.metrics.handler)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
}

// handle registers an apiHandler under a ServeMux pattern, wrapping it in the
// standard middleware/error-translation chain and request metrics. The pattern
// doubles as the low-cardinality route label for metrics.
func (s *Server) handle(pattern string, h apiHandler) {
	s.mux.Handle(pattern, s.adapt(pattern, h))
}

// SetReady flips the readiness state reported by /readyz. main calls
// SetReady(false) when a shutdown signal arrives so traffic drains before the
// listener closes. Liveness (/healthz) is unaffected.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

// handleHealthz is the liveness probe. The process being able to serve this is
// the whole signal: cachelet has no external dependencies, so anything that
// would make it unhealthy (a wedged process, a panic loop) also makes it unable
// to answer here, and a failing liveness probe should restart the pod.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok\n")
}

// handleReadyz is the readiness probe. It reports not-ready once shutdown has
// begun so Kubernetes removes the pod from Service endpoints before the process
// stops accepting connections.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "shutting down\n")
		return
	}
	io.WriteString(w, "ok\n")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleGet returns the value for the requested key as text/plain.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	v, ok := s.store.Get(key)
	if !ok {
		s.metrics.misses.Inc()
		return fmt.Errorf("key %q: %w", key, errNotFound)
	}
	s.metrics.hits.Inc()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, err := io.WriteString(w, v)
	return err
}

// handleSet stores the request body under the requested key. An optional ttl
// query parameter (in whole seconds) sets an expiry.
func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("reading body: %v: %w", err, errBadRequest)
	}

	var ttl time.Duration
	if raw := r.URL.Query().Get("ttl"); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 0 {
			return fmt.Errorf("invalid ttl %q: %w", raw, errBadRequest)
		}
		ttl = time.Duration(secs) * time.Second
	}

	s.store.Set(key, string(body), ttl)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleDelete removes the requested key. Deleting an absent key still
// succeeds.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) error {
	s.store.Delete(r.PathValue("key"))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleStats reports basic cache statistics as JSON.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(struct {
		Entries int `json:"entries"`
	}{Entries: s.store.Len()})
}
