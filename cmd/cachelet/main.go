package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/signadot/cachelet-lab/cache"
	"github.com/signadot/cachelet-lab/server"
)

// drainDelay is how long to keep serving after failing readiness, giving
// Kubernetes time to remove this pod from Service endpoints before the listener
// closes. It is intentionally short; in-flight requests still get the full
// shutdown timeout below.
const drainDelay = 5 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := os.Getenv("CACHELET_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	store := cache.New(1 * time.Minute)
	defer store.Close()

	app := server.New(store, logger)
	srv := &http.Server{
		Addr:         addr,
		Handler:      app,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("cachelet listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("shutting down")

	// Fail readiness first so Kubernetes pulls this pod out of Service
	// endpoints, then give in-flight requests a window to finish. The brief
	// drain pause covers the lag between failing /readyz and the kube-proxy /
	// load-balancer actually removing the endpoint.
	app.SetReady(false)
	time.Sleep(drainDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
}
