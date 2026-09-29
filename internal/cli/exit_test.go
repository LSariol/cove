package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExitStopsCoveInsteadOfKillingIt(t *testing.T) {
	c := New(nil, nil, Options{})
	stopped := false
	c.stop = func() { stopped = true }

	c.exit(context.Background(), []string{"exit"})

	if !stopped {
		t.Fatal("exit didn't call stop")
	}
}

func TestExecReportsUnknownCommandsAndUsage(t *testing.T) {
	captureOutput(t)
	c := New(nil, nil, Options{})
	ctx := context.Background()

	if err := c.Exec(ctx, []string{"nope"}); err == nil {
		t.Error("unknown command returned no error")
	}

	err := c.Exec(ctx, []string{"get"})
	var usage usageError
	if !errors.As(err, &usage) || err.Error() != "Usage: get <key>" {
		t.Errorf("get with no key = %v, want a usage error", err)
	}

	if err := c.Exec(ctx, []string{"help"}); err != nil {
		t.Errorf("help returned %v", err)
	}
}

func TestExitHelpDependsOnMode(t *testing.T) {
	for _, tc := range []struct {
		embedded bool
		want     string
	}{
		{true, "Stops Cove"},
		{false, "Leaves the shell"},
	} {
		c := New(nil, nil, Options{Embedded: tc.embedded})
		if got := c.byName["exit"].usages[0].help; !strings.HasPrefix(got, tc.want) {
			t.Errorf("embedded=%v: exit help = %q", tc.embedded, got)
		}
	}
}
