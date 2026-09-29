// Package server is Cove's HTTP API.
package server

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/vault"
)

type Server struct {
	vault        *vault.Vault
	bootstrap    *bootstrap.Marker
	clientSecret string
	port         string
}

// New returns a Server. clientSecret is the bearer token clients must send, and
// is also what the bootstrap endpoint hands out.
func New(v *vault.Vault, marker *bootstrap.Marker, clientSecret string, port string) *Server {
	return &Server{
		vault:        v,
		bootstrap:    marker,
		clientSecret: clientSecret,
		port:         port,
	}
}

func (s *Server) Start() {
	srv := s.httpServer()

	fmt.Printf("Running on %s\n", srv.Addr)

	log.Fatal(srv.ListenAndServe())
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
