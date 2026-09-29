package cli

import (
	"context"
	"strings"
	"testing"
)

func TestHelpForOneCommand(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestCLI(t, "")

	o, _ := captureOutput(t)
	if err := c.Exec(ctx, []string{"help", "delete"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.String(), "delete, d <key> [--yes]") || strings.Contains(o.String(), "get, g") {
		t.Errorf("help delete = %q", o.String())
	}

	o, _ = captureOutput(t)
	_ = c.Exec(ctx, []string{"help"})
	for _, want := range []string{"get, g <key>", "search, s <text>", `help <command>`} {
		if !strings.Contains(o.String(), want) {
			t.Errorf("full help is missing %q", want)
		}
	}

	if err := c.Exec(ctx, []string{"help", "nope"}); err == nil {
		t.Error("help for an unknown command returned no error")
	}
}
