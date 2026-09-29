package cli

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
)

const (
	minBootstrapWindow = time.Minute
	maxBootstrapWindow = 24 * time.Hour
)

func (c *CLI) bootstrapCmd(ctx context.Context, args []string) error {
	const form = "bootstrap [open [duration] | lock | status]"

	if len(args) == 1 {
		return c.bootstrapStatus(ctx)
	}

	switch strings.ToLower(args[1]) {
	case "open", "clear": // clear is the v0.2.0 name
		window := bootstrap.DefaultWindow
		if len(args) == 3 {
			d, err := time.ParseDuration(args[2])
			if err != nil || d < minBootstrapWindow || d > maxBootstrapWindow {
				return usageError{reason: fmt.Sprintf("%q isn't a duration from 1m to 24h (e.g. 10m, 1h).", args[2]), form: form}
			}
			window = d
		} else if len(args) > 3 {
			return usageError{form: form}
		}

		until, err := c.bootstrap.Open(window)
		if err != nil {
			return fmt.Errorf("Couldn't open the bootstrap endpoint: %v", err)
		}
		success(fmt.Sprintf("Bootstrap endpoint open until %s (%s). It closes after one successful handout.",
			until.Local().Format("15:04"), window))
		return nil

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
