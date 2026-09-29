package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
)

const (
	defaultGeneratedLength = 32
	minGeneratedLength     = 16
	maxGeneratedLength     = 256
)

// generate creates a secret with a random value, or replaces an existing
// secret's value with one. The value is printed to stdout, so
// `cove generate KEY` can also feed a script.
func (c *CLI) generate(ctx context.Context, args []string) error {
	const form = "generate <key> [length] [--yes]"

	skipConfirm, rest := takeYesFlag(args[1:])
	if len(rest) < 1 || len(rest) > 2 {
		return usageError{form: form}
	}
	key := rest[0]

	length := defaultGeneratedLength
	if len(rest) == 2 {
		n, err := strconv.Atoi(rest[1])
		if err != nil || n < minGeneratedLength || n > maxGeneratedLength {
			return usageError{
				reason: fmt.Sprintf("Length must be a number from %d to %d.", minGeneratedLength, maxGeneratedLength),
				form:   form,
			}
		}
		length = n
	}

	value, err := encryption.GenerateSecret(length)
	if err != nil {
		return fmt.Errorf("Couldn't generate a value: %v", err)
	}

	err = c.vault.Create(ctx, key, value, source)
	if err == nil {
		out(value)
		success(fmt.Sprintf("Created %q with a random %d-character value.", key, length))
		return nil
	}
	if !errors.Is(err, vault.ErrAlreadyExists) {
		return secretError("create", key, err)
	}

	// The key exists: replacing its value changes what apps get, so ask first.
	if !skipConfirm {
		yes, answered := c.confirm(fmt.Sprintf("%q already exists. Replace its value with a new random one? (y/N)", key))
		if !answered {
			return fmt.Errorf("Generate cancelled: %q already exists and there was no answer to the confirmation. Use --yes to replace it.", key)
		}
		if !yes {
			info("Generate cancelled.")
			return nil
		}
	}

	updated, err := c.vault.Update(ctx, key, value, source)
	if err != nil {
		return secretError("update", key, err)
	}

	out(value)
	success(fmt.Sprintf("Replaced %q with a random %d-character value (now version %d).", key, length, updated.Version))
	return nil
}
