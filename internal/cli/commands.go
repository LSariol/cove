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
	names []string // first is the primary name, the rest are aliases

	// group and summary are what `help` shows: one line per command, under
	// its group's heading. synopsis is the arguments shown on that line.
	group    string
	synopsis string
	summary  string

	usages   []usage  // every way to call it, shown by `help <command>`
	examples []string // shown by `help <command>`

	run func(c *CLI, ctx context.Context, args []string) error

	// complete returns the Tab-completion candidates for the command's first
	// argument, or is nil when it has none.
	complete func(c *CLI) []string
}

// usage is one way to call a command, shown by `help <command>`.
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

// Command groups, in the order `help` shows them.
const (
	groupSecrets = "Secrets"
	groupFind    = "Finding and inspecting secrets"
	groupAccess  = "Project access"
	groupAdmin   = "Cove"
)

var groupOrder = []string{groupSecrets, groupFind, groupAccess, groupAdmin}

// commandTable lists every command. Adding a command here makes it available
// at the prompt and in the help text.
func commandTable(embedded bool) []command {
	exitHelp, exitSummary := "Leaves the shell.", "Leave the shell"
	if embedded {
		exitHelp, exitSummary = "Stops Cove, including the API server.", "Stop Cove, including the API server"
	}

	return []command{
		{
			names:    []string{"get", "g"},
			group:    groupSecrets,
			synopsis: "<key>",
			summary:  "Show a secret's value",
			usages:   []usage{{forms: []string{"<key>"}, help: "Shows the decrypted value of a secret."}},
			examples: []string{"get lighthouse.github-pat"},
			run:      (*CLI).get,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"create", "c"},
			group:    groupSecrets,
			synopsis: "<key> <value>",
			summary:  "Add a secret",
			usages: []usage{{forms: []string{"<key> <value>"}, help: "Creates a new secret. Afterwards it says which project tokens can read it,\n" +
				"      and how to give one access if none can."}},
			examples: []string{
				"create lighthouse.github-pat ghp_xxxx",
				"create shared.tmdb-api-key xxxx        then: token allow shared.tmdb-api-key botsuite marquee",
			},
			run: (*CLI).create,
		},
		{
			names:    []string{"update", "u"},
			group:    groupSecrets,
			synopsis: "<key> <value>",
			summary:  "Change a secret's value",
			usages:   []usage{{forms: []string{"<key> <value>"}, help: "Replaces a secret's value and increases its version."}},
			examples: []string{"update shared.tmdb-api-key new-value"},
			run:      (*CLI).update,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"generate"},
			group:    groupSecrets,
			synopsis: "<key> [length]",
			summary:  "Add or replace a secret with a random value",
			usages: []usage{{
				forms: []string{"<key> [length] [--yes]"},
				help: "Creates a secret with a random value (letters and digits, 32 characters\n" +
					"      unless given) and shows it. If the key exists, asks before replacing its value.",
			}},
			examples: []string{"generate marquee.session-key 64"},
			run:      (*CLI).generate,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"delete", "d"},
			group:    groupSecrets,
			synopsis: "<key>",
			summary:  "Delete a secret (can be restored)",
			usages:   []usage{{forms: []string{"<key> [--yes]"}, help: "Deletes a secret. Asks for confirmation unless --yes is given.\n      Warns which project tokens list the key."}},
			run:      (*CLI).delete,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"rename"},
			group:    groupSecrets,
			synopsis: "<key> <new-key>",
			summary:  "Rename a secret, keeping its history",
			usages: []usage{{forms: []string{"<key> <new-key> [--yes]"}, help: "Renames a secret, keeping its value, version and history.\n" +
				"      Apps using the old key stop finding it. Tokens that list the key by name\n" +
				"      are updated too (asks first unless --yes is given)."}},
			examples: []string{"rename shared.tmdb-api-key shared.tmdb-key"},
			run:      (*CLI).rename,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"restore"},
			group:    groupSecrets,
			synopsis: "<key> [version]",
			summary:  "Bring back an earlier or deleted value",
			usages: []usage{
				{forms: []string{"<key> [--yes]"}, help: "Brings back the value before the current one, or a deleted secret's last value."},
				{forms: []string{"<key> <version> [--yes]"}, help: "Brings back the value from that version (see \"history <key>\").\n" +
					"      The restored value is saved as a new version, so nothing is lost."},
			},
			run:      (*CLI).restore,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"list", "l"},
			group:    groupFind,
			synopsis: "[prefix]",
			summary:  "List secrets (never values)",
			usages: []usage{
				{help: "Lists every secret's name and details. Values are never shown."},
				{forms: []string{"<prefix>"}, help: "Lists secrets whose keys start with <prefix>."},
			},
			examples: []string{"list", "list marquee."},
			run:      (*CLI).list,
		},
		{
			names:    []string{"search", "s"},
			group:    groupFind,
			synopsis: "<text>",
			summary:  "List secrets whose keys contain <text>",
			usages:   []usage{{forms: []string{"<text>"}, help: "Lists secrets whose keys contain <text> (not case-sensitive)."}},
			run:      (*CLI).search,
		},
		{
			names:    []string{"info", "i"},
			group:    groupFind,
			synopsis: "<key>",
			summary:  "A secret's details, and which projects can read it",
			usages: []usage{{forms: []string{"<key>"}, help: "Shows a secret's details, when it was last read, and which project tokens\n" +
				"      can read it. Never shows the value."}},
			run:      (*CLI).info,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"history"},
			group:    groupFind,
			synopsis: "<key> [count]",
			summary:  "A secret's recent events, and by which project",
			usages:   []usage{{forms: []string{"<key> [count]"}, help: "Shows a secret's recent events (default 20): created, read, updated, deleted,\n      and by which app. Works for deleted secrets too. Never shows values."}},
			run:      (*CLI).history,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"token", "t"},
			group:    groupAccess,
			synopsis: "<action> ...",
			summary:  "Per-project access: create, show, allow, deny, rotate, revoke",
			usages: []usage{
				{forms: []string{"list", ""}, help: "Lists the per-project tokens and what each can reach."},
				{forms: []string{"create <name> [--allow <pattern>]... [--write <pattern>]..."}, help: "Creates a token for a project and shows it once. --allow lets it read\n" +
					"      matching secrets; --write also lets it create, update and delete them.\n" +
					"      (See \"help patterns\".)"},
				{forms: []string{"show <name>"}, help: "Shows a token's patterns, the secrets it can reach now, and recent changes."},
				{forms: []string{"allow <pattern> <name>... [--write]"}, help: "Lets one or more tokens read (or, with --write, change) a key or pattern."},
				{forms: []string{"deny <pattern> <name>..."}, help: "Removes a key or pattern from one or more tokens."},
				{forms: []string{"rotate <name> [--yes]"}, help: "Gives a token a new value with the same access. The old one stops working."},
				{forms: []string{"revoke <name> [--yes]"}, help: "Deletes a token. The project can't reach Cove until it gets a new one."},
			},
			examples: []string{
				"token create lighthouse --allow lighthouse.*",
				"token create botsuite --allow botsuite.* --allow shared.tmdb-api-key",
				"token allow shared.openai-key botsuite marquee     share a key with two projects",
				"token allow botsuite.cache.* botsuite --write      let botsuite change its cache keys",
				"token deny shared.openai-key marquee",
				"token show marquee                                 what can marquee reach?",
			},
			run:      (*CLI).tokenCmd,
			complete: tokenCompletions,
		},
		{
			names:    []string{"bootstrap", "b"},
			group:    groupAccess,
			synopsis: "open|lock|status",
			summary:  "Let a new client fetch its token once",
			usages: []usage{
				{forms: []string{"open <project> [duration]"}, help: "Opens the bootstrap endpoint for 10 minutes (or the given duration, e.g. 30m)\n" +
					"      to hand out a new token for that project, so a new client (e.g. Lighthouse)\n" +
					"      can fetch it without credentials. The project's current token stops working.\n" +
					"      It closes after one successful handout."},
				{forms: []string{"open [duration]"}, help: "The same, but hands out the master token (COVE_CLIENT_SECRET), which can\n" +
					"      reach every secret. (Also: bootstrap clear.)"},
				{forms: []string{"lock"}, help: "Closes the bootstrap endpoint now."},
				{forms: []string{"status", ""}, help: "Shows whether it's open, which token it hands out, the last handout, and\n" +
					"      which addresses may use it."},
			},
			examples: []string{"bootstrap open lighthouse", "bootstrap status"},
			run:      (*CLI).bootstrapCmd,
			complete: func(*CLI) []string { return []string{"lock", "open", "status"} },
		},
		{
			names:   []string{"status"},
			group:   groupAdmin,
			summary: "Is Cove healthy?",
			usages:  []usage{{help: "Shows whether Cove is healthy: version, environment, database, schema,\n      number of secrets, and whether the bootstrap endpoint is open."}},
			run:     (*CLI).status,
		},
		{
			names:    []string{"help", "h"},
			group:    groupAdmin,
			synopsis: "[command|guide]",
			summary:  "This overview, one command in detail, or a guide",
			usages:   []usage{{forms: []string{"[command]", "setup", "patterns"}, help: "Shows every command, or one in detail with examples, or a step-by-step guide."}},
			run:      (*CLI).help,
			complete: (*CLI).helpTopics,
		},
		{
			names:   []string{"exit", "quit"},
			group:   groupAdmin,
			summary: exitSummary,
			usages:  []usage{{help: exitHelp}},
			run:     (*CLI).exit,
		},
	}
}

// guides are the step-by-step help topics: `help setup`, `help patterns`.
var guides = []struct {
	name, summary, text string
}{
	{"setup", "Set up a new project, step by step", setupGuide},
	{"patterns", "How token patterns (--allow, --write) match keys", patternsGuide},
}

const setupGuide = `Setting up a new project (e.g. "marquee")

1. Add its secrets. Start each key with the project's name, so one pattern
   covers them all:
     create marquee.db-url postgres://...
     generate marquee.session-key 64

2. Create its token, allowing its own keys and any shared ones it needs:
     token create marquee --allow marquee.* --allow shared.tmdb-api-key
   Copy the token it prints; it's shown only once.

3. Check what it can reach (a warning means a pattern matches nothing):
     token show marquee

4. Give the token to the project, in one of two ways:
   - Put it where the project reads its Cove token (e.g. its .env), or
   - If it uses CoveClient's LoadOrBootstrap: bootstrap open marquee, then
     start the project; it fetches and saves its token by itself.

5. Check it's working: "token list" shows when it was last used, and
   "history marquee.db-url" shows marquee as the reader.

Later:
  A new secret for it:        create marquee.new-key value   (marquee.* covers it)
  Share a key with it:        token allow shared.openai-key marquee
  Take a key away:            token deny shared.openai-key marquee
  Token leaked or lost:       token rotate marquee  (or: token revoke marquee)
  Who can read a key?         info shared.tmdb-api-key`

const patternsGuide = `How token patterns match keys

A token can only reach the secrets its patterns cover. Nothing else is
reachable, and a key named "shared.x" isn't special: it must be allowed.

  Pattern              Matches
  marquee.*            every key starting with "marquee." (marquee.db-url, ...)
  shared.tmdb-api-key  exactly that key
  *                    every key (like the master token, but revocable)

Keys are case-sensitive. "*" only works at the end of a pattern.

--allow <pattern>   the project can read matching secrets.
--write <pattern>   the project can also create, update and delete them.
                    Without --write, a project can't change a shared key
                    and break another project.

If a project asks for a key its token doesn't cover, it gets
"403 forbidden_key: marquee's token can't read botsuite.db-url".
Fix it with: token allow <key> <project>

Renaming a key that tokens list by name offers to update them. Deleting one
warns which projects will lose it.`

// help shows the overview, one command in detail, or a guide.
func (c *CLI) help(ctx context.Context, args []string) error {
	switch len(args) {
	case 1:
		out(c.overview())
		return nil
	case 2:
		topic := strings.ToLower(args[1])
		for _, g := range guides {
			if g.name == topic {
				out(g.text)
				return nil
			}
		}
		cmd, ok := c.byName[topic]
		if !ok {
			return usageError{reason: fmt.Sprintf("Unknown command or guide %q.", args[1]), form: "help [command|setup|patterns]"}
		}
		out(commandHelp(*cmd))
		return nil
	default:
		return usageError{form: "help [command|setup|patterns]"}
	}
}

// overview lists every command on one line, by group, then the guides.
func (c *CLI) overview() string {
	var b strings.Builder

	type line struct{ left, right string }
	var groups [][]line
	width := 0
	for _, group := range groupOrder {
		var lines []line
		for _, cmd := range c.commands {
			if cmd.group != group {
				continue
			}
			left := strings.TrimSpace(cmd.names[0] + " " + cmd.synopsis)
			lines = append(lines, line{left, cmd.summary})
			width = max(width, len(left))
		}
		groups = append(groups, lines)
	}
	for _, g := range guides {
		width = max(width, len("help "+g.name))
	}

	for i, group := range groupOrder {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(group + ":\n")
		for _, l := range groups[i] {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, l.left, l.right)
		}
	}

	b.WriteString("\nGuides:\n")
	for _, g := range guides {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, "help "+g.name, g.summary)
	}

	b.WriteString("\nType \"help <command>\" for details and examples, e.g. \"help token\".")
	return b.String()
}

// commandHelp is `help <command>`: every way to call it, then examples.
func commandHelp(cmd command) string {
	var b strings.Builder
	names := strings.Join(cmd.names, ", ")

	for i, u := range cmd.usages {
		if i > 0 {
			b.WriteString("\n")
		}
		forms := u.forms
		if len(forms) == 0 {
			forms = []string{""}
		}
		for _, form := range forms {
			fmt.Fprintf(&b, "  %s\n", strings.TrimSpace(names+" "+form))
		}
		fmt.Fprintf(&b, "      %s\n", u.help)
	}

	if len(cmd.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, e := range cmd.examples {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// helpTopics completes `help <Tab>`: command names and guides.
func (c *CLI) helpTopics() []string {
	topics := c.commandNames()
	for _, g := range guides {
		topics = append(topics, g.name)
	}
	return topics
}

func (c *CLI) exit(ctx context.Context, args []string) error {
	if c.embedded {
		info("Shutting down Cove...")
	}
	if c.stop != nil {
		c.stop()
	}
	return nil
}
