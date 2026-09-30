package server

import (
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Failed attempts (a wrong or missing token, a refused bootstrap request)
// are limited per address: after maxFailures within failureWindow, the
// address is refused for blockDuration. Successful requests are never
// limited, so a busy client with a valid token is unaffected.
const (
	maxFailures   = 10
	failureWindow = time.Minute
	blockDuration = 5 * time.Minute
)

// failureLimiter counts failed attempts per address. It lives in memory, so
// a restart clears it; that's fine for slowing down guessing.
type failureLimiter struct {
	mu    sync.Mutex
	addrs map[netip.Addr]*failures
	now   func() time.Time
}

type failures struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

func newFailureLimiter() *failureLimiter {
	return &failureLimiter{addrs: make(map[netip.Addr]*failures), now: time.Now}
}

// blocked reports whether addr is refused right now, and for how much longer.
func (l *failureLimiter) blocked(addr netip.Addr) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, ok := l.addrs[addr]
	if !ok {
		return false, 0
	}
	if left := f.blockedUntil.Sub(l.now()); left > 0 {
		return true, left
	}
	return false, 0
}

// fail records a failed attempt from addr, and blocks it once it reaches
// maxFailures within failureWindow.
func (l *failureLimiter) fail(addr netip.Addr, what string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.forgetOld(now)

	f, ok := l.addrs[addr]
	if !ok || now.Sub(f.windowStart) > failureWindow {
		f = &failures{windowStart: now}
		l.addrs[addr] = f
	}
	f.count++
	if f.count >= maxFailures && !now.Before(f.blockedUntil) {
		f.blockedUntil = now.Add(blockDuration)
		log.Printf("rate limit: refusing %s for %s after %d failed attempts in %s (last: %s)",
			addr, blockDuration, f.count, failureWindow, what)
	}
}

// forgetOld drops addresses with nothing left to remember, so the map
// doesn't grow without bound.
func (l *failureLimiter) forgetOld(now time.Time) {
	if len(l.addrs) < 1000 {
		return
	}
	for addr, f := range l.addrs {
		if now.Sub(f.windowStart) > failureWindow && !now.Before(f.blockedUntil) {
			delete(l.addrs, addr)
		}
	}
}

// refuseIfBlocked answers 429 when the request's address is blocked, and
// reports whether it did.
func (s *Server) refuseIfBlocked(w http.ResponseWriter, r *http.Request) bool {
	blocked, left := s.limiter.blocked(remoteAddr(r))
	if !blocked {
		return false
	}
	seconds := int(left.Round(time.Second) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
	writeError(w, http.StatusTooManyRequests, "too_many_requests",
		fmt.Sprintf("too many failed attempts from this address; try again in %s", left.Round(time.Second)))
	return true
}
