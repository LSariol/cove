package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/vault"
)

const defaultHistoryCount = 20

func (c *CLI) info(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usageError{form: "info <key>"}
	}
	key := args[1]

	details, err := c.vault.Info(ctx, key)
	if errors.Is(err, vault.ErrNotFound) {
		return c.notFoundWithHistory(ctx, key)
	}
	if err != nil {
		return secretError("get details for", key, err)
	}

	lastRead := "never"
	if details.LastRead != nil {
		lastRead = fmt.Sprintf("%s by %s", formatTime(details.LastRead.OccurredAt), details.LastRead.Source)
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Key:\t%s\n", details.Key)
	fmt.Fprintf(w, "Version:\t%d\n", details.Version)
	fmt.Fprintf(w, "Reads:\t%d (by apps; the CLI's own reads aren't counted)\n", details.ReadCount)
	fmt.Fprintf(w, "Last read:\t%s\n", lastRead)
	fmt.Fprintf(w, "Created:\t%s\n", formatTime(details.CreatedAt))
	fmt.Fprintf(w, "Updated:\t%s\n", formatTime(details.UpdatedAt))
	if c.tokens != nil {
		readers, err := c.tokens.Readers(ctx, key)
		if err != nil {
			return fmt.Errorf("Couldn't check which tokens can read %q: %v", key, err)
		}
		fmt.Fprintf(w, "Readable by:\t%s\n", describeReaders(readers))
	}
	return w.Flush()
}

func describeReaders(names []string) string {
	if len(names) == 0 {
		return "no project tokens"
	}
	return strings.Join(names, ", ")
}

func (c *CLI) history(ctx context.Context, args []string) error {
	const form = "history <key> [count]"
	if len(args) < 2 || len(args) > 3 {
		return usageError{form: form}
	}
	key := args[1]

	count := defaultHistoryCount
	if len(args) == 3 {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 1 {
			return usageError{reason: fmt.Sprintf("%q isn't a positive number.", args[2]), form: form}
		}
		count = n
	}

	events, err := c.vault.History(ctx, key, count)
	if err != nil {
		return fmt.Errorf("Couldn't read the history of %q: %v", key, err)
	}
	if len(events) == 0 {
		return fmt.Errorf("No history for %q. Check the key with \"search\".", key)
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tEVENT\tVERSION\tSOURCE\tDETAIL")
	for _, e := range events {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", formatTime(e.OccurredAt), e.Kind, e.SecretVersion, e.Source, e.Detail)
	}
	return w.Flush()
}

// notFoundWithHistory explains a missing key, mentioning when it was deleted
// if the event log shows it once existed.
func (c *CLI) notFoundWithHistory(ctx context.Context, key string) error {
	events, err := c.vault.History(ctx, key, 1)
	if err == nil && len(events) == 1 && events[0].Kind == database.EventDelete {
		return fmt.Errorf("No secret named %q. It was deleted %s by %s; \"restore %s\" brings it back.",
			key, formatTime(events[0].OccurredAt), events[0].Source, key)
	}
	return fmt.Errorf("No secret named %q.", key)
}

// formatTime shows t in local time, to the minute.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
