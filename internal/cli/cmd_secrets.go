package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/LSariol/Cove/internal/vault"
)

func (c *CLI) get(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usageError{form: "get <key>"}
	}
	key := args[1]

	secret, err := c.vault.Show(ctx, key, source)
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
	skipConfirm := false
	var rest []string
	for _, arg := range args[1:] {
		if arg == "--yes" || arg == "-y" {
			skipConfirm = true
		} else {
			rest = append(rest, arg)
		}
	}

	if len(rest) != 1 {
		return usageError{form: "delete <key> [--yes]"}
	}
	key := rest[0]

	if !skipConfirm {
		yes, answered := c.confirm(fmt.Sprintf("Delete %q? (y/N)", key))
		if !answered {
			return fmt.Errorf("Delete cancelled: no answer to the confirmation. Use --yes to delete without asking.")
		}
		if !yes {
			info("Delete cancelled.")
			return nil
		}
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

func (c *CLI) rename(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return usageError{form: "rename <key> <new-key>"}
	}
	oldKey, newKey := args[1], args[2]

	err := c.vault.Rename(ctx, oldKey, newKey, source)
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return fmt.Errorf("No secret named %q.", oldKey)
	case errors.Is(err, vault.ErrAlreadyExists):
		return fmt.Errorf("A secret named %q already exists. Pick another name, or delete that one first.", newKey)
	case err != nil:
		return fmt.Errorf("Couldn't rename %q to %q: %v", oldKey, newKey, err)
	}

	success(fmt.Sprintf("Renamed %q to %q.", oldKey, newKey))
	warn(fmt.Sprintf("Apps still asking for %q will get \"not found\" until they use the new key.", oldKey))
	return nil
}

func (c *CLI) search(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usageError{form: "search <text>"}
	}
	return c.printSecrets(ctx, args[1], "fuzzy")
}

// list also accepts the older `list <text> fuzzy` (or `f`) form of search.
func (c *CLI) list(ctx context.Context, args []string) error {
	const form = "list [prefix]   (to match anywhere in the key: search <text>)"

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

	printSecretTable(matched)

	if len(matched) == len(secrets) {
		info(fmt.Sprintf("%d %s", len(matched), plural(len(matched), "secret", "secrets")))
	} else {
		info(fmt.Sprintf("%d of %d secrets", len(matched), len(secrets)))
	}
	return nil
}

// printSecretTable writes secrets as aligned columns to stdout. The key column
// is as wide as the longest key.
func printSecretTable(secrets []vault.Secret) {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tVERSION\tREADS\tCREATED\tUPDATED")
	for _, s := range secrets {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", s.Key, s.Version, s.ReadCount, formatTime(s.CreatedAt), formatTime(s.UpdatedAt))
	}
	w.Flush()
}

func plural(n int, one string, many string) string {
	if n == 1 {
		return one
	}
	return many
}
