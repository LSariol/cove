package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LSariol/Cove/internal/vault"
)

func (c *CLI) get(ctx context.Context, args []string) {
	if len(args) != 2 {
		warningLog("Get requires 1 additional argument.")
		infoLog("get <secret>")
		return
	}

	res, err := c.vault.Get(ctx, args[1], source)
	if err != nil {
		errorLog(fmt.Sprintf("error getting %q: %v", args[1], err))
		return
	}

	successLog(fmt.Sprintf("%s : %s\n", res.Key, res.Value))
}

func (c *CLI) create(ctx context.Context, args []string) {
	if len(args) != 3 {
		warningLog("Create requires 2 additional arguments.")
		infoLog("create <secretName> <value>")
		return
	}

	key := args[1]
	value := args[2]

	if err := c.vault.Create(ctx, key, value, source); err != nil {
		errorLog(err.Error())
		return
	}

	successLog(fmt.Sprintf("%s has been created and stored.\n", key))
}

func (c *CLI) delete(ctx context.Context, args []string) {
	if len(args) != 2 {
		warningLog("Delete requires 1 additional argument.")
		infoLog("delete <secretName>")
		return
	}

	secretName := args[1]
	warningLog(fmt.Sprintf("Are you sure you want to delete %q (y/N)", secretName))
	fmt.Print("Cove CLI> ")

	if !c.scanner.Scan() {
		warningLog("Delete Cancelled")
		return
	}

	response := strings.ToLower(strings.TrimSpace(c.scanner.Text()))

	if response != "y" && response != "yes" {
		infoLog("Delete cancelled.")
		return
	}

	if err := c.vault.Delete(ctx, secretName, source); err != nil {
		errorLog(err.Error())
		return
	}

	successLog("Secret has been removed\n")
}

func (c *CLI) update(ctx context.Context, args []string) {
	if len(args) != 3 {
		warningLog("Update requires 2 additional arguments.")
		infoLog("update <secretName> <newValue>")
		return
	}

	key := args[1]
	value := args[2]

	if err := c.vault.Update(ctx, key, value, source); err != nil {
		errorLog(err.Error())
		return
	}

	successLog("Secret has been updated.\n")
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
			warningLog("List third argument must be 'fuzzy' or 'f'.")
			infoLog("list [term] [fuzzy|f]")
		}
	default:
		warningLog("List takes at most 2 additional arguments.")
		infoLog("list [term] [fuzzy|f]")
	}
}

// printSecrets prints a table of secrets whose keys match term. mode is "all",
// "prefix" (key starts with term) or "fuzzy" (key contains term).
func (c *CLI) printSecrets(ctx context.Context, term string, mode string) {

	secrets, err := c.vault.List(ctx)
	if err != nil {
		errorLog(fmt.Sprintf("list secrets: %v", err))
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
		infoLog("No secrets matched your query.")
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
