package cli

import (
	"bufio"
	"context"
	"strings"
	"testing"
)

// These tests cover how rename, delete and info treat keys that tokens use.

func TestRenameAsksAndUpdatesTokensThatListTheKey(t *testing.T) {
	ctx := context.Background()
	c, m := newTokenCLI(t, "y\n")
	run(t, c, "token create botsuite --allow botsuite.* --allow shared.tmdb-api-key")
	run(t, c, "token create marquee --allow marquee.* --allow shared.tmdb-api-key")
	run(t, c, "token create everything --allow *")

	_, e, err := run(t, c, "rename shared.tmdb-api-key shared.tmdb-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`botsuite and marquee's tokens list "shared.tmdb-api-key" by name.`,
		`Updated botsuite and marquee's tokens to "shared.tmdb-key".`,
	} {
		if !strings.Contains(e, want) {
			t.Errorf("stderr is missing %q:\n%s", want, e)
		}
	}
	for _, name := range []string{"botsuite", "marquee"} {
		tok, _ := m.Get(ctx, name)
		if !tok.CanRead("shared.tmdb-key") || tok.Lists("shared.tmdb-api-key") {
			t.Errorf("%s after rename: %v", name, tok.Read)
		}
	}
}

func TestRenameCancelledLeavesEverything(t *testing.T) {
	ctx := context.Background()
	c, m := newTokenCLI(t, "n\n")
	run(t, c, "token create marquee --allow shared.tmdb-api-key")

	if _, e, err := run(t, c, "rename shared.tmdb-api-key shared.tmdb-key"); err != nil || !strings.Contains(e, "Rename cancelled.") {
		t.Fatalf("answered n: %q, %v", e, err)
	}
	if _, err := c.vault.Show(ctx, "shared.tmdb-api-key", "test"); err != nil {
		t.Error("the secret was renamed although the answer was no")
	}
	if tok, _ := m.Get(ctx, "marquee"); !tok.Lists("shared.tmdb-api-key") {
		t.Error("the token changed although the answer was no")
	}

	// Without input to answer with, it refuses and points at --yes.
	c.scanner = bufio.NewScanner(strings.NewReader(""))
	if _, _, err := run(t, c, "rename shared.tmdb-api-key shared.tmdb-key"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("no answer: %v", err)
	}
	if _, _, err := run(t, c, "rename shared.tmdb-api-key shared.tmdb-key --yes"); err != nil {
		t.Fatalf("--yes: %v", err)
	}
	if tok, _ := m.Get(ctx, "marquee"); !tok.Lists("shared.tmdb-key") {
		t.Error("--yes didn't update the token")
	}
}

func TestRenameWithoutTokensDoesNotAsk(t *testing.T) {
	c, _ := newTokenCLI(t, "") // no input: asking would fail
	run(t, c, "token create marquee --allow marquee.*")

	// marquee.* covers both names, so nothing needs updating.
	if _, _, err := run(t, c, "rename marquee.db-url marquee.database-url"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteWarnsAboutTokensThatListTheKey(t *testing.T) {
	c, m := newTokenCLI(t, "")
	run(t, c, "token create botsuite --allow shared.tmdb-api-key")

	_, e, err := run(t, c, "delete shared.tmdb-api-key --yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e, `botsuite's token lists "shared.tmdb-api-key" by name; those projects will get "not found".`) {
		t.Fatalf("no warning:\n%s", e)
	}
	// The token keeps the key, so a restore makes it reachable again.
	if tok, _ := m.Get(context.Background(), "botsuite"); !tok.Lists("shared.tmdb-api-key") {
		t.Error("delete removed the key from the token")
	}
}

func TestInfoShowsWhichTokensCanRead(t *testing.T) {
	c, _ := newTokenCLI(t, "")

	o, _, err := run(t, c, "info shared.tmdb-api-key")
	if err != nil || !strings.Contains(o, "Readable by:  no project tokens (only the master token)") {
		t.Fatalf("info before any token = %q, %v", o, err)
	}

	run(t, c, "token create botsuite --allow shared.*")
	run(t, c, "token create marquee --allow shared.tmdb-api-key")
	run(t, c, "token create lighthouse --allow lighthouse.*")

	o, _, _ = run(t, c, "info shared.tmdb-api-key")
	if !strings.Contains(o, "Readable by:  botsuite, marquee (and the master token)") {
		t.Fatalf("info = %q", o)
	}
}
