package server

import (
	"log"
	"net/http"
	"time"
)

// logRequests logs one line per request:
//
//	GET /v0/secrets/MYAPP_DB_URL 200 1.2ms source=myapp from=172.18.0.5
//
// It never logs request or response bodies, or headers such as Authorization,
// so values and tokens stay out of the log. Successful health and readiness
// checks are skipped: Docker's healthcheck calls them every 10 seconds.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		if rec.status == http.StatusOK && (r.URL.Path == "/v0/health" || r.URL.Path == "/v0/ready") {
			return
		}

		source := r.Header.Get("X-Cove-Source")
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
