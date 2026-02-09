package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log/slog"
)

// Server wraps the HTTP server and its lifecycle.
type Server struct {
	container *Container
	srv       *http.Server
}

// NewServer creates a new HTTP server with routes.
func NewServer(container *Container) *Server {
	mux := http.NewServeMux()

	// Routes
	mux.Handle("POST /webhook", container.WebhookHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", container.Config.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return &Server{
		container: container,
		srv:       srv,
	}
}

// Run starts the server and handles graceful shutdown.
func (s *Server) Run() error {
	log := s.container.Logger

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		log.Info("starting server", slog.Int("port", s.container.Config.Port))
		errCh <- s.srv.ListenAndServe()
	}()

	// Wait for shutdown signal or error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("shutting down", "signal", sig.String())
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	log.Info("server stopped")
	return nil
}
