package tokens_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/tokens"
	"github.com/LSariol/Cove/internal/tokens/tokenstest"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		pattern, key string
		want         bool
	}{
		{"*", "anything.at-all", true},
		{"lighthouse.*", "lighthouse.github-pat", true},
		{"lighthouse.*", "lighthouse.", true},
		{"lighthouse.*", "lighthousex.github-pat", false},
		{"lighthouse.*", "botsuite.db-url", false},
		{"shared.tmdb-api-key", "shared.tmdb-api-key", true},
		{"shared.tmdb-api-key", "shared.tmdb-api-key2", false},
		{"shared.tmdb", "shared.tmdb-api-key", false},
		{"Lighthouse.*", "lighthouse.x", false}, // keys are case-sensitive
	}
	for _, tc := range cases {
		if got := tokens.Matches(tc.pattern, tc.key); got != tc.want {
			t.Errorf("Matches(%q, %q) = %v", tc.pattern, tc.key, got)
		}
	}
}

func TestReadAndWriteAccess(t *testing.T) {
	tok := tokens.Token{Read: []string{"marquee.*", "shared.tmdb-api-key"}, Write: []string{"marquee.cache.*"}}

	for key, want := range map[string][2]bool{ // {read, write}
		"marquee.db-url":      {true, false},
		"marquee.cache.x":     {true, true},
		"shared.tmdb-api-key": {true, false},
		"botsuite.db-url":     {false, false},
	} {
		if tok.CanRead(key) != want[0] || tok.CanWrite(key) != want[1] {
			t.Errorf("%s: read=%v write=%v, want %v", key, tok.CanRead(key), tok.CanWrite(key), want)
		}
	}

	if !tok.Lists("shared.tmdb-api-key") || tok.Lists("marquee.db-url") {
		t.Error("Lists should only match patterns that are exactly the key")
	}
}

func TestValidatePattern(t *testing.T) {
	for _, ok := range []string{"*", "a.*", "lighthouse.github-pat", "MYAPP_*", strings.Repeat("k", 256)} {
		if err := tokens.ValidatePattern(ok); err != nil {
			t.Errorf("ValidatePattern(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "**", "a*b", "*.x", "has space", "a/b*", strings.Repeat("k", 257)} {
		if err := tokens.ValidatePattern(bad); err == nil {
			t.Errorf("ValidatePattern(%q) was accepted", bad)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"lighthouse", "bot-suite", "a", "twitch.bot_2"} {
		if err := tokens.ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Lighthouse", "-x", "has space", "cove_cli", "cove", strings.Repeat("a", 65)} {
		if err := tokens.ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) was accepted", bad)
		}
	}
}

func TestGenerate(t *testing.T) {
	a, hashA, err := tokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := tokens.Generate()

	if !strings.HasPrefix(a, tokens.Prefix) || len(a) != len(tokens.Prefix)+43 {
		t.Fatalf("token %q has the wrong shape", a)
	}
	if a == b {
		t.Fatal("two tokens were the same")
	}
	if !slices.Equal(hashA, tokens.Hash(a)) || len(hashA) != 32 {
		t.Fatal("the hash doesn't match the token")
	}
}

func newManager() (*tokens.Manager, *tokenstest.Store) {
	store := tokenstest.NewStore()
	return tokens.NewManager(store), store
}

func TestCreateAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	m, store := newManager()

	value, tok, err := m.Create(ctx, "marquee", []string{"marquee.*", "shared.tmdb-api-key", "marquee.*"}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tok.Read, []string{"marquee.*", "shared.tmdb-api-key"}) {
		t.Fatalf("read patterns = %v, want duplicates removed", tok.Read)
	}

	got, err := m.Authenticate(ctx, value)
	if err != nil || got.Name != "marquee" {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
	if got, _ := m.Get(ctx, "marquee"); got.LastUsedAt == nil {
		t.Error("last use wasn't recorded")
	}

	for _, wrong := range []string{"", "cove_nope", "not-a-cove-token", value + "x"} {
		if _, err := m.Authenticate(ctx, wrong); !errors.Is(err, tokens.ErrNotFound) {
			t.Errorf("Authenticate(%q) = %v, want ErrNotFound", wrong, err)
		}
	}

	if _, _, err := m.Create(ctx, "marquee", nil, nil, "test"); !errors.Is(err, tokens.ErrExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if len(store.Events) != 1 || store.Events[0].Action != "create" || !strings.Contains(store.Events[0].Detail, "shared.tmdb-api-key") {
		t.Fatalf("events = %+v", store.Events)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	m, store := newManager()
	ctx := context.Background()

	if _, _, err := m.Create(ctx, "Bad Name", nil, nil, "test"); err == nil {
		t.Error("bad name accepted")
	}
	if _, _, err := m.Create(ctx, "ok", []string{"a*b"}, nil, "test"); err == nil {
		t.Error("bad pattern accepted")
	}
	if len(store.Events) != 0 {
		t.Error("a refused create was logged")
	}
}

func TestWritePatternsAreNotRepeatedAsRead(t *testing.T) {
	m, _ := newManager()
	_, tok, err := m.Create(context.Background(), "bot", []string{"bot.*", "shared.x"}, []string{"bot.*"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tok.Read, []string{"shared.x"}) || !slices.Equal(tok.Write, []string{"bot.*"}) {
		t.Fatalf("read=%v write=%v", tok.Read, tok.Write)
	}
}

func TestRotate(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager()
	old, _, _ := m.Create(ctx, "lighthouse", []string{"lighthouse.*"}, nil, "test")

	value, err := m.Rotate(ctx, "lighthouse", "test", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(ctx, old); !errors.Is(err, tokens.ErrNotFound) {
		t.Error("the old value still works after rotating")
	}
	if got, err := m.Authenticate(ctx, value); err != nil || !slices.Equal(got.Read, []string{"lighthouse.*"}) {
		t.Errorf("new value: %+v, %v (access should be kept)", got, err)
	}
	if _, err := m.Rotate(ctx, "nope", "test", ""); !errors.Is(err, tokens.ErrNotFound) {
		t.Errorf("rotate unknown: %v", err)
	}
}

func TestRevoke(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager()
	value, _, _ := m.Create(ctx, "marquee", nil, nil, "test")

	if err := m.Revoke(ctx, "marquee", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(ctx, value); !errors.Is(err, tokens.ErrNotFound) {
		t.Error("a revoked token still works")
	}
	if err := m.Revoke(ctx, "marquee", "test"); !errors.Is(err, tokens.ErrNotFound) {
		t.Errorf("revoking twice: %v", err)
	}
	if events, _ := m.History(ctx, "marquee", 0); len(events) != 2 || events[0].Action != "revoke" {
		t.Errorf("history after revoke = %+v (should outlive the token)", events)
	}
}

func TestAllowAndDeny(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager()
	m.Create(ctx, "botsuite", []string{"botsuite.*"}, nil, "test")
	m.Create(ctx, "marquee", []string{"marquee.*", "shared.openai-key"}, nil, "test")

	already, err := m.Allow(ctx, "shared.openai-key", false, []string{"botsuite", "marquee"}, "test")
	if err != nil || !slices.Equal(already, []string{"marquee"}) {
		t.Fatalf("Allow = %v, %v", already, err)
	}
	for _, name := range []string{"botsuite", "marquee"} {
		if tok, _ := m.Get(ctx, name); !tok.CanRead("shared.openai-key") {
			t.Errorf("%s can't read shared.openai-key after allow", name)
		}
	}

	notListed, err := m.Deny(ctx, "shared.openai-key", []string{"marquee", "botsuite"}, "test")
	if err != nil || len(notListed) != 0 {
		t.Fatalf("Deny = %v, %v", notListed, err)
	}
	if tok, _ := m.Get(ctx, "marquee"); tok.CanRead("shared.openai-key") {
		t.Error("marquee can still read it after deny")
	}

	if _, err := m.Allow(ctx, "shared.x", true, []string{"botsuite"}, "test"); err != nil {
		t.Fatal(err)
	}
	if tok, _ := m.Get(ctx, "botsuite"); !tok.CanWrite("shared.x") {
		t.Error("allow --write didn't grant writing")
	}
}

func TestAllowIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	m, store := newManager()
	m.Create(ctx, "botsuite", nil, nil, "test")
	events := len(store.Events)

	_, err := m.Allow(ctx, "shared.x", false, []string{"botsuite", "typo"}, "test")
	if !errors.Is(err, tokens.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for the unknown name", err)
	}
	if tok, _ := m.Get(ctx, "botsuite"); tok.CanRead("shared.x") {
		t.Error("botsuite got access even though the command failed")
	}
	if len(store.Events) != events {
		t.Error("a failed allow left log entries")
	}
}

func TestRenameKeyAndListing(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager()
	m.Create(ctx, "botsuite", []string{"botsuite.*", "shared.tmdb-api-key"}, nil, "test")
	m.Create(ctx, "marquee", []string{"marquee.*", "shared.tmdb-api-key"}, nil, "test")
	m.Create(ctx, "everything", []string{"*"}, nil, "test")

	listing, _ := m.Listing(ctx, "shared.tmdb-api-key")
	if !slices.Equal(listing, []string{"botsuite", "marquee"}) {
		t.Fatalf("Listing = %v (the * token covers it but doesn't list it)", listing)
	}
	readers, _ := m.Readers(ctx, "shared.tmdb-api-key")
	if !slices.Equal(readers, []string{"botsuite", "everything", "marquee"}) {
		t.Fatalf("Readers = %v", readers)
	}

	names, err := m.RenameKey(ctx, "shared.tmdb-api-key", "shared.tmdb-key", "test")
	if err != nil || !slices.Equal(names, []string{"botsuite", "marquee"}) {
		t.Fatalf("RenameKey = %v, %v", names, err)
	}
	if tok, _ := m.Get(ctx, "marquee"); !tok.CanRead("shared.tmdb-key") || tok.CanRead("shared.tmdb-api-key") {
		t.Errorf("after rename: %v", tok.Read)
	}
}
