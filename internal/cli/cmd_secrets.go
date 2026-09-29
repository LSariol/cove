package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LSariol/Cove/internal/vault"
)

func (c *CLI) get(ctx context.Context, args []string) {
	if len(args) != 2 {
		usageLog("get <key>")
		return
	}
	key := args[1]

	secret, err := c.vault.Get(ctx, key, source)
	if err != nil {
		errorLog(secretError("get", key, err))
		return
	}

	successLog(fmt.Sprintf("%s: %s\n", secret.Key, secret.Value))
}

func (c *CLI) create(ctx context.Context, args []string) {
	if len(args) != 3 {
		usageLog("create <key> <value>")
		return
	}
	key := args[1]
	value := args[2]

	if err := c.vault.Create(ctx, key, value, source); err != nil {
		errorLog(secretError("create", key, err))
		return
	}

	successLog(fmt.Sprintf("Created %q.\n", key))
}

func (c *CLI) delete(ctx context.Context, args []string) {
	if len(args) != 2 {
		usageLog("delete <key>")
		return
	}
	key := args[1]

	warningLog(fmt.Sprintf("Delete %q? (y/N)", key))
	fmt.Print("Cove CLI> ")

	if !c.scanner.Scan() {
		infoLog("Delete cancelled.")
		return
	}

	response := strings.ToLower(strings.TrimSpace(c.scanner.Text()))

	if response != "y" && response != "yes" {
		infoLog("Delete cancelled.")
		return
	}

	if err := c.vault.Delete(ctx, key, source); err != nil {
		errorLog(secretError("delete", key, err))
		return
	}

	successLog(fmt.Sprintf("Deleted %q.\n", key))
}

func (c *CLI) update(ctx context.Context, args []string) {
	if len(args) != 3 {
		usageLog("update <key> <value>")
		return
	}
	key := args[1]
	value := args[2]

	updated, err := c.vault.Update(ctx, key, value, source)
	if errors.Is(err, vault.ErrNotFound) {
		errorLog(fmt.Sprintf("No secret named %q. Use \"create\" to add it.", key))
		return
	}
	if err != nil {
		errorLog(secretError("update", key, err))
		return
	}

	successLog(fmt.Sprintf("Updated %q (now version %d).\n", key, updated.Version))
}

func (c *CLI) list(ctx context.Context, args []string) {
	switch len(args) {
	case 1:
		c.printSecrets(ctx, "", "all")
	case 2:
		c.printSecrets(ctx, args[1], "prefix")
	case 3:
		mode := strings.ToLower(args[2])
		if mode == "fuzzy" || mode == "f" {
			c.printSecrets(ctx, args[1], "fuzzy")
		} else {
			warningLog(fmt.Sprintf("Unknown list option %q.", args[2]))
			usageLog("list [prefix]  or  list <text> fuzzy")
		}
	default:
		usageLog("list [prefix]  or  list <text> fuzzy")
	}
}

// secretError turns an error from the vault into a message for the prompt.
// action is what was being attempted, e.g. "create".
func secretError(action string, key string, err error) string {
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return fmt.Sprintf("No secret named %q.", key)
	case errors.Is(err, vault.ErrAlreadyExists):
		return fmt.Sprintf("A secret named %q already exists. Use \"update\" to change its value.", key)
	case errors.Is(err, vault.ErrDecrypt):
		return fmt.Sprintf("Couldn't decrypt %q. VAULT_ENCRYPTION_KEY may have changed since it was stored.", key)
	default:
		return fmt.Sprintf("Couldn't %s %q: %v", action, key, err)
	}
}

// printSecrets prints a table of secrets whose keys match term. mode is "all",
// "prefix" (key starts with term) or "fuzzy" (key contains term).
func (c *CLI) printSecrets(ctx context.Context, term string, mode string) {

	secrets, err := c.vault.List(ctx)
	if err != nil {
		errorLog(fmt.Sprintf("Couldn't list secrets: %v", err))
		return
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
			infoLog("The vault is empty.")
		} else {
			infoLog(fmt.Sprintf("No secrets match %q.", term))
		}
		return
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

	plainLog(header)
	plainLog(divider)

	for _, entry := range matched {
		row := fmt.Sprintf(
			"%-*s | %-*s | %-*s | %-*d | %-*d\n",
			keyW, entry.Key,
			dateW, formatTime(entry.CreatedAt),
			dateW, formatTime(entry.UpdatedAt),
			versW, entry.Version,
			pulledW, entry.ReadCount,
		)
		plainLog(row)
	}
}
