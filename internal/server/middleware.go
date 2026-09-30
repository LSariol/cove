package server

import (
	"context"
	"crypto/subtle"
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

// caller is who made a request: the master token (project is nil), or a
// project token.
type caller struct {
	project *tokens.Token
}

type callerKey struct{}

// callerFrom returns the caller that requireToken stored in ctx.
func callerFrom(ctx context.Context) caller {
	c, _ := ctx.Value(callerKey{}).(caller)
	return c
}

// requireToken is middleware that accepts the master token
// (COVE_CLIENT_SECRET) or a project token in the Authorization header, and
// records which one was used for the handlers. Failed attempts count toward
// the address's rate limit (see ratelimit.go).
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
		provided := tokenParts[1]

		if s.clientSecret != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(s.clientSecret)) == 1 {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, caller{})))
			return
		}

		if s.tokens != nil {
			tok, err := s.tokens.Authenticate(r.Context(), provided)
			switch {
			case err == nil:
				setTokenName(r.Context(), tok.Name)
				ctx := context.WithValue(r.Context(), callerKey{}, caller{project: &tok})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			case !errors.Is(err, tokens.ErrNotFound):
				log.Printf("check token: %v", err)
				writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "the token couldn't be checked right now; try again")
				return
			}
		}

		s.limiter.fail(addr, "invalid token")
		writeError(w, http.StatusUnauthorized, "invalid_token", "the provided token is invalid")
	})
}
