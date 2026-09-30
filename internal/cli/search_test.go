package cli

import (
	"context"
	"strings"
	"testing"
)

func TestSearchAndListFilters(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	for _, key := range []string{"MYAPP_DB_URL", "OTHER_DB_URL", "MYAPP_TOKEN"} {
		_ = v.Create(ctx, key, "x", "test")
	}

	cases := []struct {
		args []string
		want []string
		not  []string
	}{
		{[]string{"search", "db_url"}, []string{"MYAPP_DB_URL", "OTHER_DB_URL"}, []string{"MYAPP_TOKEN"}},
		{[]string{"list", "myapp"}, []string{"MYAPP_DB_URL", "MYAPP_TOKEN"}, []string{"OTHER_DB_URL"}},
	}

	for _, tc := range cases {
		o, _ := captureOutput(t)
		if err := c.Exec(ctx, tc.args); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		for _, key := range tc.want {
			if !strings.Contains(o.String(), key) {
				t.Errorf("%v: output is missing %s", tc.args, key)
			}
		}
		for _, key := range tc.not {
			if strings.Contains(o.String(), key) {
				t.Errorf("%v: output shouldn't include %s", tc.args, key)
			}
		}
	}
}

func TestListTable(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	long := "A_VERY_LONG_SECRET_KEY_NAME_THAT_IS_WIDER_THAN_THIRTY_FIVE_CHARACTERS"
	_ = v.Create(ctx, long, "x", "test")
	_ = v.Create(ctx, "SHORT", "x", "test")

	o, e := captureOutput(t)
	if err := c.Exec(ctx, []string{"list"}); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(o.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "KEY") {
		t.Fatalf("table = %q", o.String())
	}
	// Columns line up: VERSION starts at the same position on every line.
	col := strings.Index(lines[0], "VERSION")
	if col <= len(long) || strings.Index(lines[1], " 1 ") != col-1 {
		t.Errorf("columns aren't aligned:\n%s", o.String())
	}
	if !strings.Contains(e.String(), "2 secrets") {
		t.Errorf("count = %q, want 2 secrets", e.String())
	}

	_, e = captureOutput(t)
	_ = c.Exec(ctx, []string{"list", "SHORT"})
	if !strings.Contains(e.String(), "1 of 2 secrets") {
		t.Errorf("filtered count = %q, want 1 of 2 secrets", e.String())
	}
}
