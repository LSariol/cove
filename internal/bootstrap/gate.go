// Package bootstrap controls the one-time bootstrap endpoint, which hands a
// token to a new client that has no credentials yet (e.g. Lighthouse): the
// master token, or when opened for a project, that project's own token.
//
// The endpoint is closed unless someone opens it with `bootstrap open`, and
// then only for a limited time: it closes after one successful handout, or
// when the window runs out. For a short grace period after a handout, the same
// address can fetch the token again, so a client that crashed before saving it
// can recover on restart.
package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultWindow is how long `bootstrap open` keeps the endpoint open.
	DefaultWindow = 10 * time.Minute

	// GracePeriod is how long after a handout the same address can fetch the
	// token again.
	GracePeriod = 2 * time.Minute

	stateFile = "bootstrap.json"

	// legacyMarker is the file Cove v0.2.0 used to lock the endpoint. It's
	// removed on the next open or lock; the new state file replaces it.
	legacyMarker = "bootstrap_completed"
)

// Outcome is the result of a request to the bootstrap endpoint.
type Outcome string

const (
	Granted     Outcome = "granted"     // the token was handed out, and the endpoint closed
	Redelivered Outcome = "redelivered" // handed out again to the same address within the grace period
	Locked      Outcome = "locked"      // the endpoint is closed
	Expired     Outcome = "expired"     // the endpoint was opened, but the window ran out unused
	Forbidden   Outcome = "forbidden"   // the address isn't in the allowed networks
)

// Status describes the endpoint for `bootstrap status`.
type Status struct {
	Open          bool
	OpenUntil     time.Time // when an open window closes; after it passes, Expired is true
	Expired       bool
	LastHandoutAt time.Time // zero if the token has never been handed out
	LastHandoutTo string
	GraceUntil    time.Time
	Allowed       []netip.Prefix // empty means any address
	TokenName     string         // the project whose token is handed out; empty for the master token
}

// Handout is what a granted request receives: a project's token, or the
// master token when Token is empty.
type Handout struct {
	TokenName string
	Token     string
}

// state is what's saved in the state file.
type state struct {
	OpenUntil     time.Time `json:"open_until,omitzero"`
	LastHandoutAt time.Time `json:"last_handout_at,omitzero"`
	LastHandoutTo string    `json:"last_handout_to,omitzero"`
	GraceUntil    time.Time `json:"grace_until,omitzero"`

	// A project token to hand out instead of the master token. It's kept
	// only until the window and grace period are over, then removed.
	TokenName string `json:"token_name,omitzero"`
	Token     string `json:"token,omitzero"`
}

// Gate decides whether a bootstrap request may receive the client token. Its
// state lives in a file, so the CLI (a separate process) can open and lock it.
type Gate struct {
	dir     string
	allowed []netip.Prefix
	now     func() time.Time

	mu sync.Mutex // serializes Claim within the server process
}

// NewGate returns a Gate whose state lives in dir. allowed limits which
// addresses may bootstrap; empty means any address.
func NewGate(dir string, allowed []netip.Prefix) *Gate {
	return &Gate{dir: dir, allowed: allowed, now: time.Now}
}

// Open opens the endpoint for d (DefaultWindow if d <= 0) to hand out the
// master token, and returns when it will close.
func (g *Gate) Open(d time.Duration) (time.Time, error) {
	return g.OpenFor(d, "", "")
}

// OpenFor opens the endpoint for d (DefaultWindow if d <= 0) to hand out
// token, the named project's token, and returns when it will close. An empty
// token means the master token.
func (g *Gate) OpenFor(d time.Duration, tokenName string, token string) (time.Time, error) {
	if d <= 0 {
		d = DefaultWindow
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	st, err := g.load()
	if err != nil {
		return time.Time{}, err
	}
	st.OpenUntil = g.now().Add(d)
	st.GraceUntil = time.Time{} // the previous handout can't be fetched again
	st.TokenName, st.Token = tokenName, token
	return st.OpenUntil, g.save(st)
}

// Lock closes the endpoint immediately, including any grace period.
func (g *Gate) Lock() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	st, err := g.load()
	if err != nil {
		return err
	}
	st.OpenUntil = time.Time{}
	st.GraceUntil = time.Time{}
	st.TokenName, st.Token = "", ""
	return g.save(st)
}

// Status reports whether the endpoint is open and when the token was last
// handed out.
func (g *Gate) Status() (Status, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	st, err := g.load()
	if err != nil {
		return Status{}, err
	}

	now := g.now()
	if err := g.scrub(&st, now); err != nil {
		return Status{}, err
	}
	return Status{
		Open:          now.Before(st.OpenUntil),
		OpenUntil:     st.OpenUntil,
		Expired:       !st.OpenUntil.IsZero() && !now.Before(st.OpenUntil),
		LastHandoutAt: st.LastHandoutAt,
		LastHandoutTo: st.LastHandoutTo,
		GraceUntil:    st.GraceUntil,
		Allowed:       g.allowed,
		TokenName:     st.TokenName,
	}, nil
}

// scrub removes a project token from the state once nobody can receive it any
// more: the window is over and so is the grace period. It saves the change.
func (g *Gate) scrub(st *state, now time.Time) error {
	if st.Token == "" || now.Before(st.OpenUntil) || now.Before(st.GraceUntil) {
		return nil
	}
	st.Token = ""
	return g.save(*st)
}

// Claim decides a request from addr. Granted and Redelivered mean the caller
// may receive the Handout; a grant also closes the endpoint and starts the
// grace period for addr.
func (g *Gate) Claim(addr netip.Addr) (Outcome, Handout, error) {
	if !g.isAllowed(addr) {
		return Forbidden, Handout{}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	st, err := g.load()
	if err != nil {
		return "", Handout{}, err
	}

	now := g.now()
	handout := Handout{TokenName: st.TokenName, Token: st.Token}
	switch {
	case now.Before(st.OpenUntil):
		st.OpenUntil = time.Time{}
		st.LastHandoutAt = now
		st.LastHandoutTo = addr.String()
		st.GraceUntil = now.Add(GracePeriod)
		if err := g.save(st); err != nil {
			return "", Handout{}, err
		}
		return Granted, handout, nil

	case addr.String() == st.LastHandoutTo && now.Before(st.GraceUntil):
		return Redelivered, handout, nil
	}

	if err := g.scrub(&st, now); err != nil {
		return "", Handout{}, err
	}
	if !st.OpenUntil.IsZero() {
		return Expired, Handout{}, nil
	}
	return Locked, Handout{}, nil
}

func (g *Gate) isAllowed(addr netip.Addr) bool {
	if len(g.allowed) == 0 {
		return true
	}
	addr = addr.Unmap()
	for _, prefix := range g.allowed {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// load reads the state file. A missing file means a closed endpoint that has
// never handed out the token.
func (g *Gate) load() (state, error) {
	var st state

	data, err := os.ReadFile(filepath.Join(g.dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("read bootstrap state: %w", err)
	}

	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("bootstrap state file %s is corrupt: %w", filepath.Join(g.dir, stateFile), err)
	}
	return st, nil
}

// save writes the state file atomically (temp file, then rename), so a crash
// can't leave it half-written.
func (g *Gate) save(st state) error {
	if err := os.MkdirAll(g.dir, 0o700); err != nil {
		return fmt.Errorf("create bootstrap directory %s: %w", g.dir, err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(g.dir, stateFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("save bootstrap state: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("save bootstrap state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("save bootstrap state: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(g.dir, stateFile)); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("save bootstrap state: %w", err)
	}

	_ = os.Remove(filepath.Join(g.dir, legacyMarker))
	return nil
}
