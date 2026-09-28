// Package cli is Cove's interactive command line (the `Cove CLI>` prompt).
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/vault"
)

// source is recorded in the event log for everything done through the CLI.
const source = "cove_cli"

type CLI struct {
	vault     *vault.Vault
	bootstrap *bootstrap.Marker

	// scanner reads stdin for both the prompt and follow-up questions such as
	// delete confirmations, so no input is lost between two readers.
	scanner *bufio.Scanner

	commands []command
	byName   map[string]*command
}

func New(v *vault.Vault, marker *bootstrap.Marker) *CLI {
	c := &CLI{
		vault:     v,
		bootstrap: marker,
		scanner:   bufio.NewScanner(os.Stdin),
		commands:  commandTable(),
		byName:    make(map[string]*command),
	}

	for i := range c.commands {
		for _, name := range c.commands[i].names {
			c.byName[name] = &c.commands[i]
		}
	}

	return c
}

// StartCLI reads and runs commands until stdin closes.
func (c *CLI) StartCLI(ctx context.Context) {
	for {
		fmt.Print("Cove CLI> ")
		if !c.scanner.Scan() {
			break
		}
		c.run(ctx, strings.Fields(c.scanner.Text()))
	}
}

func (c *CLI) run(ctx context.Context, args []string) {
	if len(args) == 0 {
		return
	}

	cmd, ok := c.byName[args[0]]
	if !ok {
		warningLog(fmt.Sprintf("Unknown command %q", args[0]))
		infoLog("Type 'help' to see available commands.")
		return
	}

	cmd.run(c, ctx, args)
}
