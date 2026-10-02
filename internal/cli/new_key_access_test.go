package cli

import (
	"strings"
	"testing"
)

func TestCreateSaysWhoCanReadTheNewSecret(t *testing.T) {
	c, _ := newTokenCLI(t, "")

	// No project tokens yet: nothing to say.
	if _, e, _ := run(t, c, "create a.first x"); strings.Contains(e, "token") {
		t.Errorf("mentions tokens before any exist:\n%s", e)
	}

	run(t, c, "token create marquee --allow marquee.*")
	run(t, c, "token create botsuite --allow botsuite.* --allow shared.*")

	_, e, _ := run(t, c, "create shared.openai-key x")
	if !strings.Contains(e, "Readable by botsuite.") {
		t.Errorf("create a covered key:\n%s", e)
	}

	_, e, _ = run(t, c, "create other.key x")
	if !strings.Contains(e, `No project token can read "other.key" yet. To give one access: token allow other.key <project>`) {
		t.Errorf("create an uncovered key:\n%s", e)
	}

	_, e, _ = run(t, c, "generate marquee.session-key --yes")
	if !strings.Contains(e, "Readable by marquee.") {
		t.Errorf("generate:\n%s", e)
	}
}
