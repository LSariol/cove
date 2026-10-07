// Package tokenstest provides an in-memory tokens.Store for tests, so code
// built on a tokens.Manager can be tested without Postgres.
package tokenstest

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/LSariol/Cove/internal/tokens"
)

type entry struct {
	token tokens.Token
	hash  []byte
}

// Store is an in-memory tokens.Store. The zero value is not usable; call
// NewStore.
type Store struct {
	mu     sync.Mutex
	byName map[string]entry
	nextID int64

	Events []tokens.Event
}

func NewStore() *Store {
	return &Store{byName: make(map[string]entry)}
}

var _ tokens.Store = (*Store)(nil)

// WithinTokenTx runs fn and, like a real transaction, undoes its changes if it
// returns an error.
func (s *Store) WithinTokenTx(ctx context.Context, fn func(tx tokens.Store) error) error {
	s.mu.Lock()
	saved := make(map[string]entry, len(s.byName))
	for k, v := range s.byName {
		v.token.Read = slices.Clone(v.token.Read)
		v.token.Write = slices.Clone(v.token.Write)
		saved[k] = v
	}
	nextID, events := s.nextID, len(s.Events)
	s.mu.Unlock()

	if err := fn(s); err != nil {
		s.mu.Lock()
		s.byName, s.nextID, s.Events = saved, nextID, s.Events[:events]
		s.mu.Unlock()
		return err
	}
	return nil
}

func clone(t tokens.Token) tokens.Token {
	t.Read = slices.Clone(t.Read)
	t.Write = slices.Clone(t.Write)
	return t
}

func (s *Store) CreateToken(ctx context.Context, name string, hash []byte, read []string, write []string) (tokens.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.byName[name]; taken {
		return tokens.Token{}, fmt.Errorf("create token %q: %w", name, tokens.ErrExists)
	}
	for _, e := range s.byName {
		if bytes.Equal(e.hash, hash) {
			return tokens.Token{}, fmt.Errorf("create token %q: %w", name, tokens.ErrExists)
		}
	}

	s.nextID++
	t := tokens.Token{ID: s.nextID, Name: name, Read: slices.Clone(read), Write: slices.Clone(write), CreatedAt: time.Now()}
	s.byName[name] = entry{token: t, hash: hash}
	return clone(t), nil
}

func (s *Store) TokenByHash(ctx context.Context, hash []byte) (tokens.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range s.byName {
		if bytes.Equal(e.hash, hash) {
			return clone(e.token), nil
		}
	}
	return tokens.Token{}, fmt.Errorf("look up token: %w", tokens.ErrNotFound)
}

func (s *Store) TokenByName(ctx context.Context, name string) (tokens.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byName[name]
	if !ok {
		return tokens.Token{}, fmt.Errorf("token %q: %w", name, tokens.ErrNotFound)
	}
	return clone(e.token), nil
}

func (s *Store) ListTokens(ctx context.Context) ([]tokens.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var list []tokens.Token
	for _, e := range s.byName {
		list = append(list, clone(e.token))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (s *Store) AddTokenPattern(ctx context.Context, name string, pattern string, write bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byName[name]
	if !ok {
		return false, fmt.Errorf("token %q: %w", name, tokens.ErrNotFound)
	}
	list := &e.token.Read
	if write {
		list = &e.token.Write
	}
	if slices.Contains(*list, pattern) {
		return false, nil
	}
	*list = append(slices.Clone(*list), pattern)
	s.byName[name] = e
	return true, nil
}

func (s *Store) RemoveTokenPattern(ctx context.Context, name string, pattern string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byName[name]
	if !ok {
		return false, fmt.Errorf("token %q: %w", name, tokens.ErrNotFound)
	}
	if !e.token.Lists(pattern) {
		return false, nil
	}
	remove := func(list []string) []string {
		return slices.DeleteFunc(slices.Clone(list), func(p string) bool { return p == pattern })
	}
	e.token.Read, e.token.Write = remove(e.token.Read), remove(e.token.Write)
	s.byName[name] = e
	return true, nil
}

func (s *Store) ReplaceTokenPattern(ctx context.Context, oldPattern string, newPattern string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var names []string
	for name, e := range s.byName {
		if !e.token.Lists(oldPattern) {
			continue
		}
		replace := func(list []string) []string {
			out := slices.Clone(list)
			for i, p := range out {
				if p == oldPattern {
					out[i] = newPattern
				}
			}
			return out
		}
		e.token.Read, e.token.Write = replace(e.token.Read), replace(e.token.Write)
		s.byName[name] = e
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *Store) SetTokenHash(ctx context.Context, name string, hash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byName[name]
	if !ok {
		return fmt.Errorf("rotate token %q: %w", name, tokens.ErrNotFound)
	}
	now := time.Now()
	e.hash, e.token.RotatedAt = hash, &now
	s.byName[name] = e
	return nil
}

func (s *Store) DeleteToken(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byName[name]; !ok {
		return fmt.Errorf("revoke token %q: %w", name, tokens.ErrNotFound)
	}
	delete(s.byName, name)
	return nil
}

func (s *Store) TouchToken(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, e := range s.byName {
		if e.token.ID == id {
			now := time.Now()
			e.token.LastUsedAt = &now
			s.byName[name] = e
		}
	}
	return nil
}

func (s *Store) LogTokenEvent(ctx context.Context, e tokens.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e.OccurredAt = time.Now()
	s.Events = append(s.Events, e)
	return nil
}

func (s *Store) ListTokenEvents(ctx context.Context, name string, limit int) ([]tokens.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var events []tokens.Event
	for i := len(s.Events) - 1; i >= 0; i-- {
		if s.Events[i].TokenName == name {
			events = append(events, s.Events[i])
			if limit > 0 && len(events) == limit {
				break
			}
		}
	}
	return events, nil
}
