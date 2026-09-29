// Package server is Cove's HTTP API.
package server

import (
	"context"
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
	bootstrap    *bootstrap.Marker
	db           Pinger
	clientSecret string
	port         string
	version      string
}

// New returns a Server. db is used by the readiness check.
func New(v *vault.Vault, marker *bootstrap.Marker, db Pinger, opts Options) *Server {
	return &Server{
		vault:        v,
		bootstrap:    marker,
		db:           db,
		clientSecret: opts.ClientSecret,
		port:         opts.Port,
		version:      opts.Version,
	}
}

func (s *Server) Start() {
	srv := s.httpServer()

	log.Printf("API listening on %s", srv.Addr)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("API server stopped: %v", err)
	}
}

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
