package server

import (
	"context"
	"log"
	"net/http"
	"time"
)

// requestInfo is filled in while a request is handled, for its log line.
type requestInfo struct {
	tokenName string // set when a project token was used
}

type requestInfoKey struct{}

// setTokenName records which project token a request used, for the log line.
func setTokenName(ctx context.Context, name string) {
	if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
		info.tokenName = name
	}
}

// logRequests logs one line per request:
//
//	GET /v0/secrets/MYAPP_DB_URL 200 1.2ms source=myapp from=172.18.0.5
//
// source is the project token's name when one was used (it can't be faked),
// otherwise the X-Cove-Source header.
//
// It never logs request or response bodies, or headers such as Authorization,
// so values and tokens stay out of the log. Successful health and readiness
// checks are skipped: Docker's healthcheck calls them every 10 seconds.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		info := &requestInfo{}

		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))

		if rec.status == http.StatusOK && (r.URL.Path == "/v0/health" || r.URL.Path == "/v0/ready") {
			return
		}

		source := r.Header.Get("X-Cove-Source")
		if info.tokenName != "" {
			source = info.tokenName
		}
		if source == "" {
			source = "-"
		}
		log.Printf("%s %s %d %s source=%s from=%s",
			r.Method, r.URL.Path, rec.status, time.Since(start).Round(100*time.Microsecond), source, remoteAddr(r))
	})
}

// statusRecorder remembers the status code a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
