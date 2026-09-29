// Package server is Cove's HTTP API.
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/vault"
)

// Pinger checks that the database is reachable. *database.Database implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options are the Server's settings.
type Options struct {
	ClientSecret string // bearer token clients must send; also what the bootstrap endpoint hands out
	Port         string
	Version      string // reported by /v0/version
}

type Server struct {
	vault        *vault.Vault
	bootstrap    *bootstrap.Gate
	db           Pinger
	clientSecret string
	port         string
	version      string
}

// New returns a Server. db is used by the readiness check.
func New(v *vault.Vault, gate *bootstrap.Gate, db Pinger, opts Options) *Server {
	return &Server{
		vault:        v,
		bootstrap:    gate,
		db:           db,
		clientSecret: opts.ClientSecret,
		port:         opts.Port,
		version:      opts.Version,
	}
}

// Run serves the API until ctx is cancelled, then shuts down gracefully:
// requests in progress get up to shutdownTimeout to finish. It returns an
// error if the server can't start, e.g. because the port is in use.
func (s *Server) Run(ctx context.Context) error {
	srv := s.httpServer()

	errs := make(chan error, 1)
	go func() {
		log.Printf("API listening on %s", srv.Addr)
		errs <- srv.ListenAndServe()
	}()

	select {
	case err := <-errs:
		return fmt.Errorf("API server stopped: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down the API server: %w", err)
	}
	return nil
}

// shutdownTimeout is how long requests in progress get to finish when Cove stops.
const shutdownTimeout = 5 * time.Second

// httpServer builds the http.Server with timeouts, so a slow or stalled client
// can't hold a connection open forever. Real requests take milliseconds.
func (s *Server) httpServer() *http.Server {

	//multiplexer (router)
	mux := http.NewServeMux()
	s.defineRoutes(mux)

	return &http.Server{
		Addr:              "0.0.0.0:" + s.port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
