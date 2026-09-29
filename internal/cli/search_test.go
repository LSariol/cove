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
		{[]string{"list", "db", "fuzzy"}, []string{"MYAPP_DB_URL", "OTHER_DB_URL"}, []string{"MYAPP_TOKEN"}}, // older form still works
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
