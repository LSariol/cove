package cli

import (
	"context"
	"strings"
	"testing"
)

func helpOutput(t *testing.T, c *CLI, args ...string) (string, error) {
	t.Helper()
	o, _ := captureOutput(t)
	err := c.Exec(context.Background(), append([]string{"help"}, args...))
	return o.String(), err
}

func TestHelpOverview(t *testing.T) {
	c, _ := newTestCLI(t, "")
	o, err := helpOutput(t, c)
	if err != nil {
		t.Fatal(err)
	}

	// Grouped, one line per command, then the guides.
	for _, want := range []string{
		"Secrets:", "Finding and inspecting secrets:", "Project access:", "Cove:",
		"create <key> <value>", "Add a secret",
		"token <action> ...",
		"Guides:", "help setup", "help patterns",
		`"help <command>" for details and examples`,
	} {
		if !strings.Contains(o, want) {
			t.Errorf("overview is missing %q:\n%s", want, o)
		}
	}

	// Every command is listed.
	for _, cmd := range c.commands {
		if !strings.Contains(o, "  "+cmd.names[0]) {
			t.Errorf("overview doesn't list %q", cmd.names[0])
		}
	}

	// Short enough to read at a glance.
	if n := strings.Count(o, "\n"); n > 40 {
		t.Errorf("overview is %d lines", n)
	}
}

func TestHelpForOneCommand(t *testing.T) {
	c, _ := newTestCLI(t, "")

	o, err := helpOutput(t, c, "delete")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o, "delete, d <key> [--yes]") || strings.Contains(o, "get, g") {
		t.Errorf("help delete = %q", o)
	}

	o, _ = helpOutput(t, c, "token")
	for _, want := range []string{"token, t create <name>", "Examples:", "token allow shared.openai-key botsuite marquee", `"help patterns"`} {
		if !strings.Contains(o, want) {
			t.Errorf("help token is missing %q:\n%s", want, o)
		}
	}

	// Aliases work too.
	if o2, _ := helpOutput(t, c, "t"); o2 != o {
		t.Error(`"help t" differs from "help token"`)
	}

	if _, err := helpOutput(t, c, "nope"); err == nil {
		t.Error("help for an unknown command returned no error")
	}
}

func TestHelpGuides(t *testing.T) {
	c, _ := newTestCLI(t, "")

	o, err := helpOutput(t, c, "setup")
	for _, want := range []string{"DATABASE_URL={marquee.db-url}", "COVE_TOKEN={lighthouse.token.marquee}", "--write marquee.oauth-token"} {
		if err != nil || !strings.Contains(o, want) {
			t.Errorf("help setup is missing %q (%v):\n%s", want, err, o)
		}
	}

	o, err = helpOutput(t, c, "patterns")
	if err != nil || !strings.Contains(o, "--write <pattern>") || !strings.Contains(o, "forbidden_key") {
		t.Fatalf("help patterns = %q, %v", o, err)
	}
}

func TestHelpCompletesGuides(t *testing.T) {
	c, _ := newTestCLI(t, "")
	topics := strings.Join(c.helpTopics(), " ")
	for _, want := range []string{"token", "setup", "patterns"} {
		if !strings.Contains(topics, want) {
			t.Errorf("help completion is missing %q: %s", want, topics)
		}
	}
}
