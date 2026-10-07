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
	for _, want := range []string{"delete (d): Delete a secret", "Usage:", "  delete <key>   ", "Flags:", "  --yes          ", "Examples:"} {
		if !strings.Contains(o, want) {
			t.Errorf("help delete is missing %q:\n%s", want, o)
		}
	}
	if strings.Contains(o, "get <key>") {
		t.Errorf("help delete shows other commands:\n%s", o)
	}

	o, _ = helpOutput(t, c, "token")
	for _, want := range []string{"token (t): Per-project access", "token create <name>", "--allow <pattern>", "token allow SHARED_OPENAI_API_KEY botsuite marquee", `"help patterns"`} {
		if !strings.Contains(o, want) {
			t.Errorf("help token is missing %q:\n%s", want, o)
		}
	}

	if o2, _ := helpOutput(t, c, "t"); o2 != o {
		t.Error(`"help t" differs from "help token"`)
	}

	if _, err := helpOutput(t, c, "nope"); err == nil {
		t.Error("help for an unknown command returned no error")
	}
}

// Every help page fits in 80 columns, descriptions in a block start in the
// same column, and example notes never wrap onto a second line.
func TestHelpLayout(t *testing.T) {
	c, _ := newTestCLI(t, "")
	for _, cmd := range c.commands {
		o, err := helpOutput(t, c, cmd.names[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(o, "\n") {
			if len([]rune(line)) > helpWidth {
				t.Errorf("help %s: line longer than %d:\n%s", cmd.names[0], helpWidth, line)
			}
		}
		for _, e := range cmd.examples {
			if !strings.Contains(o, e.note) {
				t.Errorf("help %s: the note %q wraps onto another line:\n%s", cmd.names[0], e.note, o)
			}
		}
		// Usage and flag descriptions start in one column.
		usageAndFlags, _, _ := strings.Cut(o, "\nExamples:")
		starts := map[int]bool{}
		for _, u := range append(append([]usage{}, cmd.usages...), flagsAsUsages(cmd.flags)...) {
			for _, line := range strings.Split(usageAndFlags, "\n") {
				if strings.HasPrefix(line, "  "+u.form+"   ") { // a description is at least 3 spaces after its form
					starts[len(line)-len(strings.TrimLeft(line[2+len(u.form):], " "))] = true
				}
			}
		}
		if len(starts) > 1 {
			t.Errorf("help %s: usage and flag descriptions start in %d different columns:\n%s", cmd.names[0], len(starts), o)
		}
	}
}

func flagsAsUsages(flags []flag) []usage {
	var us []usage
	for _, f := range flags {
		us = append(us, usage{form: f.name, help: f.help})
	}
	return us
}

func TestHelpGuides(t *testing.T) {
	c, _ := newTestCLI(t, "")

	o, err := helpOutput(t, c, "setup")
	for _, want := range []string{"DATABASE_URL=${MARQUEE_DATABASE_URL}", "COVE_TOKEN=${MARQUEE_COVE_TOKEN}", "--write MARQUEE_TWITCH_ACCESS_TOKEN", "PROJECT_PLATFORM_TYPE"} {
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
