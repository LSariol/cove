package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/LSariol/Cove/internal/database"
)

// StatusSource reports database health for `status`. *database.Database
// implements it.
type StatusSource interface {
	Ping(ctx context.Context) error
	SchemaVersion(ctx context.Context) (have int64, want int64, err error)
	RecentBootstraps(ctx context.Context, limit int) ([]database.BootstrapAttempt, error)
}

// status prints an overview of Cove's health, and returns an error (so a
// one-shot `cove status` exits non-zero) when something needs attention.
func (c *CLI) status(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError{form: "status"}
	}

	var problems []string
	rows := [][2]string{
		{"Version", orDash(c.version)},
		{"Environment", orDash(strings.ToLower(c.env))},
	}

	dbOK := false
	switch {
	case c.db == nil:
		rows = append(rows, [2]string{"Database", "-"})
	case c.db.Ping(ctx) != nil:
		rows = append(rows, [2]string{"Database", "unreachable"})
		problems = append(problems, "the database is unreachable")
	default:
		dbOK = true
		rows = append(rows, [2]string{"Database", "reachable"})
	}

	if dbOK {
		have, want, err := c.db.SchemaVersion(ctx)
		switch {
		case err != nil:
			rows = append(rows, [2]string{"Schema", "unknown"})
			problems = append(problems, fmt.Sprintf("the schema version couldn't be read (%v)", err))
		case have < want:
			rows = append(rows, [2]string{"Schema", fmt.Sprintf("version %d (this build needs %d)", have, want)})
			problems = append(problems, "migrations are missing")
		default:
			rows = append(rows, [2]string{"Schema", fmt.Sprintf("version %d (up to date)", have)})
		}

		if secrets, err := c.vault.List(ctx); err == nil {
			rows = append(rows, [2]string{"Secrets", fmt.Sprint(len(secrets))})
		} else {
			rows = append(rows, [2]string{"Secrets", "unknown"})
			problems = append(problems, fmt.Sprintf("secrets couldn't be listed (%v)", err))
		}
	}

	if c.bootstrap != nil {
		st, err := c.bootstrap.Status()
		if err != nil {
			rows = append(rows, [2]string{"Bootstrap", "unknown"})
			problems = append(problems, fmt.Sprintf("the bootstrap state couldn't be read (%v)", err))
		} else {
			rows = append(rows, [2]string{"Bootstrap", describeBootstrap(st)})
		}
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
	}
	w.Flush()

	if len(problems) > 0 {
		return errors.New("Needs attention: " + strings.Join(problems, "; ") + ".")
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
