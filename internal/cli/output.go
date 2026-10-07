package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Output follows the clig.dev conventions:
//
//   - Data (secret values, tables, help) goes to stdout, uncolored, so it can
//     be piped or captured: value=$(cove get MYAPP_KEY).
//   - Messages go to stderr, each marked with a symbol so the meaning doesn't
//     depend on color: ✓ success, ! warning, ✗ error.
//   - Color is only used when stderr is a terminal and NO_COLOR isn't set.
var (
	stdout   io.Writer = os.Stdout
	stderr   io.Writer = os.Stderr
	useColor           = colorEnabled()
)

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

func colorize(color string, s string) string {
	if !useColor {
		return s
	}
	return color + s + reset
}

func out(s string) {
	fmt.Fprintln(stdout, s)
}

// success reports that something worked, e.g. "✓ Created "x".".
func success(msg string) {
	fmt.Fprintln(stderr, colorize(green, "✓ "+msg))
}

// warn reports something the user should notice or fix, e.g. wrong arguments.
func warn(msg string) {
	fmt.Fprintln(stderr, colorize(yellow, "! "+msg))
}

func fail(msg string) {
	fmt.Fprintln(stderr, colorize(red, "✗ "+msg))
}

// info reports something neutral, e.g. "The vault is empty.".
func info(msg string) {
	fmt.Fprintln(stderr, msg)
}

// ask shows a question and leaves the cursor after it for the answer.
func ask(question string) {
	fmt.Fprint(stderr, colorize(yellow, "? "+question)+" ")
}

// promptFor returns the prompt for an environment such as "dev" or "prod":
// "cove (dev)> ". Production is shown in red, so it's hard to mistake.
func promptFor(env string) string {
	env = strings.ToLower(strings.TrimSpace(env))
	if env == "" {
		return "cove> "
	}

	label := "(" + env + ")"
	if env == "prod" || env == "production" {
		label = colorize(red, label)
	}
	return "cove " + label + "> "
}
