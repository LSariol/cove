package cli

import (
	"strings"
	"testing"
)

func TestNamingWarning(t *testing.T) {
	c, _ := newTestCLI(t, "")

	for _, key := range []string{"BOTSUITE_TWITCH_CLIENT_ID", "SHARED_TMDB_API_KEY", "MARQUEE_DATABASE_URL"} {
		if _, e, err := run(t, c, "create "+key+" x"); err != nil || strings.Contains(e, "naming standard") {
			t.Errorf("create %s: %v, warned:\n%s", key, err, e)
		}
	}

	for _, line := range []string{"create botsuite.api-key x", "create my-key x", "create lower_case x", "create 1ST_KEY x", "generate twitch.token --yes"} {
		_, e, err := run(t, c, line)
		if err != nil {
			t.Fatalf("%q was refused: %v (it should only warn)", line, err)
		}
		if !strings.Contains(e, "doesn't follow the naming standard") {
			t.Errorf("%q: no warning:\n%s", line, e)
		}
	}

	_, e, _ := run(t, c, "rename botsuite.api-key BOTSUITE_NETFLIX_API_KEY")
	if strings.Contains(e, "naming standard") {
		t.Errorf("renaming into the standard warned:\n%s", e)
	}
	_, e, _ = run(t, c, "rename BOTSUITE_NETFLIX_API_KEY netflix.key")
	if !strings.Contains(e, "naming standard") {
		t.Errorf("renaming out of the standard didn't warn:\n%s", e)
	}
}
