package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func TestBootstrapCommands(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestCLI(t, "")
	c.bootstrap = bootstrap.NewGate(t.TempDir(), nil)

	run := func(args ...string) (string, string, error) {
		o, e := captureOutput(t)
		err := c.Exec(ctx, append([]string{"bootstrap"}, args...))
		return o.String(), e.String(), err
	}

	if o, _, err := run(); err != nil || !strings.Contains(o, "Endpoint:      closed") || !strings.Contains(o, "Last handout:  never") {
		t.Fatalf("bootstrap (status) = %q, %v", o, err)
	}

	if _, e, err := run("open"); err != nil || !strings.Contains(e, "(10m0s)") {
		t.Fatalf("bootstrap open = %q, %v", e, err)
	}
	if o, _, _ := run("status"); !strings.Contains(o, "open for") {
		t.Fatalf("status after open = %q", o)
	}

	if _, e, err := run("clear", "30m"); err != nil || !strings.Contains(e, "(30m0s)") {
		t.Fatalf("bootstrap clear 30m (the old name) = %q, %v", e, err)
	}

	if _, _, err := run("lock"); err != nil {
		t.Fatal(err)
	}
	if o, _, _ := run("status"); !strings.Contains(o, "Endpoint:      closed") {
		t.Fatalf("status after lock = %q", o)
	}

	for _, bad := range [][]string{{"open", "5s"}, {"open", "48h"}, {"open", "soon"}, {"nope"}} {
		if _, _, err := run(bad...); err == nil {
			t.Errorf("bootstrap %v was accepted", bad)
		}
	}
}
