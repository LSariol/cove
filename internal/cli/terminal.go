package cli

import (
	"context"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

// runTerminal runs the prompt with line editing: arrow keys, history (up/down),
// and Tab completion of command names and secret keys. It returns false if
// stdin can't be switched to raw mode, so the caller falls back to plain input.
// Ctrl+D or Ctrl+C on the prompt leaves the shell.
func (c *CLI) runTerminal(ctx context.Context) bool {
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return false
	}
	defer term.Restore(fd, oldState)

	c.term = term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stderr}, c.prompt)
	c.term.AutoCompleteCallback = c.complete

	// In raw mode, output has to go through the terminal, which also turns
	// "\n" into the "\r\n" a raw terminal needs.
	oldOut, oldErr := stdout, stderr
	stdout, stderr = c.term, c.term
	defer func() { stdout, stderr = oldOut, oldErr }()

	for ctx.Err() == nil {
		line, err := c.term.ReadLine()
		if err != nil {
			return true
		}

		args := strings.Fields(line)
		if len(args) == 0 {
			continue
		}
		report(c.Exec(ctx, args))
	}
	return true
}

// complete handles Tab. The first word completes to a command name; the word
// after a command completes to whatever that command takes (secret keys,
// bootstrap options, command names for help). With one match the word is
// completed; with several, their common prefix is completed, and pressing Tab
// again lists them.
func (c *CLI) complete(line string, pos int, key rune) (string, int, bool) {
	if key != '\t' {
		return "", 0, false
	}

	head, tail := line[:pos], line[pos:]
	start := strings.LastIndex(head, " ") + 1
	word := head[start:]
	before := strings.Fields(head[:start])

	var candidates []string
	switch len(before) {
	case 0:
		candidates = c.commandNames()
	case 1:
		cmd, ok := c.byName[strings.ToLower(before[0])]
		if !ok || cmd.complete == nil {
			return "", 0, false
		}
		candidates = cmd.complete(c)
	default:
		return "", 0, false
	}

	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, word) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return "", 0, false
	}

	completed := commonPrefix(matches)
	if len(matches) == 1 {
		completed += " "
	} else if completed == word && c.term != nil {
		// Nothing more to complete: show the options instead.
		c.term.Write([]byte(strings.Join(matches, "  ") + "\n"))
		return "", 0, false
	}

	return head[:start] + completed + tail, start + len(completed), true
}

// commandNames returns every command's primary name, sorted.
func (c *CLI) commandNames() []string {
	names := make([]string, 0, len(c.commands))
	for _, cmd := range c.commands {
		names = append(names, cmd.names[0])
	}
	sort.Strings(names)
	return names
}

// keyNames returns every secret's key, for completion.
func (c *CLI) keyNames() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	secrets, err := c.vault.List(ctx)
	if err != nil {
		return nil
	}

	keys := make([]string, 0, len(secrets))
	for _, s := range secrets {
		keys = append(keys, s.Key)
	}
	return keys
}

func commonPrefix(words []string) string {
	prefix := words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
