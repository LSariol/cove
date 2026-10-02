package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/database"
)

func TestBootstrapCommands(t *testing.T) {
	ctx := context.Background()
	c, _ := newTokenCLI(t, "")
	c.bootstrap = bootstrap.NewGate(t.TempDir(), nil)
	if err := c.Exec(ctx, []string{"token", "create", "lighthouse", "--allow", "*"}); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, string, error) {
		o, e := captureOutput(t)
		err := c.Exec(ctx, append([]string{"bootstrap"}, args...))
		return o.String(), e.String(), err
	}

	if o, _, err := run(); err != nil || !strings.Contains(o, "Endpoint:      closed") || !strings.Contains(o, "Last handout:  never") {
		t.Fatalf("bootstrap (status) = %q, %v", o, err)
	}

	if _, e, err := run("open", "lighthouse"); err != nil || !strings.Contains(e, "(10m0s)") {
		t.Fatalf("bootstrap open lighthouse = %q, %v", e, err)
	}
	if o, _, _ := run("status"); !strings.Contains(o, "open for") {
		t.Fatalf("status after open = %q", o)
	}

	if _, e, err := run("open", "lighthouse", "30m"); err != nil || !strings.Contains(e, "(30m0s)") {
		t.Fatalf("bootstrap open lighthouse 30m = %q, %v", e, err)
	}

	if _, _, err := run("lock"); err != nil {
		t.Fatal(err)
	}
	if o, _, _ := run("status"); !strings.Contains(o, "Endpoint:      closed") || strings.Contains(o, "Hands out") {
		t.Fatalf("status after lock = %q", o)
	}

	// A project is required: there's no token to hand out without one.
	bad := [][]string{{"open"}, {"open", "30m"}, {"open", "lighthouse", "5s"}, {"open", "lighthouse", "48h"}, {"open", "soon"}, {"nope"}}
	for _, bad := range bad {
		if _, _, err := run(bad...); err == nil {
			t.Errorf("bootstrap %v was accepted", bad)
		}
	}
}

func TestBootstrapStatusShowsRecentAttempts(t *testing.T) {
	c, _ := newTestCLI(t, "")
	c.bootstrap = bootstrap.NewGate(t.TempDir(), nil)
	c.db = &fakeDB{attempts: []database.BootstrapAttempt{
		{OccurredAt: time.Now(), RemoteAddr: "172.18.0.5", Outcome: "granted"},
		{OccurredAt: time.Now(), RemoteAddr: "192.168.1.50", Outcome: "locked"},
	}}

	o, _ := captureOutput(t)
	if err := c.Exec(context.Background(), []string{"bootstrap", "status"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Recent attempts:", "172.18.0.5", "granted", "192.168.1.50", "locked"} {
		if !strings.Contains(o.String(), want) {
			t.Errorf("status is missing %q:\n%s", want, o.String())
		}
	}
}
