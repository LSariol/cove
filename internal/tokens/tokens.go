// Package tokens manages per-project API tokens. Each project gets its own
// bearer token, limited to the secrets its patterns cover. There is no token
// that isn't one of these.
//
// A pattern is an exact key ("shared.tmdb-api-key") or a prefix ending in "*"
// ("lighthouse.*"; "*" alone matches every key). Read patterns let a token read
// matching secrets; write patterns let it read, create, update and delete them.
//
// Only a SHA-256 hash of each token is stored. The token itself is shown once,
// when it's created or rotated, and can't be recovered afterwards.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	// ErrNotFound is returned when no token has the given name or value.
	ErrNotFound = errors.New("token not found")

	// ErrExists is returned when creating a token whose name is taken.
	ErrExists = errors.New("a token with this name already exists")
)

// Prefix starts every project token, so one is easy to recognize (e.g. by a
// secret scanner).
const Prefix = "cove_"

// randomLength is the number of random characters after Prefix: 43 characters
// from 62 give about 256 bits.
const randomLength = 43

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Token is a project token, without its value.
type Token struct {
	ID         int64
	Name       string
	Read       []string // patterns this token can read
	Write      []string // patterns this token can read and change
	CreatedAt  time.Time
	RotatedAt  *time.Time // nil if never rotated
	LastUsedAt *time.Time // nil if never used
}

// CanRead reports whether the token may read key.
func (t Token) CanRead(key string) bool {
	return matchesAny(t.Read, key) || matchesAny(t.Write, key)
}

// CanWrite reports whether the token may create, update or delete key.
func (t Token) CanWrite(key string) bool {
	return matchesAny(t.Write, key)
}

// Lists reports whether one of the token's patterns is exactly key (not a
// wildcard that happens to cover it).
func (t Token) Lists(key string) bool {
	return slices.Contains(t.Read, key) || slices.Contains(t.Write, key)
}

// Event is an entry in cove.token_log.
type Event struct {
	TokenName  string
	Action     string // create, rotate, revoke, allow, deny, rename_key
	Detail     string
	Source     string
	OccurredAt time.Time
}

// Matches reports whether pattern covers key.
func Matches(pattern string, key string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(key, prefix)
	}
	return pattern == key
}

func matchesAny(patterns []string, key string) bool {
	for _, p := range patterns {
		if Matches(p, key) {
			return true
		}
	}
	return false
}

var nameFormat = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidateName checks a token name: 1–64 lowercase letters, digits, '.', '_'
// or '-', starting with a letter or digit. The name is what the event log
// records as the source of the project's requests. Names starting with "cove"
// are reserved (the CLI records itself as "cove_cli").
func ValidateName(name string) error {
	if !nameFormat.MatchString(name) {
		return fmt.Errorf("%q isn't a valid token name: use 1-64 lowercase letters, digits, '.', '_' or '-', starting with a letter or digit", name)
	}
	if strings.HasPrefix(name, "cove") {
		return fmt.Errorf("%q isn't a valid token name: names starting with \"cove\" are reserved", name)
	}
	return nil
}

// maxKeyLength matches the vault's key rule.
const maxKeyLength = 256

// ValidatePattern checks a pattern: a valid secret key, a key prefix followed
// by "*", or "*" alone. Keys follow the vault's rule (vault.ValidateKey): 1–256
// letters, digits, '.', '_' or '-'.
func ValidatePattern(pattern string) error {
	if pattern == "*" {
		return nil
	}
	body := strings.TrimSuffix(pattern, "*")
	if body == "" || len(body) > maxKeyLength {
		return fmt.Errorf("%q isn't a valid pattern: use a key, a key prefix ending in *, or * for every key", pattern)
	}
	for _, c := range body {
		if !isKeyChar(c) {
			return fmt.Errorf("%q isn't a valid pattern: %q can't be in a key (only letters, digits, '.', '_' and '-'; * only at the end)", pattern, c)
		}
	}
	return nil
}

func isKeyChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.'
}

// Generate returns a new random token and its hash.
func Generate() (token string, hash []byte, err error) {
	b := make([]byte, randomLength)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", nil, fmt.Errorf("generate token: %w", err)
		}
		b[i] = alphabet[n.Int64()]
	}
	token = Prefix + string(b)
	return token, Hash(token), nil
}

// Hash returns the SHA-256 hash stored for token. Tokens are long random
// strings, so a plain hash (no salt or slow hashing) is enough: there's
// nothing to guess.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
