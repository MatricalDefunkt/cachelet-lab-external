package server

import (
	"errors"
	"net/http"
)

// apiHandler is an HTTP handler that returns an error. It is adapted to
// http.Handler by Server.adapt, which translates the returned error into a
// response. This keeps individual handlers free of repetitive status-writing
// boilerplate.
//
// Handlers should return one of the sentinel errors below (optionally wrapped
// with %w) to produce a client-facing status. Any other error is treated as an
// internal server error and logged.
type apiHandler func(http.ResponseWriter, *http.Request) error

// middleware wraps an apiHandler with cross-cutting behavior.
type middleware func(apiHandler) apiHandler

var (
	errNotFound   = errors.New("not found")
	errBadRequest = errors.New("bad request")
)

// statusFor maps a handler error to an HTTP status code.
func statusFor(err error) int {
	switch {
	case errors.Is(err, errNotFound):
		return http.StatusNotFound
	case errors.Is(err, errBadRequest):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// adapt wires a business handler into an http.Handler: it applies the standard
// middleware chain, translates a returned error into an HTTP response, and wraps
// the result in request metrics. Cross-cutting behavior (e.g. logging) lives in
// the middleware listed here, not inline in adapt.
//
// pattern is the ServeMux route (e.g. "GET /cache/{key}") and is used as the
// metrics route label.
func (s *Server) adapt(pattern string, h apiHandler) http.Handler {
	// Standard middleware chain, applied to every route. Listed outermost
	// first: the first entry runs before those below it.
	chain := []middleware{
		s.logRequests,
	}
	for i := len(chain) - 1; i >= 0; i-- {
		h = chain[i](h)
	}

	// Error translation runs inside the metrics wrapper so the recorded status
	// code reflects the response actually sent to the client.
	translate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			http.Error(w, err.Error(), statusFor(err))
		}
	})
	return s.metrics.instrument(pattern, translate)
}
