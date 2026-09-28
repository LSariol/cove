// Package server is Cove's HTTP API.
package server

import (
	"fmt"
	"log"
	"net/http"

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

	//multiplexer (router)
	mux := http.NewServeMux()
	s.defineRoutes(mux)

	address := "0.0.0.0:" + s.port

	fmt.Printf("Running on %s\n", address)

	log.Fatal(http.ListenAndServe(address, mux))
}
