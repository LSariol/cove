package tokens

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
)

// Store is what a Manager needs from the database. *database.Database
// implements it; tokenstest.Store is an in-memory version for tests.
type Store interface {
	CreateToken(ctx context.Context, name string, hash []byte, read []string, write []string) (Token, error)
	TokenByHash(ctx context.Context, hash []byte) (Token, error)
	TokenByName(ctx context.Context, name string) (Token, error)
	ListTokens(ctx context.Context) ([]Token, error)

	// AddTokenPattern adds pattern to the token's read (or write) patterns,
	// and reports false if it was already there.
	AddTokenPattern(ctx context.Context, name string, pattern string, write bool) (added bool, err error)

	// RemoveTokenPattern removes pattern from both of the token's lists, and
	// reports false if it was in neither.
	RemoveTokenPattern(ctx context.Context, name string, pattern string) (removed bool, err error)

	// ReplaceTokenPattern replaces oldPattern with newPattern in every token,
	// and returns the names of the tokens changed.
	ReplaceTokenPattern(ctx context.Context, oldPattern string, newPattern string) ([]string, error)

	SetTokenHash(ctx context.Context, name string, hash []byte) error
	DeleteToken(ctx context.Context, name string) error
	TouchToken(ctx context.Context, id int64) error

	LogTokenEvent(ctx context.Context, e Event) error
	ListTokenEvents(ctx context.Context, name string, limit int) ([]Event, error)

	// WithinTokenTx runs fn in a transaction, like database.Store.WithinTx.
	WithinTokenTx(ctx context.Context, fn func(tx Store) error) error
}

// Manager creates, changes and checks project tokens. Every change and its
// token_log entry are saved together or not at all.
type Manager struct {
	store Store
}

func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// Create makes a token for a project and returns its value, which is shown
// once and can't be recovered. read and write are patterns; see the package
// docs.
func (m *Manager) Create(ctx context.Context, name string, read []string, write []string, source string) (string, Token, error) {
	if err := ValidateName(name); err != nil {
		return "", Token{}, err
	}
	read, write, err := cleanPatterns(read, write)
	if err != nil {
		return "", Token{}, err
	}

	value, hash, err := Generate()
	if err != nil {
		return "", Token{}, err
	}

	var created Token
	err = m.store.WithinTokenTx(ctx, func(tx Store) error {
		created, err = tx.CreateToken(ctx, name, hash, read, write)
		if err != nil {
			return err
		}
		return logEvent(ctx, tx, Event{TokenName: name, Action: "create", Detail: describeAccess(read, write), Source: source})
	})
	if err != nil {
		return "", Token{}, err
	}
	return value, created, nil
}

// Rotate gives a token a new value, keeping its name and access. The old value
// stops working at once. detail is recorded in the log (optional).
func (m *Manager) Rotate(ctx context.Context, name string, source string, detail string) (string, error) {
	value, hash, err := Generate()
	if err != nil {
		return "", err
	}

	err = m.store.WithinTokenTx(ctx, func(tx Store) error {
		if err := tx.SetTokenHash(ctx, name, hash); err != nil {
			return err
		}
		return logEvent(ctx, tx, Event{TokenName: name, Action: "rotate", Detail: detail, Source: source})
	})
	if err != nil {
		return "", err
	}
	return value, nil
}

// Revoke deletes a token. It stops working at once; its log entries remain.
func (m *Manager) Revoke(ctx context.Context, name string, source string) error {
	return m.store.WithinTokenTx(ctx, func(tx Store) error {
		if err := tx.DeleteToken(ctx, name); err != nil {
			return err
		}
		return logEvent(ctx, tx, Event{TokenName: name, Action: "revoke", Source: source})
	})
}

// Allow adds pattern to each named token: as a read pattern, or with write as
// a write pattern. Nothing changes unless every name exists. It returns the
// names that already had it.
func (m *Manager) Allow(ctx context.Context, pattern string, write bool, names []string, source string) (already []string, err error) {
	if err := ValidatePattern(pattern); err != nil {
		return nil, err
	}
	access := "read"
	if write {
		access = "read/write"
	}

	err = m.store.WithinTokenTx(ctx, func(tx Store) error {
		already = nil
		for _, name := range names {
			added, err := tx.AddTokenPattern(ctx, name, pattern, write)
			if err != nil {
				return err
			}
			if !added {
				already = append(already, name)
				continue
			}
			if err := logEvent(ctx, tx, Event{TokenName: name, Action: "allow", Detail: access + " " + pattern, Source: source}); err != nil {
				return err
			}
		}
		return nil
	})
	return already, err
}

// Deny removes pattern from each named token. Nothing changes unless every
// name exists. It returns the names that didn't have it.
func (m *Manager) Deny(ctx context.Context, pattern string, names []string, source string) (notListed []string, err error) {
	err = m.store.WithinTokenTx(ctx, func(tx Store) error {
		notListed = nil
		for _, name := range names {
			removed, err := tx.RemoveTokenPattern(ctx, name, pattern)
			if err != nil {
				return err
			}
			if !removed {
				notListed = append(notListed, name)
				continue
			}
			if err := logEvent(ctx, tx, Event{TokenName: name, Action: "deny", Detail: pattern, Source: source}); err != nil {
				return err
			}
		}
		return nil
	})
	return notListed, err
}

// RenameKey updates every token that lists oldKey by name, after the secret
// was renamed, and returns their names.
func (m *Manager) RenameKey(ctx context.Context, oldKey string, newKey string, source string) ([]string, error) {
	var names []string
	err := m.store.WithinTokenTx(ctx, func(tx Store) error {
		var err error
		names, err = tx.ReplaceTokenPattern(ctx, oldKey, newKey)
		if err != nil {
			return err
		}
		for _, name := range names {
			e := Event{TokenName: name, Action: "rename_key", Detail: oldKey + " -> " + newKey, Source: source}
			if err := logEvent(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	return names, err
}

// Get returns the named token.
func (m *Manager) Get(ctx context.Context, name string) (Token, error) {
	return m.store.TokenByName(ctx, name)
}

// List returns every token, ordered by name.
func (m *Manager) List(ctx context.Context) ([]Token, error) {
	return m.store.ListTokens(ctx)
}

// History returns the named token's log entries, newest first.
func (m *Manager) History(ctx context.Context, name string, limit int) ([]Event, error) {
	return m.store.ListTokenEvents(ctx, name, limit)
}

// Listing returns the names of the tokens that list key exactly (not through
// a wildcard): renaming or deleting key takes it away from them.
func (m *Manager) Listing(ctx context.Context, key string) ([]string, error) {
	return m.namesWhere(ctx, func(t Token) bool { return t.Lists(key) })
}

// Readers returns the names of the tokens that can read key.
func (m *Manager) Readers(ctx context.Context, key string) ([]string, error) {
	return m.namesWhere(ctx, func(t Token) bool { return t.CanRead(key) })
}

func (m *Manager) namesWhere(ctx context.Context, keep func(Token) bool) ([]string, error) {
	all, err := m.store.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, t := range all {
		if keep(t) {
			names = append(names, t.Name)
		}
	}
	return names, nil
}

// Authenticate returns the token whose value is value, and records that it
// was used. It returns ErrNotFound for an unknown value.
func (m *Manager) Authenticate(ctx context.Context, value string) (Token, error) {
	if !strings.HasPrefix(value, Prefix) {
		return Token{}, ErrNotFound
	}
	t, err := m.store.TokenByHash(ctx, Hash(value))
	if err != nil {
		return Token{}, err
	}
	// Last-used is a convenience for `token list`; a failure to save it
	// mustn't fail the request.
	if err := m.store.TouchToken(ctx, t.ID); err != nil {
		log.Printf("tokens: record last use of %q: %v", t.Name, err)
	}
	return t, nil
}

func logEvent(ctx context.Context, tx Store, e Event) error {
	if err := tx.LogTokenEvent(ctx, e); err != nil {
		return fmt.Errorf("record the %s of token %q (nothing was changed): %w", e.Action, e.TokenName, err)
	}
	return nil
}

// cleanPatterns validates both lists and removes duplicates. A pattern given
// for writing is dropped from the read list, since writing includes reading.
func cleanPatterns(read []string, write []string) ([]string, []string, error) {
	var r, w []string
	for _, p := range write {
		if err := ValidatePattern(p); err != nil {
			return nil, nil, err
		}
		if !slices.Contains(w, p) {
			w = append(w, p)
		}
	}
	for _, p := range read {
		if err := ValidatePattern(p); err != nil {
			return nil, nil, err
		}
		if !slices.Contains(r, p) && !slices.Contains(w, p) {
			r = append(r, p)
		}
	}
	if r == nil {
		r = []string{}
	}
	if w == nil {
		w = []string{}
	}
	return r, w, nil
}

// describeAccess summarizes patterns for the log, e.g.
// "read lighthouse.*, shared.x; write lighthouse.cache".
func describeAccess(read []string, write []string) string {
	var parts []string
	if len(read) > 0 {
		parts = append(parts, "read "+strings.Join(read, ", "))
	}
	if len(write) > 0 {
		parts = append(parts, "read/write "+strings.Join(write, ", "))
	}
	if len(parts) == 0 {
		return "no access"
	}
	return strings.Join(parts, "; ")
}

// IsNotFound reports whether err means the token doesn't exist.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
