package cli

import (
	"context"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestCLI(t, "")

	o, _ := captureOutput(t)
	if err := c.Exec(ctx, []string{"generate", "new.key"}); err != nil {
		t.Fatal(err)
	}
	printed := strings.TrimSpace(o.String())
	if len(printed) != defaultGeneratedLength || value(t, c, "new.key") != printed {
		t.Fatalf("generate printed %q; stored %q", printed, value(t, c, "new.key"))
	}

	o, _ = captureOutput(t)
	if err := c.Exec(ctx, []string{"generate", "long.key", "64"}); err != nil {
		t.Fatal(err)
	}
	if n := len(strings.TrimSpace(o.String())); n != 64 {
		t.Errorf("generate with length 64 printed %d characters", n)
	}

	for _, bad := range []string{"8", "1000", "abc"} {
		if err := c.Exec(ctx, []string{"generate", "x.key", bad}); err == nil {
			t.Errorf("length %q was accepted", bad)
		}
	}
}

func TestGenerateAsksBeforeReplacing(t *testing.T) {
	ctx := context.Background()

	c, v := newTestCLI(t, "n\n")
	_ = v.Create(ctx, "app.key", "keep-me", "test")
	if err := c.Exec(ctx, []string{"generate", "app.key"}); err != nil || value(t, c, "app.key") != "keep-me" {
		t.Fatalf("answering n replaced the value (err %v)", err)
	}

	c, v = newTestCLI(t, "")
	_ = v.Create(ctx, "app.key", "keep-me", "test")
	if err := c.Exec(ctx, []string{"generate", "app.key"}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("no answer = %v, want a hint about --yes", err)
	}

	if err := c.Exec(ctx, []string{"generate", "app.key", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if got := value(t, c, "app.key"); got == "keep-me" || len(got) != defaultGeneratedLength {
		t.Fatalf("--yes didn't replace the value: %q", got)
	}
}
