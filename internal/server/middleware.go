package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/LSariol/Cove/internal/tokens"
)

// TokenAuthenticator checks project tokens. *tokens.Manager implements it.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, value string) (tokens.Token, error)
}

type callerKey struct{}

// callerFrom returns the token that requireToken stored in ctx: the project
// that made the request.
func callerFrom(ctx context.Context) tokens.Token {
	tok, _ := ctx.Value(callerKey{}).(tokens.Token)
	return tok
}

// requireToken is middleware that accepts a project token in the
// Authorization header and records which one was used for the handlers.
// Failed attempts count toward the address's rate limit (see ratelimit.go).
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.refuseIfBlocked(w, r) {
			return
		}
		addr := remoteAddr(r)

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.limiter.fail(addr, "missing token")
			writeError(w, http.StatusUnauthorized, "missing_token", "Authorization header is required")
			return
		}

		tokenParts := strings.SplitN(authHeader, " ", 2)
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			s.limiter.fail(addr, "malformed Authorization header")
			writeError(w, http.StatusUnauthorized, "invalid_token_format", "Authorization header must be in the form: Bearer <token>")
			return
		}

		tok, err := s.tokens.Authenticate(r.Context(), tokenParts[1])
		switch {
		case err == nil:
			setTokenName(r.Context(), tok.Name)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, tok)))
			return
		case !errors.Is(err, tokens.ErrNotFound):
			log.Printf("check token: %v", err)
			writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "the token couldn't be checked right now; try again")
			return
		}

		s.limiter.fail(addr, "invalid token")
		writeError(w, http.StatusUnauthorized, "invalid_token", "the provided token is invalid")
	})
}
