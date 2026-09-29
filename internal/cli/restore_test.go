package cli

import (
	"context"
	"strings"
	"testing"
)

func value(t *testing.T, c *CLI, key string) string {
	t.Helper()
	s, err := c.vault.Show(context.Background(), key, "test")
	if err != nil {
		t.Fatalf("show %s: %v", key, err)
	}
	return s.Value
}

func TestRestorePreviousAndSpecificVersions(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	_ = v.Create(ctx, "app.key", "one", "test")
	_, _ = v.Update(ctx, "app.key", "two", "test")
	_, _ = v.Update(ctx, "app.key", "three", "test")

	// No version: the value before the current one. Saved as version 4.
	if err := c.Exec(ctx, []string{"restore", "app.key", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if got := value(t, c, "app.key"); got != "two" {
		t.Fatalf("after restore = %q, want two", got)
	}

	// A specific version.
	if err := c.Exec(ctx, []string{"restore", "app.key", "1", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if got := value(t, c, "app.key"); got != "one" {
		t.Fatalf("after restore 1 = %q, want one", got)
	}

	events, _ := v.History(ctx, "app.key", 0)
	restores := 0
	for _, e := range events {
		if strings.HasPrefix(e.Detail, "restored version") {
			restores++
		}
	}
	if restores != 2 {
		t.Errorf("history has %d restore events, want 2", restores)
	}
}

func TestRestoreDeletedSecret(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	_ = v.Create(ctx, "app.key", "one", "test")
	_, _ = v.Update(ctx, "app.key", "last value", "test")
	_ = v.Delete(ctx, "app.key", "test")

	// No confirmation needed: nothing is being replaced.
	if err := c.Exec(ctx, []string{"restore", "app.key"}); err != nil {
		t.Fatal(err)
	}
	if got := value(t, c, "app.key"); got != "last value" {
		t.Fatalf("restored deleted secret = %q, want its last value", got)
	}
}

func TestRestoreAsksAndExplainsProblems(t *testing.T) {
	ctx := context.Background()

	c, v := newTestCLI(t, "n\n")
	_ = v.Create(ctx, "app.key", "one", "test")
	_, _ = v.Update(ctx, "app.key", "two", "test")
	if err := c.Exec(ctx, []string{"restore", "app.key"}); err != nil || value(t, c, "app.key") != "two" {
		t.Fatalf("answering n changed the value (err %v)", err)
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"restore", "never.existed"}, "no history"},
		{[]string{"restore", "app.key", "9", "--yes"}, "has no version 9"},
		{[]string{"restore", "app.key", "2", "--yes"}, "Nothing to restore"},
		{[]string{"restore", "app.key", "x"}, "isn't a version number"},
	} {
		if err := c.Exec(ctx, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v = %v, want it to mention %q", tc.args, err, tc.want)
		}
	}

	fresh, v2 := newTestCLI(t, "")
	_ = v2.Create(ctx, "new.key", "only", "test")
	if err := fresh.Exec(ctx, []string{"restore", "new.key", "--yes"}); err == nil || !strings.Contains(err.Error(), "Nothing to restore") {
		t.Errorf("restore at version 1 = %v", err)
	}
}
