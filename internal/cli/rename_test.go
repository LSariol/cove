package cli

import (
	"context"
	"strings"
	"testing"
)

func TestRename(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	_ = v.Create(ctx, "old.key", "value", "test")
	_ = v.Create(ctx, "taken.key", "other", "test")

	if err := c.Exec(ctx, []string{"rename", "old.key", "new.key"}); err != nil {
		t.Fatal(err)
	}

	// Both names' history records the rename. (Checked before reading the
	// secret below, which adds a read event.)
	for key, want := range map[string]string{"old.key": "renamed to new.key", "new.key": "renamed from old.key"} {
		events, _ := v.History(ctx, key, 1)
		if len(events) != 1 || events[0].Detail != want {
			t.Errorf("history of %s = %+v, want %q", key, events, want)
		}
	}

	got, err := v.Show(ctx, "new.key", "test")
	if err != nil || got.Value != "value" {
		t.Fatalf("after rename: %+v, %v", got, err)
	}
	if exists(v, "old.key") {
		t.Error("old key still exists")
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"rename", "missing", "x"}, `No secret named "missing"`},
		{[]string{"rename", "new.key", "taken.key"}, "already exists"},
		{[]string{"rename", "new.key", "bad/key"}, "invalid character"},
	} {
		if err := c.Exec(ctx, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v = %v, want it to mention %q", tc.args, err, tc.want)
		}
	}
}
