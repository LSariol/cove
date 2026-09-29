package cli

import (
	"context"
	"fmt"
	"strings"
)

// command is one CLI command. run receives the full argument list, including
// the command name in args[0]. It prints its own results and returns an error
// for anything that went wrong; the caller shows the error.
type command struct {
	names  []string // first is the primary name, the rest are aliases
	usages []usage
	run    func(c *CLI, ctx context.Context, args []string) error
}

// usage is one way to call a command, shown by `help`.
type usage struct {
	forms []string // arguments after the command name; empty means none
	help  string
}

// usageError is returned when a command is called with the wrong arguments.
type usageError struct {
	reason string // optional, e.g. `Unknown list option "x".`
	form   string
}

func (e usageError) Error() string {
	if e.reason == "" {
		return "Usage: " + e.form
	}
	return e.reason + " Usage: " + e.form
}

// commandTable lists every command in the order `help` shows them. Adding a
// command here makes it available at the prompt and in the help text.
func commandTable(embedded bool) []command {
	exitHelp := "Leaves the shell."
	if embedded {
		exitHelp = "Stops Cove, including the API server."
	}

	return []command{
		{
			names:  []string{"exit", "quit"},
			usages: []usage{{help: exitHelp}},
			run:    (*CLI).exit,
		},
		{
			names:  []string{"get", "g"},
			usages: []usage{{forms: []string{"<key>"}, help: "Shows the decrypted value of a secret."}},
			run:    (*CLI).get,
		},
		{
			names:  []string{"create", "c"},
			usages: []usage{{forms: []string{"<key> <value>"}, help: "Creates a new secret."}},
			run:    (*CLI).create,
		},
		{
			names:  []string{"delete", "d"},
			usages: []usage{{forms: []string{"<key>"}, help: "Deletes a secret (asks for confirmation)."}},
			run:    (*CLI).delete,
		},
		{
			names:  []string{"update", "u"},
			usages: []usage{{forms: []string{"<key> <value>"}, help: "Replaces a secret's value and increases its version."}},
			run:    (*CLI).update,
		},
		{
			names: []string{"list", "l"},
			usages: []usage{
				{help: "Lists every secret's name and details. Values are never shown."},
				{forms: []string{"<prefix>"}, help: "Lists secrets whose keys start with <prefix>."},
				{forms: []string{"<text> fuzzy", "<text> f"}, help: "Lists secrets whose keys contain <text>."},
			},
			run: (*CLI).list,
		},
		{
			names: []string{"bootstrap", "b"},
			usages: []usage{
				{forms: []string{"clear"}, help: "Opens the one-time bootstrap endpoint, so a new client (e.g. Lighthouse)\n" +
					"      can fetch the client token without credentials. It locks again after one use."},
				{forms: []string{"lock"}, help: "Locks the bootstrap endpoint without it being used."},
			},
			run: (*CLI).bootstrapCmd,
		},
		{
			names:  []string{"help", "h"},
			usages: []usage{{help: "Shows this help."}},
			run:    (*CLI).help,
		},
	}
}

func (c *CLI) help(ctx context.Context, args []string) error {
	var b strings.Builder
	b.WriteString("Available Commands:\n")

	for _, cmd := range c.commands {
		names := strings.Join(cmd.names, ", ")

		for _, u := range cmd.usages {
			forms := u.forms
			if len(forms) == 0 {
				forms = []string{""}
			}

			b.WriteString("\n")
			for _, form := range forms {
				fmt.Fprintf(&b, "  %s\n", strings.TrimSpace(names+" "+form))
			}
			fmt.Fprintf(&b, "      %s\n", u.help)
		}
	}

	infoLog("\n" + strings.TrimRight(b.String(), "\n"))
	return nil
}

func (c *CLI) exit(ctx context.Context, args []string) error {
	if c.embedded {
		fmt.Println("Shutting down Cove...")
	}
	if c.stop != nil {
		c.stop()
	}
	return nil
}
