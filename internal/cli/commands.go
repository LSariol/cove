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

	// What `help <command>` shows, each as aligned columns.
	usages   []usage
	flags    []flag
	examples []example

	run func(c *CLI, ctx context.Context, args []string) error

	// complete returns the Tab-completion candidates for the command's first
	// argument, or is nil when it has none.
	complete func(c *CLI) []string
}

// usage is one way to call a command: the full form, e.g. "get <key>", and
// what it does.
type usage struct {
	form string
	help string
}

// flag is an option a command takes, e.g. "--yes".
type flag struct {
	name string
	help string
}

// example is a command line and a short note on what it does.
type example struct {
	line string
	note string
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
	exitHelp := "Leave the shell. Cove keeps running."
	if embedded {
		exitHelp = "Stop Cove, including the API server."
	}
	yes := flag{"--yes", "Don't ask for confirmation (for scripts)."}

	return []command{
		{
			names:    []string{"get", "g"},
			group:    groupSecrets,
			synopsis: "<key>",
			summary:  "Show a secret's value",
			usages: []usage{
				{"get <key>", "Show a secret's decrypted value. It's logged, but not counted as an app read."},
			},
			examples: []example{
				{"get LIGHTHOUSE_GITHUB_TOKEN", "print the value"},
			},
			run:      (*CLI).get,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"create", "c"},
			group:    groupSecrets,
			synopsis: "<key> <value>",
			summary:  "Add a secret",
			usages: []usage{
				{"create <key> <value>", "Create a secret. Then it says which project tokens can read it, or how to give one access."},
			},
			examples: []example{
				{"create MARQUEE_TMDB_API_KEY abc123", "a project's own key"},
				{"create SHARED_TMDB_API_KEY abc123", "a shared key (then: token allow)"},
			},
			run: (*CLI).create,
		},
		{
			names:    []string{"update", "u"},
			group:    groupSecrets,
			synopsis: "<key> <value>",
			summary:  "Change a secret's value",
			usages: []usage{
				{"update <key> <value>", "Replace a secret's value; its version goes up by one. Redeploy the projects that use it."},
			},
			examples: []example{
				{"update SHARED_TMDB_API_KEY newvalue", "change the value"},
			},
			run:      (*CLI).update,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"generate"},
			group:    groupSecrets,
			synopsis: "<key> [length]",
			summary:  "Add or replace a secret with a random value",
			usages: []usage{
				{"generate <key> [length]", "Create a secret with a random value (letters and digits; 32 characters, or 16 to 256) and show it once. If the key exists, ask before replacing its value."},
			},
			flags: []flag{yes},
			examples: []example{
				{"generate MARQUEE_SESSION_SECRET", "32 random characters"},
				{"generate MARQUEE_SESSION_SECRET 64", "64 random characters"},
			},
			run:      (*CLI).generate,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"delete", "d"},
			group:    groupSecrets,
			synopsis: "<key>",
			summary:  "Delete a secret (can be restored)",
			usages: []usage{
				{"delete <key>", "Delete a secret; restore brings it back. Asks first, and warns which project tokens list the key."},
			},
			flags: []flag{yes},
			examples: []example{
				{"delete MARQUEE_OLD_API_KEY", "asks first"},
				{"delete MARQUEE_OLD_API_KEY --yes", "no question"},
			},
			run:      (*CLI).delete,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"rename"},
			group:    groupSecrets,
			synopsis: "<key> <new-key>",
			summary:  "Rename a secret, keeping its history",
			usages: []usage{
				{"rename <key> <new-key>", "Rename a secret, keeping its value, version and history. Projects asking for the old name stop finding it. Offers to update tokens that list the key."},
			},
			flags: []flag{{"--yes", "Update those tokens without asking."}},
			examples: []example{
				{"rename tmdb.api-key SHARED_TMDB_API_KEY", "move a key to the naming standard"},
			},
			run:      (*CLI).rename,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"restore"},
			group:    groupSecrets,
			synopsis: "<key> [version]",
			summary:  "Bring back an earlier or deleted value",
			usages: []usage{
				{"restore <key>", "Bring back the value before the current one, or a deleted secret's last value."},
				{"restore <key> <version>", "Bring back the value from that version (history shows them). Either way it's saved as a new version, so nothing is lost."},
			},
			flags: []flag{{"--yes", "Replace the current value without asking."}},
			examples: []example{
				{"restore MARQUEE_TMDB_API_KEY", "undo the last change"},
				{"restore MARQUEE_TMDB_API_KEY 2", "go back to version 2"},
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
				{"list", "List every secret with its version, reads and dates. Never shows values."},
				{"list <prefix>", "Only keys starting with <prefix> (not case-sensitive)."},
			},
			examples: []example{
				{"list", "everything"},
				{"list MARQUEE_", "one project's keys"},
			},
			run: (*CLI).list,
		},
		{
			names:    []string{"search", "s"},
			group:    groupFind,
			synopsis: "<text>",
			summary:  "List secrets whose keys contain <text>",
			usages: []usage{
				{"search <text>", "List secrets whose keys contain <text> (not case-sensitive)."},
			},
			examples: []example{
				{"search TWITCH", "every Twitch key, in any project"},
			},
			run: (*CLI).search,
		},
		{
			names:    []string{"info", "i"},
			group:    groupFind,
			synopsis: "<key>",
			summary:  "A secret's details, and which projects can read it",
			usages: []usage{
				{"info <key>", "Show a secret's details, when and by whom it was last read, and which project tokens can read it. Never shows the value."},
			},
			examples: []example{
				{"info SHARED_TMDB_API_KEY", "who can read a shared key"},
			},
			run:      (*CLI).info,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"history"},
			group:    groupFind,
			synopsis: "<key> [count]",
			summary:  "A secret's recent events, and by which project",
			usages: []usage{
				{"history <key> [count]", "Show a secret's recent events (20 unless given): created, read, updated, deleted, renamed, and by whom. Works for deleted secrets too. Never shows values."},
			},
			examples: []example{
				{"history MARQUEE_DATABASE_URL", "the last 20 events"},
				{"history MARQUEE_DATABASE_URL 100", "the last 100"},
			},
			run:      (*CLI).history,
			complete: (*CLI).keyNames,
		},
		{
			names:    []string{"token", "t"},
			group:    groupAccess,
			synopsis: "<action> ...",
			summary:  "Per-project access: create, show, allow, deny, rotate, revoke",
			usages: []usage{
				{"token [list]", "List the project tokens and what each can reach."},
				{"token create <name>", "Create a token for a project and print it; it's shown only once. Give it access with --allow and --write (see \"help patterns\")."},
				{"token show <name>", "Show a token's patterns, the secrets it can reach now, and its recent changes."},
				{"token allow <pattern> <name>...", "Let one or more tokens read a key or pattern (with --write, also change it)."},
				{"token deny <pattern> <name>...", "Remove a key or pattern from one or more tokens."},
				{"token rotate <name>", "Give a token a new value with the same access. The old one stops working."},
				{"token revoke <name>", "Delete a token. The project can't reach Cove until it gets a new one."},
			},
			flags: []flag{
				{"--allow <pattern>", "create: let the token read matching secrets. Repeatable."},
				{"--write <pattern>", "create: also let it create, update and delete them. Repeatable."},
				{"--write", "allow: grant change access, not just read."},
				{"--yes", "rotate, revoke: don't ask for confirmation."},
			},
			examples: []example{
				{"token create marquee --allow 'MARQUEE_*'", "a project that reads its own keys"},
				{"token create lighthouse --allow '*'", "Lighthouse: read-only, every key"},
				{"token allow BOTSUITE_TWITCH_ACCESS_TOKEN botsuite --write", "botsuite may update its Twitch key"},
				{"token allow SHARED_OPENAI_API_KEY botsuite marquee", "share a key with two projects"},
				{"token deny SHARED_OPENAI_API_KEY marquee", "take it away from one"},
				{"token show marquee", "what can marquee reach?"},
				{"token rotate marquee", "a new token after a leak"},
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
				{"bootstrap open <project> [duration]", "Hand out a new token for that project, once: open for 10 minutes (or a duration such as 30m, up to 24h). The project's current token stops working."},
				{"bootstrap lock", "Close it now."},
				{"bootstrap [status]", "Show whether it's open, which token it hands out, the last handout, recent attempts, and which addresses may use it."},
			},
			examples: []example{
				{"bootstrap open lighthouse", "then start Lighthouse (10 minutes)"},
				{"bootstrap open lighthouse 30m", "a longer window"},
				{"bootstrap status", "check it was handed out"},
			},
			run:      (*CLI).bootstrapCmd,
			complete: func(*CLI) []string { return []string{"lock", "open", "status"} },
		},
		{
			names:   []string{"status"},
			group:   groupAdmin,
			summary: "Is Cove healthy?",
			usages: []usage{
				{"status", "Show whether Cove is healthy: version, environment, database, schema, secrets, vault key, and the bootstrap endpoint. Exits non-zero if something needs attention."},
			},
			run: (*CLI).status,
		},
		{
			names:    []string{"help", "h"},
			group:    groupAdmin,
			synopsis: "[command|guide]",
			summary:  "This overview, one command in detail, or a guide",
			usages: []usage{
				{"help", "List every command."},
				{"help <command>", "Show one command in detail, with examples."},
				{"help setup", "A step-by-step guide to setting up a new project."},
				{"help patterns", "How token patterns (--allow, --write) match keys."},
			},
			examples: []example{
				{"help token", "everything about project tokens"},
			},
			run:      (*CLI).help,
			complete: (*CLI).helpTopics,
		},
		{
			names:   []string{"exit", "quit"},
			group:   groupAdmin,
			summary: strings.TrimSuffix(strings.SplitN(exitHelp, ".", 2)[0], "."),
			usages: []usage{
				{"exit", exitHelp},
			},
			run: (*CLI).exit,
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

The standard: Lighthouse hands a project its secrets when it deploys it. The
project only reads environment variables; it has no Cove code or token.

1. Add its secrets, named PROJECT_PLATFORM_TYPE (shared ones SHARED_...):
     create MARQUEE_DATABASE_URL postgres://...
     create MARQUEE_TWITCH_CLIENT_ID abc123
     generate MARQUEE_SESSION_SECRET 64
   TYPE is one of: API_KEY, CLIENT_ID, CLIENT_SECRET, ACCESS_TOKEN,
   REFRESH_TOKEN, TOKEN, URL, PASSWORD, SECRET, ID. An optional role can
   go before it: MARQUEE_DATABASE_MIGRATOR_URL. Capitals, digits and _
   only, so the name works as a ${...} variable in a compose file.

2. In the project's docker-compose.yml, refer to each secret as ${KEY}:
     environment:
       - DATABASE_URL=${MARQUEE_DATABASE_URL}
       - TMDB_API_KEY=${SHARED_TMDB_API_KEY}
   Lighthouse fetches every ${...} name from Cove on deploy and passes it
   to docker compose, which fills them in. The project reads os.Getenv(...).

3. Deploy it with Lighthouse. Check: "history MARQUEE_DATABASE_URL" shows
   lighthouse as the reader.

Only if the project must change secrets itself (e.g. botsuite refreshing
its Twitch tokens), it also gets its own token:

4. Create it, with --write only on the keys it updates:
     token create marquee --allow MARQUEE_* --write MARQUEE_TWITCH_ACCESS_TOKEN
   Store the printed token (shown once) for Lighthouse to hand over:
     create MARQUEE_COVE_TOKEN <the token>

5. Add to its compose file, and use CoveClient with these two values
   (COVE_URL isn't a secret, so it's written out):
       - COVE_URL=http://cove:2100
       - COVE_TOKEN=${MARQUEE_COVE_TOKEN}

6. Check: "token show marquee" (what it can reach), "token list" (last used).

Later:
  A new secret for it:        create it, add ${KEY} to its compose, redeploy
  An old key, other naming:   rename old.name MARQUEE_..., update ${...}, redeploy
  Changed a secret's value:   redeploy it with Lighthouse
  Token leaked or lost:       token rotate marquee, update MARQUEE_COVE_TOKEN
  Who can read a key?         info SHARED_TMDB_API_KEY

Lighthouse itself: token create lighthouse --allow * (read-only: it can
deploy everything but change nothing), then bootstrap open lighthouse.`

const patternsGuide = `How token patterns match keys

A token can only reach the secrets its patterns cover. Nothing else is
reachable, and a key starting SHARED_ isn't special: it must be allowed.

  Pattern              Matches
  MARQUEE_*            every key starting with "MARQUEE_" (MARQUEE_DATABASE_URL, ...)
  SHARED_TMDB_API_KEY  exactly that key
  *                    every key (like the master token, but revocable)

Keys are case-sensitive. "*" only works at the end of a pattern.

--allow <pattern>   the project can read matching secrets.
--write <pattern>   the project can also create, update and delete them.
                    Without --write, a project can't change a shared key
                    and break another project.

If a project asks for a key its token doesn't cover, it gets
"403 forbidden_key: marquee's token can't read BOTSUITE_DATABASE_URL".
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

// helpWidth is the widest line help text wraps to.
const helpWidth = 80

// commandHelp is `help <command>`: a header, then usage, flags and examples.
// Usage and flags share one description column; examples have their own.
func commandHelp(cmd command) string {
	var b strings.Builder

	header := cmd.names[0]
	if len(cmd.names) > 1 {
		header += " (" + strings.Join(cmd.names[1:], ", ") + ")"
	}
	b.WriteString(header + ": " + cmd.summary + "\n")

	var usages, flags, examples [][2]string
	for _, u := range cmd.usages {
		usages = append(usages, [2]string{u.form, u.help})
	}
	for _, f := range cmd.flags {
		flags = append(flags, [2]string{f.name, f.help})
	}
	for _, e := range cmd.examples {
		examples = append(examples, [2]string{e.line, e.note})
	}

	width := columnWidth(append(append([][2]string{}, usages...), flags...), 36)
	b.WriteString("\nUsage:\n" + columns(usages, width))
	if len(flags) > 0 {
		b.WriteString("\nFlags:\n" + columns(flags, width))
	}
	if len(examples) > 0 {
		b.WriteString("\nExamples:\n" + columns(examples, columnWidth(examples, 42)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// columnWidth is the width of the left column for rows: the longest left
// side, not counting those longer than maxLeft (they get their own line).
func columnWidth(rows [][2]string, maxLeft int) int {
	width := 0
	for _, r := range rows {
		if len(r[0]) <= maxLeft {
			width = max(width, len(r[0]))
		}
	}
	return width
}

// columns lays out rows as two columns: the left one indented by two spaces
// and width wide, the right one wrapped to helpWidth. A left side wider than
// width gets its own line, with its text below it in the right column.
func columns(rows [][2]string, width int) string {
	col := 2 + width + 3
	pad := strings.Repeat(" ", col)

	var b strings.Builder
	for _, r := range rows {
		lines := wrap(r[1], helpWidth-col)
		if len(r[0]) > width {
			b.WriteString("  " + r[0] + "\n")
			for _, l := range lines {
				b.WriteString(pad + l + "\n")
			}
			continue
		}
		for i, l := range lines {
			left := ""
			if i == 0 {
				left = r[0]
			}
			fmt.Fprintf(&b, "  %-*s   %s\n", width, left, l)
		}
	}
	return b.String()
}

// wrap splits text into lines of at most width characters, at spaces.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
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
