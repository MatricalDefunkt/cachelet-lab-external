package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/signadot/cachelet-lab/cache"
)

// maxBodyBytes caps the size of a single cache entry written via PUT.
const maxBodyBytes = 1 << 20 // 1 MiB

// Server exposes a cache.Store over HTTP.
type Server struct {
	store  *cache.Store
	logger *slog.Logger
	mux    *http.ServeMux
}

// New returns a Server backed by store with its routes registered. If logger is
// nil, slog.Default is used.
func New(store *cache.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: store, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.Handle("GET /cache/{key}", s.adapt(s.handleGet))
	s.mux.Handle("PUT /cache/{key}", s.adapt(s.handleSet))
	s.mux.Handle("DELETE /cache/{key}", s.adapt(s.handleDelete))
	s.mux.Handle("GET /stats", s.adapt(s.handleStats))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleGet returns the value for the requested key as text/plain.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	v, ok := s.store.Get(key)
	if !ok {
		return fmt.Errorf("key %q: %w", key, errNotFound)
	}
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
