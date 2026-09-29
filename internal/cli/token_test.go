package cli

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/tokens"
	"github.com/LSariol/Cove/internal/tokens/tokenstest"
)

// newTokenCLI returns a CLI with a token manager and the example secrets from
// the docs. input answers confirmation questions.
func newTokenCLI(t *testing.T, input string) (*CLI, *tokens.Manager) {
	t.Helper()
	c, v := newTestCLI(t, input)
	m := tokens.NewManager(tokenstest.NewStore())
	c.tokens = m

	ctx := context.Background()
	for _, key := range []string{"lighthouse.github-pat", "botsuite.db-url", "marquee.db-url", "shared.tmdb-api-key", "shared.discord-webhook"} {
		if err := v.Create(ctx, key, "x", "test"); err != nil {
			t.Fatal(err)
		}
	}
	return c, m
}

// run runs a command and returns its stdout, stderr and error.
func run(t *testing.T, c *CLI, line string) (string, string, error) {
	t.Helper()
	o, e := captureOutput(t)
	err := c.Exec(context.Background(), strings.Fields(line))
	return o.String(), e.String(), err
}

func TestTokenCreatePrintsTheTokenOnStdout(t *testing.T) {
	c, m := newTokenCLI(t, "")

	o, e, err := run(t, c, "token create marquee --allow marquee.* --allow=shared.tmdb-api-key")
	if err != nil {
		t.Fatal(err)
	}

	// stdout is just the token, so `cove token create x ... > file` works.
	value := strings.TrimSpace(o)
	if !strings.HasPrefix(value, tokens.Prefix) || strings.Contains(value, "\n") {
		t.Fatalf("stdout = %q, want only the token", o)
	}
	if _, err := m.Authenticate(context.Background(), value); err != nil {
		t.Fatalf("the printed token doesn't work: %v", err)
	}
	for _, want := range []string{"✓ Created a token for marquee", "can't be shown again", "It can reach 2 secrets now"} {
		if !strings.Contains(e, want) {
			t.Errorf("stderr is missing %q:\n%s", want, e)
		}
	}
}

func TestTokenCreateWarnsAboutPatternsThatMatchNothing(t *testing.T) {
	c, _ := newTokenCLI(t, "")
	_, e, err := run(t, c, "token create marquee --allow shared.tmdb-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e, "! shared.tmdb-key matches no secret yet.") {
		t.Fatalf("no typo warning:\n%s", e)
	}
}

func TestTokenCreateErrors(t *testing.T) {
	c, _ := newTokenCLI(t, "")
	run(t, c, "token create marquee")

	for line, want := range map[string]string{
		"token create":                        "Usage:",
		"token create marquee":                "already exists",
		"token create Bad":                    "isn't a valid token name",
		"token create x --allow":              "--allow needs a pattern",
		"token create x --allow a*b":          "isn't a valid pattern",
		"token create x y":                    "Unexpected",
		"token create cove_cli --allow *":     "reserved",
		"token frobnicate":                    "Unknown token option",
		"token create x --read marquee.*":     "Unexpected",
		"token create x --allow marquee.* -y": "Unexpected",
	} {
		_, _, err := run(t, c, line)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", line, err, want)
		}
	}
}

func TestTokenListAndShow(t *testing.T) {
	c, _ := newTokenCLI(t, "")
	run(t, c, "token create marquee --allow marquee.* --allow shared.tmdb-api-key --write marquee.cache.*")
	run(t, c, "token create lighthouse --allow lighthouse.*")

	// Quotes copied from the docs work at the cove> prompt too.
	if _, _, err := run(t, c, `token create quoted --allow 'shared.*'`); err != nil {
		t.Fatalf("quoted pattern: %v", err)
	}
	run(t, c, "token revoke quoted --yes")

	o, _, err := run(t, c, "token list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "lighthouse", "marquee.*, shared.tmdb-api-key", "marquee.cache.*", "never"} {
		if !strings.Contains(o, want) {
			t.Errorf("list is missing %q:\n%s", want, o)
		}
	}
	if o2, _, _ := run(t, c, "token"); o2 != o {
		t.Error(`"token" alone should list`)
	}

	o, e, err := run(t, c, "token show marquee")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"It can reach 2 secrets:", "marquee.db-url", "shared.tmdb-api-key", "Recent changes:", "create"} {
		if !strings.Contains(o, want) {
			t.Errorf("show is missing %q:\n%s", want, o)
		}
	}
	if strings.Contains(o, "botsuite") {
		t.Errorf("show lists a key marquee can't reach:\n%s", o)
	}
	if !strings.Contains(e, "marquee.cache.* matches no secret yet") {
		t.Errorf("no warning for the unmatched pattern:\n%s", e)
	}

	if _, _, err := run(t, c, "token show nope"); err == nil || !strings.Contains(err.Error(), `No token named "nope"`) {
		t.Errorf("show unknown: %v", err)
	}
}

func TestTokenAllowAndDeny(t *testing.T) {
	c, m := newTokenCLI(t, "")
	ctx := context.Background()
	run(t, c, "token create botsuite --allow botsuite.*")
	run(t, c, "token create marquee --allow marquee.*")

	_, e, err := run(t, c, "token allow shared.tmdb-api-key botsuite marquee")
	if err != nil || !strings.Contains(e, "botsuite and marquee can now read shared.tmdb-api-key.") {
		t.Fatalf("allow = %q, %v", e, err)
	}
	for _, name := range []string{"botsuite", "marquee"} {
		if tok, _ := m.Get(ctx, name); !tok.CanRead("shared.tmdb-api-key") {
			t.Errorf("%s can't read it after allow", name)
		}
	}

	_, e, _ = run(t, c, "token allow shared.tmdb-api-key marquee")
	if !strings.Contains(e, "marquee already had shared.tmdb-api-key; unchanged.") {
		t.Errorf("allow twice = %q", e)
	}

	_, e, err = run(t, c, "token deny shared.tmdb-api-key marquee")
	if err != nil || !strings.Contains(e, "Removed shared.tmdb-api-key from marquee.") {
		t.Fatalf("deny = %q, %v", e, err)
	}
	if tok, _ := m.Get(ctx, "marquee"); tok.CanRead("shared.tmdb-api-key") {
		t.Error("marquee can still read it after deny")
	}

	_, _, err = run(t, c, "token allow shared.x botsuite typo")
	if err == nil || !strings.Contains(err.Error(), `No token named "typo"`) {
		t.Errorf("allow with an unknown name: %v", err)
	}
	if tok, _ := m.Get(ctx, "botsuite"); tok.CanRead("shared.x") {
		t.Error("botsuite got access even though the command failed")
	}

	_, e, _ = run(t, c, "token allow shared.discord-* botsuite --write")
	if !strings.Contains(e, "botsuite can now read and change shared.discord-*.") {
		t.Errorf("allow --write = %q", e)
	}
}

func TestTokenDenyWarnsWhenAWildcardStillCoversTheKey(t *testing.T) {
	c, _ := newTokenCLI(t, "")
	run(t, c, "token create lighthouse --allow lighthouse.* --allow shared.* --allow shared.discord-webhook")

	_, e, _ := run(t, c, "token deny shared.discord-webhook lighthouse")
	if !strings.Contains(e, "lighthouse can still reach shared.discord-webhook through shared.*.") {
		t.Fatalf("no wildcard warning:\n%s", e)
	}
}

func TestTokenRotateAndRevokeAsk(t *testing.T) {
	ctx := context.Background()

	c, m := newTokenCLI(t, "n\n")
	o, _, _ := run(t, c, "token create marquee --allow marquee.*")
	first := strings.TrimSpace(o)

	if _, e, err := run(t, c, "token rotate marquee"); err != nil || !strings.Contains(e, "Rotate cancelled.") {
		t.Fatalf("rotate answered n: %q, %v", e, err)
	}
	if _, err := m.Authenticate(ctx, first); err != nil {
		t.Fatal("the token changed although the rotate was cancelled")
	}

	c.scanner = bufio.NewScanner(strings.NewReader("y\n"))
	o, e, err := run(t, c, "token rotate marquee")
	if err != nil || !strings.Contains(e, "The old token stopped working") {
		t.Fatalf("rotate = %q, %v", e, err)
	}
	second := strings.TrimSpace(o)
	if _, err := m.Authenticate(ctx, first); err == nil {
		t.Error("the old token still works")
	}
	if _, err := m.Authenticate(ctx, second); err != nil {
		t.Error("the printed new token doesn't work")
	}

	// No input to answer with (a one-shot command without a terminal).
	c.scanner = bufio.NewScanner(strings.NewReader(""))
	if _, _, err := run(t, c, "token revoke marquee"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("revoke without an answer: %v", err)
	}

	if _, e, err := run(t, c, "token revoke marquee --yes"); err != nil || !strings.Contains(e, "Revoked marquee's token.") {
		t.Fatalf("revoke --yes = %q, %v", e, err)
	}
	if _, err := m.Authenticate(ctx, second); err == nil {
		t.Error("a revoked token still works")
	}
}

func TestTokenCommandsWithoutADatabase(t *testing.T) {
	c, _ := newTestCLI(t, "")
	if _, _, err := run(t, c, "token list"); err == nil || !strings.Contains(err.Error(), "need the database") {
		t.Fatalf("err = %v", err)
	}
}
