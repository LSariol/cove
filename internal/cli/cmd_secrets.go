package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LSariol/Cove/internal/vault"
)

func (c *CLI) get(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usageError{form: "get <key>"}
	}
	key := args[1]

	secret, err := c.vault.Get(ctx, key, source)
	if err != nil {
		return secretError("get", key, err)
	}

	out(secret.Value)
	return nil
}

func (c *CLI) create(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return usageError{form: "create <key> <value>"}
	}
	key := args[1]
	value := args[2]

	if err := c.vault.Create(ctx, key, value, source); err != nil {
		return secretError("create", key, err)
	}

	success(fmt.Sprintf("Created %q.", key))
	return nil
}

func (c *CLI) delete(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usageError{form: "delete <key>"}
	}
	key := args[1]

	if !c.confirm(fmt.Sprintf("Delete %q? (y/N)", key)) {
		info("Delete cancelled.")
		return nil
	}

	if err := c.vault.Delete(ctx, key, source); err != nil {
		return secretError("delete", key, err)
	}

	success(fmt.Sprintf("Deleted %q.", key))
	return nil
}

func (c *CLI) update(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return usageError{form: "update <key> <value>"}
	}
	key := args[1]
	value := args[2]

	updated, err := c.vault.Update(ctx, key, value, source)
	if errors.Is(err, vault.ErrNotFound) {
		return fmt.Errorf("No secret named %q. Use \"create\" to add it.", key)
	}
	if err != nil {
		return secretError("update", key, err)
	}

	success(fmt.Sprintf("Updated %q (now version %d).", key, updated.Version))
	return nil
}

func (c *CLI) list(ctx context.Context, args []string) error {
	const form = "list [prefix]  or  list <text> fuzzy"

	switch len(args) {
	case 1:
		return c.printSecrets(ctx, "", "all")
	case 2:
		return c.printSecrets(ctx, args[1], "prefix")
	case 3:
		mode := strings.ToLower(args[2])
		if mode != "fuzzy" && mode != "f" {
			return usageError{reason: fmt.Sprintf("Unknown list option %q.", args[2]), form: form}
		}
		return c.printSecrets(ctx, args[1], "fuzzy")
	default:
		return usageError{form: form}
	}
}

// secretError turns an error from the vault into a message for the prompt.
// action is what was being attempted, e.g. "create".
func secretError(action string, key string, err error) error {
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return fmt.Errorf("No secret named %q.", key)
	case errors.Is(err, vault.ErrAlreadyExists):
		return fmt.Errorf("A secret named %q already exists. Use \"update\" to change its value.", key)
	case errors.Is(err, vault.ErrDecrypt):
		return fmt.Errorf("Couldn't decrypt %q. VAULT_ENCRYPTION_KEY may have changed since it was stored.", key)
	default:
		return fmt.Errorf("Couldn't %s %q: %v", action, key, err)
	}
}

// printSecrets prints a table of secrets whose keys match term. mode is "all",
// "prefix" (key starts with term) or "fuzzy" (key contains term).
func (c *CLI) printSecrets(ctx context.Context, term string, mode string) error {

	secrets, err := c.vault.List(ctx)
	if err != nil {
		return fmt.Errorf("Couldn't list secrets: %v", err)
	}

	term = strings.ToLower(term)

	var matched []vault.Secret

	for _, entry := range secrets {
		keyLower := strings.ToLower(entry.Key)

		switch mode {
		case "prefix":
			if strings.HasPrefix(keyLower, term) {
				matched = append(matched, entry)
			}
		case "fuzzy":
			if strings.Contains(keyLower, term) {
				matched = append(matched, entry)
			}
		default:
			matched = append(matched, entry)
		}
	}

	if len(matched) == 0 {
		if mode == "all" {
			info("The vault is empty.")
		} else {
			info(fmt.Sprintf("No secrets match %q.", term))
		}
		return nil
	}

	const (
		keyW    = 35
		dateW   = 19
		versW   = 7
		pulledW = 12
		timeFmt = "2006-01-02 15:04:05"
	)

	formatTime := func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Format(timeFmt)
	}

	header := fmt.Sprintf(
		"%-*s | %-*s | %-*s | %-*s | %-*s\n",
		keyW, "Key",
		dateW, "Date Added",
		dateW, "Last Modified",
		versW, "Version",
		pulledW, "Times Pulled",
	)

	divider := fmt.Sprintln(
		strings.Repeat("-", keyW) + "-+-" +
			strings.Repeat("-", dateW) + "-+-" +
			strings.Repeat("-", dateW) + "-+-" +
			strings.Repeat("-", versW) + "-+-" +
			strings.Repeat("-", pulledW),
	)

	fmt.Fprint(stdout, header)
	fmt.Fprint(stdout, divider)

	for _, entry := range matched {
		row := fmt.Sprintf(
			"%-*s | %-*s | %-*s | %-*d | %-*d\n",
			keyW, entry.Key,
			dateW, formatTime(entry.CreatedAt),
			dateW, formatTime(entry.UpdatedAt),
			versW, entry.Version,
			pulledW, entry.ReadCount,
		)
		fmt.Fprint(stdout, row)
	}
	return nil
}
