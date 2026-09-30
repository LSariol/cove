package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/tokens"
)

const (
	minBootstrapWindow = time.Minute
	maxBootstrapWindow = 24 * time.Hour
)

func (c *CLI) bootstrapCmd(ctx context.Context, args []string) error {
	const form = "bootstrap [open [project] [duration] | lock | status]"

	if len(args) == 1 {
		return c.bootstrapStatus(ctx)
	}

	switch strings.ToLower(args[1]) {
	case "open":
		window := bootstrap.DefaultWindow
		project := ""
		if len(args) > 4 {
			return usageError{form: form}
		}
		for _, arg := range args[2:] {
			if d, err := time.ParseDuration(arg); err == nil {
				if d < minBootstrapWindow || d > maxBootstrapWindow {
					return usageError{reason: fmt.Sprintf("%q isn't a duration from 1m to 24h (e.g. 10m, 1h).", arg), form: form}
				}
				window = d
				continue
			}
			if project != "" || c.tokens == nil {
				return usageError{reason: fmt.Sprintf("%q isn't a duration from 1m to 24h (e.g. 10m, 1h).", arg), form: form}
			}
			project = arg
		}

		if project == "" {
			until, err := c.bootstrap.Open(window)
			if err != nil {
				return fmt.Errorf("Couldn't open the bootstrap endpoint: %v", err)
			}
			success(fmt.Sprintf("Bootstrap endpoint open until %s (%s) to hand out the master token. It closes after one successful handout.",
				until.Local().Format("15:04"), window))
			return nil
		}
		return c.bootstrapOpenFor(ctx, project, window)

	case "lock":
		if len(args) != 2 {
			return usageError{form: form}
		}
		if err := c.bootstrap.Lock(); err != nil {
			return fmt.Errorf("Couldn't lock the bootstrap endpoint: %v", err)
		}
		success("Bootstrap endpoint locked.")
		return nil

	case "status":
		if len(args) != 2 {
			return usageError{form: form}
		}
		return c.bootstrapStatus(ctx)

	default:
		return usageError{reason: fmt.Sprintf("Unknown bootstrap option %q.", args[1]), form: form}
	}
}

// recentBootstrapCount is how many attempts `bootstrap status` shows.
const recentBootstrapCount = 5

func (c *CLI) bootstrapStatus(ctx context.Context) error {
	st, err := c.bootstrap.Status()
	if err != nil {
		return fmt.Errorf("Couldn't read the bootstrap state: %v", err)
	}

	out("Endpoint:      " + describeBootstrap(st))
	out("Hands out:     " + describeHandout(st))

	lastHandout := "never"
	if !st.LastHandoutAt.IsZero() {
		lastHandout = fmt.Sprintf("%s to %s", formatTime(st.LastHandoutAt), st.LastHandoutTo)
	}
	out("Last handout:  " + lastHandout)

	allowed := "any address"
	if len(st.Allowed) > 0 {
		names := make([]string, len(st.Allowed))
		for i, prefix := range st.Allowed {
			names[i] = prefix.String()
		}
		allowed = strings.Join(names, ", ")
	}
	out("Allowed from:  " + allowed)

	if c.db == nil {
		return nil
	}
	attempts, err := c.db.RecentBootstraps(ctx, recentBootstrapCount)
	if err != nil {
		return fmt.Errorf("Couldn't read recent bootstrap attempts: %v", err)
	}
	if len(attempts) == 0 {
		out("\nNo bootstrap attempts recorded yet.")
		return nil
	}

	out("\nRecent attempts:")
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  WHEN\tFROM\tRESULT")
	for _, a := range attempts {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", formatTime(a.OccurredAt), a.RemoteAddr, a.Outcome)
	}
	return w.Flush()
}

// bootstrapOpenFor opens the endpoint to hand out a project's own token. Only
// a hash of each token is stored, so the project gets a new one: its current
// token stops working.
func (c *CLI) bootstrapOpenFor(ctx context.Context, project string, window time.Duration) error {
	if _, err := c.tokens.Get(ctx, project); err != nil {
		if errors.Is(err, tokens.ErrNotFound) {
			return fmt.Errorf("No token named %q. Create it first: token create %s --allow '%s_*'", project, project, strings.ToUpper(project))
		}
		return fmt.Errorf("Couldn't read %s's token: %v", project, err)
	}

	value, err := c.tokens.Rotate(ctx, project, source, "for bootstrap")
	if err != nil {
		return fmt.Errorf("Couldn't prepare a new token for %s: %v", project, err)
	}
	until, err := c.bootstrap.OpenFor(window, project, value)
	if err != nil {
		return fmt.Errorf("Couldn't open the bootstrap endpoint (%s's token was already replaced; run this again): %v", project, err)
	}

	success(fmt.Sprintf("Bootstrap endpoint open until %s (%s) to hand out a new token for %s. It closes after one successful handout.",
		until.Local().Format("15:04"), window, project))
	warn(fmt.Sprintf("%s's previous token stopped working.", project))
	return nil
}

// describeHandout says which token the endpoint hands out.
func describeHandout(st bootstrap.Status) string {
	if st.TokenName == "" {
		return "the master token (COVE_CLIENT_SECRET)"
	}
	return st.TokenName + "'s token"
}

// describeBootstrap summarizes whether the endpoint is open, e.g.
// "open for 7m0s more (until 15:42)".
func describeBootstrap(st bootstrap.Status) string {
	switch {
	case st.Open:
		left := time.Until(st.OpenUntil).Round(time.Minute)
		return fmt.Sprintf("open for %s more (until %s)", left, st.OpenUntil.Local().Format("15:04"))
	case st.Expired:
		return fmt.Sprintf("closed (the window expired unused at %s)", formatTime(st.OpenUntil))
	default:
		return "closed"
	}
}
