package server

import (
	"log/slog"
	"net/http"
)

// logRequests is the single place request observability lives. It wraps a
// handler at the apiHandler layer (so it has the typed error in hand) and logs
// any request that failed: 5xx at error level, client errors at warn level.
func (s *Server) logRequests(next apiHandler) apiHandler {
	return func(w http.ResponseWriter, r *http.Request) error {
		err := next(w, r)
		if err != nil {
			level := slog.LevelWarn
			if statusFor(err) >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			s.logger.LogAttrs(r.Context(), level, "request failed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Any("err", err),
			)
		}
		return err
	}
}
