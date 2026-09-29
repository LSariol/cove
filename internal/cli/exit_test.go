package cli

import (
	"context"
	"testing"
)

func TestExitStopsCoveInsteadOfKillingIt(t *testing.T) {
	c := New(nil, nil)
	stopped := false
	c.stop = func() { stopped = true }

	c.exit(context.Background(), []string{"exit"})

	if !stopped {
		t.Fatal("exit didn't call stop")
	}
}
