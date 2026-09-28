package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// command is one CLI command. run receives the full argument list, including
// the command name in args[0].
type command struct {
	names  []string // first is the primary name, the rest are aliases
	usages []usage
	run    func(c *CLI, ctx context.Context, args []string)
}

// usage is one way to call a command, shown by `help`.
type usage struct {
	forms []string // arguments after the command name; empty means none
	help  string
}

// commandTable lists every command in the order `help` shows them. Adding a
// command here makes it available at the prompt and in the help text.
func commandTable() []command {
	return []command{
		{
			names:  []string{"exit", "quit"},
			usages: []usage{{help: "Shuts down the program."}},
			run:    (*CLI).exit,
		},
		{
			names:  []string{"get", "g"},
			usages: []usage{{forms: []string{"<secret>"}, help: "Displays the value of the specified secret."}},
			run:    (*CLI).get,
		},
		{
			names:  []string{"create", "c"},
			usages: []usage{{forms: []string{"<secret> <value>"}, help: "Creates a new secret and value to the vault."}},
			run:    (*CLI).create,
		},
		{
			names:  []string{"delete", "d"},
			usages: []usage{{forms: []string{"<secret>"}, help: "Removes the specified secret from the vault."}},
			run:    (*CLI).delete,
		},
		{
			names:  []string{"update", "u"},
			usages: []usage{{forms: []string{"<secret> <new_value>"}, help: "Updates an existing secret in the vault."}},
			run:    (*CLI).update,
		},
		{
			names: []string{"list", "l"},
			usages: []usage{
				{help: "Lists all secrets in the public vault."},
				{forms: []string{"<term>"}, help: "Lists secrets whose keys start with <term>."},
				{forms: []string{"<term> fuzzy", "<term> f"}, help: "Lists secrets containing <term> (substring match)."},
			},
			run: (*CLI).list,
		},
		{
			names: []string{"bootstrap", "b"},
			usages: []usage{{
				forms: []string{"<clear|lock>"},
				help: "Enters or exits bootstrapping mode. This allows Lighthouse to obtain a\n" +
					"      one-time-use password without authenticating first.",
			}},
			run: (*CLI).bootstrapCmd,
		},
		{
			names:  []string{"help", "h"},
			usages: []usage{{help: "Displays this help information."}},
			run:    (*CLI).help,
		},
	}
}

func (c *CLI) help(ctx context.Context, args []string) {
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
}

func (c *CLI) exit(ctx context.Context, args []string) {
	fmt.Println("Shutting down Cove...")
	os.Exit(0)
}
