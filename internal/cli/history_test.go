package cli

import (
	"context"
	"strings"
	"testing"
)

func TestInfoShowsDetailsNotValue(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	_ = v.Create(ctx, "app.key", "the-secret-value", "test")
	_, _ = v.Get(ctx, "app.key", "myapp") // an app pulls it

	o, _ := captureOutput(t)
	if err := c.Exec(ctx, []string{"info", "app.key"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app.key", "Version:", "1 (by apps", "by myapp"} {
		if !strings.Contains(o.String(), want) {
			t.Errorf("info is missing %q:\n%s", want, o.String())
		}
	}
	if strings.Contains(o.String(), "the-secret-value") {
		t.Error("info printed the secret's value")
	}
}

func TestHistoryAndDeletedSecrets(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	_ = v.Create(ctx, "app.key", "one", "test")
	_, _ = v.Update(ctx, "app.key", "two", "myapp")
	_ = v.Delete(ctx, "app.key", "admin")

	o, _ := captureOutput(t)
	if err := c.Exec(ctx, []string{"history", "app.key"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(o.String()), "\n")
	if len(lines) != 4 || !strings.Contains(lines[1], "delete") || !strings.Contains(lines[3], "create") {
		t.Errorf("history (newest first) =\n%s", o.String())
	}
	if strings.Contains(o.String(), "one") || strings.Contains(o.String(), "two") {
		t.Error("history printed a value")
	}

	o, _ = captureOutput(t)
	_ = c.Exec(ctx, []string{"history", "app.key", "1"})
	if n := len(strings.Split(strings.TrimSpace(o.String()), "\n")); n != 2 {
		t.Errorf("history with count 1 printed %d lines", n)
	}

	err := c.Exec(ctx, []string{"info", "app.key"})
	if err == nil || !strings.Contains(err.Error(), "deleted") || !strings.Contains(err.Error(), "restore app.key") {
		t.Errorf("info on a deleted secret = %v", err)
	}

	if err := c.Exec(ctx, []string{"history", "app.key", "zero"}); err == nil {
		t.Error("history accepted a non-number count")
	}
	if err := c.Exec(ctx, []string{"history", "never.existed"}); err == nil {
		t.Error("history of an unknown key returned no error")
	}
}
