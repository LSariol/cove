package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/LSariol/Cove/internal/vault"
)

func (c *CLI) restore(ctx context.Context, args []string) error {
	const form = "restore <key> [version] [--yes]"

	skipConfirm, rest := takeYesFlag(args[1:])
	if len(rest) < 1 || len(rest) > 2 {
		return usageError{form: form}
	}
	key := rest[0]

	version := 0
	if len(rest) == 2 {
		n, err := strconv.Atoi(rest[1])
		if err != nil || n < 1 {
			return usageError{reason: fmt.Sprintf("%q isn't a version number.", rest[1]), form: form}
		}
		version = n
	}

	// Restoring over an existing value replaces what apps get, so ask first.
	if _, err := c.vault.Info(ctx, key); err == nil && !skipConfirm {
		question := fmt.Sprintf("Replace the current value of %q with an earlier one? (y/N)", key)
		yes, answered := c.confirm(question)
		if !answered {
			return fmt.Errorf("Restore cancelled: no answer to the confirmation. Use --yes to restore without asking.")
		}
		if !yes {
			info("Restore cancelled.")
			return nil
		}
	}

	restored, from, err := c.vault.Restore(ctx, key, version, source)
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return fmt.Errorf("No secret named %q, and no history to restore it from.", key)
	case errors.Is(err, vault.ErrVersionNotFound):
		return fmt.Errorf("%q has no version %d. \"history %s\" shows its versions.", key, version, key)
	case errors.Is(err, vault.ErrNothingToRestore):
		return fmt.Errorf("Nothing to restore for %q: there's no earlier value (or it already has that value).", key)
	case err != nil:
		return secretError("restore", key, err)
	}

	success(fmt.Sprintf("Restored %q to the value from version %d (now version %d).", key, from, restored.Version))
	return nil
}

// takeYesFlag removes --yes / -y from args and reports whether it was there.
func takeYesFlag(args []string) (bool, []string) {
	yes := false
	var rest []string
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			yes = true
		} else {
			rest = append(rest, arg)
		}
	}
	return yes, rest
}
