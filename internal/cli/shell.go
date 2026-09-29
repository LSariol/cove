// Package cli is Cove's command line: the interactive `cove>` prompt, and
// one-shot commands such as `cove get KEY`.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/vault"
	"golang.org/x/term"
)

// source is recorded in the event log for everything done through the CLI.
const source = "cove_cli"

// Options are the CLI's settings.
type Options struct {
	// Embedded is true when the CLI runs inside the server process (plain
	// `cove`). Then `exit` stops the API server too; otherwise it only leaves
	// the shell.
	Embedded bool

	// Env is the environment shown in the prompt, e.g. "dev" or "prod"
	// (APP_ENV). Production is shown in red.
	Env string

	// Version and DB are reported by `status`.
	Version string
	DB      StatusSource
}

type CLI struct {
	vault     *vault.Vault
	bootstrap *bootstrap.Marker
	embedded  bool
	prompt    string
	env       string
	version   string
	db        StatusSource

	// scanner reads stdin for both the prompt and follow-up questions such as
	// delete confirmations, so no input is lost between two readers. When the
	// shell has line editing, term is used instead.
	scanner *bufio.Scanner
	term    *term.Terminal

	commands []command
	byName   map[string]*command

	// stop asks the process to shut down; `exit` calls it. It's set by Run.
	stop func()
}

func New(v *vault.Vault, marker *bootstrap.Marker, opts Options) *CLI {
	c := &CLI{
		vault:     v,
		bootstrap: marker,
		embedded:  opts.Embedded,
		prompt:    promptFor(opts.Env),
		env:       opts.Env,
		version:   opts.Version,
		db:        opts.DB,
		scanner:   bufio.NewScanner(os.Stdin),
		commands:  commandTable(opts.Embedded),
		byName:    make(map[string]*command),
	}

	for i := range c.commands {
		for _, name := range c.commands[i].names {
			c.byName[name] = &c.commands[i]
		}
	}

	return c
}

// Run reads and runs commands until stdin closes, `exit` is typed, or ctx is
// cancelled. `exit` calls stop.
//
// A standalone shell on a terminal gets line editing and Tab completion. The
// CLI embedded in the server keeps plain line input, so Ctrl+C there still
// stops Cove as a signal.
func (c *CLI) Run(ctx context.Context, stop func()) {
	c.stop = stop

	if !c.embedded && term.IsTerminal(int(os.Stdin.Fd())) && c.runTerminal(ctx) {
		return
	}

	for ctx.Err() == nil {
		fmt.Fprint(stderr, c.prompt)
		if !c.scanner.Scan() {
			return
		}

		args := strings.Fields(c.scanner.Text())
		if len(args) == 0 {
			continue
		}
		report(c.Exec(ctx, args))
	}
}

// Exec runs a single command, e.g. ["get", "MYAPP_KEY"]. It returns the
// command's error, which the caller shows (see report); one-shot commands
// also turn it into a non-zero exit status.
func (c *CLI) Exec(ctx context.Context, args []string) error {
	cmd, ok := c.byName[args[0]]
	if !ok {
		return fmt.Errorf("Unknown command %q. Type \"help\" to see the available commands.", args[0])
	}

	return cmd.run(c, ctx, args)
}

// report shows an error returned by a command: wrong arguments as a warning,
// anything else as an error.
func report(err error) {
	if err == nil {
		return
	}

	var usage usageError
	if errors.As(err, &usage) {
		warn(err.Error())
		return
	}
	fail(err.Error())
}

// Report shows an error returned by Exec the same way the prompt does.
func Report(err error) {
	report(err)
}

// confirm asks a yes/no question. yes reports whether the answer was yes;
// answered is false when there was no input to answer with (e.g. a one-shot
// command run without a terminal). With line editing the question becomes the
// prompt for the answer.
func (c *CLI) confirm(question string) (yes bool, answered bool) {
	var answer string

	if c.term != nil {
		c.term.SetPrompt(colorize(yellow, "? "+question) + " ")
		line, err := c.term.ReadLine()
		c.term.SetPrompt(c.prompt)
		if err != nil {
			return false, false
		}
		answer = line
	} else {
		ask(question)
		if !c.scanner.Scan() {
			fmt.Fprintln(stderr)
			return false, false
		}
		answer = c.scanner.Text()
	}

	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", true
}
