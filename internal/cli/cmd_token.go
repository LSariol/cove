package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LSariol/Cove/internal/tokens"
	"github.com/LSariol/Cove/internal/vault"
)

const tokenForm = "token [list | create <name> [--allow <pattern>]... [--write <pattern>]... | show <name> |\n" +
	"       allow <pattern> <name>... [--write] | deny <pattern> <name>... | rotate <name> [--yes] | revoke <name> [--yes]]"

// tokenCmd manages per-project tokens.
func (c *CLI) tokenCmd(ctx context.Context, args []string) error {
	if c.tokens == nil {
		return errors.New("Token commands need the database, which isn't connected.")
	}
	if len(args) == 1 {
		return c.tokenList(ctx)
	}

	rest := args[2:]
	switch strings.ToLower(args[1]) {
	case "list":
		if len(rest) != 0 {
			return usageError{form: "token list"}
		}
		return c.tokenList(ctx)
	case "create":
		return c.tokenCreate(ctx, rest)
	case "show":
		if len(rest) != 1 {
			return usageError{form: "token show <name>"}
		}
		return c.tokenShow(ctx, rest[0])
	case "allow":
		return c.tokenAllow(ctx, rest)
	case "deny":
		return c.tokenDeny(ctx, rest)
	case "rotate":
		return c.tokenRotate(ctx, rest)
	case "revoke":
		return c.tokenRevoke(ctx, rest)
	default:
		return usageError{reason: fmt.Sprintf("Unknown token option %q.", args[1]), form: tokenForm}
	}
}

func (c *CLI) tokenList(ctx context.Context) error {
	list, err := c.tokens.List(ctx)
	if err != nil {
		return fmt.Errorf("Couldn't list tokens: %v", err)
	}
	if len(list) == 0 {
		info("No project tokens yet. Create one with \"token create <name> --allow '<NAME>_*'\".")
		return nil
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREAD\tREAD/WRITE\tLAST USED")
	for _, t := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Name, joinOrDash(t.Read), joinOrDash(t.Write), formatOptionalTime(t.LastUsedAt, "never"))
	}
	w.Flush()
	info(fmt.Sprintf("%d %s. The master token (COVE_CLIENT_SECRET) also works, with access to everything.",
		len(list), plural(len(list), "token", "tokens")))
	return nil
}

func (c *CLI) tokenCreate(ctx context.Context, args []string) error {
	const form = "token create <name> [--allow <pattern>]... [--write <pattern>]..."

	var name string
	var read, write []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, value, hasValue := strings.Cut(arg, "=")
		switch flag {
		case "--allow", "--write":
			if !hasValue {
				if i+1 >= len(args) {
					return usageError{reason: fmt.Sprintf("%s needs a pattern.", flag), form: form}
				}
				i++
				value = args[i]
			}
			if flag == "--allow" {
				read = append(read, unquote(value))
			} else {
				write = append(write, unquote(value))
			}
		default:
			if strings.HasPrefix(arg, "-") || name != "" {
				return usageError{reason: fmt.Sprintf("Unexpected %q.", arg), form: form}
			}
			name = arg
		}
	}
	if name == "" {
		return usageError{form: form}
	}

	value, tok, err := c.tokens.Create(ctx, name, read, write, source)
	if errors.Is(err, tokens.ErrExists) {
		return fmt.Errorf("A token named %q already exists. Change its access with \"token allow\" / \"token deny\", or get a new value with \"token rotate %s\".", name, name)
	}
	if err != nil {
		return fmt.Errorf("Couldn't create the token: %v", err)
	}

	success(fmt.Sprintf("Created a token for %s. Copy it now; it can't be shown again:", name))
	out(value)
	c.describeReach(ctx, tok)
	return nil
}

func (c *CLI) tokenShow(ctx context.Context, name string) error {
	tok, err := c.tokens.Get(ctx, name)
	if err != nil {
		return tokenError(name, err)
	}

	rotated := "never"
	if tok.RotatedAt != nil {
		rotated = formatTime(*tok.RotatedAt)
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", tok.Name)
	fmt.Fprintf(w, "Read:\t%s\n", joinOrDash(tok.Read))
	fmt.Fprintf(w, "Read/write:\t%s\n", joinOrDash(tok.Write))
	fmt.Fprintf(w, "Created:\t%s\n", formatTime(tok.CreatedAt))
	fmt.Fprintf(w, "Rotated:\t%s\n", rotated)
	fmt.Fprintf(w, "Last used:\t%s\n", formatOptionalTime(tok.LastUsedAt, "never"))
	w.Flush()

	secrets, err := c.vault.List(ctx)
	if err != nil {
		return fmt.Errorf("Couldn't list secrets: %v", err)
	}
	var reach [][2]string
	for _, s := range secrets {
		switch {
		case tok.CanWrite(s.Key):
			reach = append(reach, [2]string{s.Key, "read/write"})
		case tok.CanRead(s.Key):
			reach = append(reach, [2]string{s.Key, "read"})
		}
	}

	if len(reach) == 0 {
		out("\nIt can't reach any secret yet.")
	} else {
		out(fmt.Sprintf("\nIt can reach %d %s:", len(reach), plural(len(reach), "secret", "secrets")))
		w = tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, r := range reach {
			fmt.Fprintf(w, "  %s\t%s\n", r[0], r[1])
		}
		w.Flush()
	}
	c.warnUnmatched(tok, secretKeys(secrets))

	events, err := c.tokens.History(ctx, name, 5)
	if err != nil {
		return fmt.Errorf("Couldn't read the token's history: %v", err)
	}
	if len(events) > 0 {
		out("\nRecent changes:")
		w = tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, e := range events {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", formatTime(e.OccurredAt), e.Action, e.Detail)
		}
		w.Flush()
	}
	return nil
}

func (c *CLI) tokenAllow(ctx context.Context, args []string) error {
	const form = "token allow <pattern> <name>... [--write]"

	write := false
	var rest []string
	for _, arg := range args {
		if arg == "--write" {
			write = true
		} else {
			rest = append(rest, arg)
		}
	}
	if len(rest) < 2 {
		return usageError{form: form}
	}
	pattern, names := unquote(rest[0]), rest[1:]

	already, err := c.tokens.Allow(ctx, pattern, write, names, source)
	if err != nil {
		return tokenError(strings.Join(names, ", "), err)
	}

	access := "read"
	if write {
		access = "read and change"
	}
	changed := without(names, already)
	if len(changed) > 0 {
		success(fmt.Sprintf("%s can now %s %s.", joinNames(changed), access, pattern))
	}
	if len(already) > 0 {
		info(fmt.Sprintf("%s already had %s; unchanged.", joinNames(already), pattern))
	}
	if secrets, err := c.vault.List(ctx); err == nil && !matchesAnyKey(pattern, secretKeys(secrets)) {
		warn(fmt.Sprintf("%s matches no secret yet.", pattern))
	}
	return nil
}

func (c *CLI) tokenDeny(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return usageError{form: "token deny <pattern> <name>..."}
	}
	pattern, names := unquote(args[0]), args[1:]

	notListed, err := c.tokens.Deny(ctx, pattern, names, source)
	if err != nil {
		return tokenError(strings.Join(names, ", "), err)
	}

	if changed := without(names, notListed); len(changed) > 0 {
		success(fmt.Sprintf("Removed %s from %s.", pattern, joinNames(changed)))
	}
	if len(notListed) > 0 {
		info(fmt.Sprintf("%s didn't list %s; unchanged.", joinNames(notListed), pattern))
	}

	// Removing an exact key doesn't help if a wildcard still covers it.
	for _, name := range names {
		tok, err := c.tokens.Get(ctx, name)
		if err != nil {
			continue
		}
		if tok.CanRead(pattern) {
			warn(fmt.Sprintf("%s can still reach %s through %s.", name, pattern, coveringPattern(tok, pattern)))
		}
	}
	return nil
}

func (c *CLI) tokenRotate(ctx context.Context, args []string) error {
	yes, rest := takeYesFlag(args)
	if len(rest) != 1 {
		return usageError{form: "token rotate <name> [--yes]"}
	}
	name := rest[0]

	if _, err := c.tokens.Get(ctx, name); err != nil {
		return tokenError(name, err)
	}
	if !yes {
		ok, answered := c.confirm(fmt.Sprintf("Give %s a new token? The current one stops working at once. (y/N)", name))
		if !answered {
			return errors.New("Rotate cancelled: no answer to the confirmation. Use --yes to rotate without asking.")
		}
		if !ok {
			info("Rotate cancelled.")
			return nil
		}
	}

	value, err := c.tokens.Rotate(ctx, name, source, "")
	if err != nil {
		return tokenError(name, err)
	}
	success(fmt.Sprintf("New token for %s. Copy it now; it can't be shown again:", name))
	out(value)
	warn(fmt.Sprintf("The old token stopped working. Put the new one where %s reads it, then restart %s.", name, name))
	return nil
}

func (c *CLI) tokenRevoke(ctx context.Context, args []string) error {
	yes, rest := takeYesFlag(args)
	if len(rest) != 1 {
		return usageError{form: "token revoke <name> [--yes]"}
	}
	name := rest[0]

	if _, err := c.tokens.Get(ctx, name); err != nil {
		return tokenError(name, err)
	}
	if !yes {
		ok, answered := c.confirm(fmt.Sprintf("Revoke %s's token? %s can't reach Cove until it gets a new one. (y/N)", name, name))
		if !answered {
			return errors.New("Revoke cancelled: no answer to the confirmation. Use --yes to revoke without asking.")
		}
		if !ok {
			info("Revoke cancelled.")
			return nil
		}
	}

	if err := c.tokens.Revoke(ctx, name, source); err != nil {
		return tokenError(name, err)
	}
	success(fmt.Sprintf("Revoked %s's token. Other projects are unaffected.", name))
	return nil
}

// describeReach tells how many secrets a new token can reach, and warns about
// patterns that match nothing (usually a typo).
func (c *CLI) describeReach(ctx context.Context, tok tokens.Token) {
	secrets, err := c.vault.List(ctx)
	if err != nil {
		return
	}
	keys := secretKeys(secrets)
	n := 0
	for _, key := range keys {
		if tok.CanRead(key) {
			n++
		}
	}
	info(fmt.Sprintf("It can reach %d %s now. See them with \"token show %s\".", n, plural(n, "secret", "secrets"), tok.Name))
	c.warnUnmatched(tok, keys)
}

func (c *CLI) warnUnmatched(tok tokens.Token, keys []string) {
	for _, p := range append(append([]string{}, tok.Read...), tok.Write...) {
		if !matchesAnyKey(p, keys) {
			warn(fmt.Sprintf("%s matches no secret yet.", p))
		}
	}
}

// tokenCompletions are the `token` subcommands, for Tab completion.
func tokenCompletions(*CLI) []string {
	return []string{"allow", "create", "deny", "list", "revoke", "rotate", "show"}
}

// tokenError turns an error from the token manager into a message.
func tokenError(name string, err error) error {
	if errors.Is(err, tokens.ErrNotFound) {
		return fmt.Errorf("No token named %s. See them with \"token list\".", missingName(err, name))
	}
	return fmt.Errorf("Couldn't change the token: %v", err)
}

// missingName picks the unknown name out of err when several were given.
func missingName(err error, names string) string {
	for _, name := range strings.Split(names, ", ") {
		if strings.Contains(err.Error(), fmt.Sprintf("%q", name)) {
			return fmt.Sprintf("%q", name)
		}
	}
	return fmt.Sprintf("%q", names)
}

func coveringPattern(tok tokens.Token, key string) string {
	for _, p := range append(append([]string{}, tok.Read...), tok.Write...) {
		if tokens.Matches(p, key) {
			return p
		}
	}
	return "?"
}

func secretKeys(secrets []vault.Secret) []string {
	keys := make([]string, len(secrets))
	for i, s := range secrets {
		keys[i] = s.Key
	}
	return keys
}

func matchesAnyKey(pattern string, keys []string) bool {
	for _, k := range keys {
		if tokens.Matches(pattern, k) {
			return true
		}
	}
	return false
}

func without(names []string, drop []string) []string {
	var keep []string
	for _, n := range names {
		found := false
		for _, d := range drop {
			found = found || n == d
		}
		if !found {
			keep = append(keep, n)
		}
	}
	return keep
}

// joinNames lists names in a sentence: "a", "a and b", "a, b and c".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// unquote removes one pair of surrounding quotes. A system shell removes them
// from `cove token create x --allow 'x.*'`; the cove> prompt doesn't, so a
// pattern copied from the docs still works there.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func joinOrDash(list []string) string {
	if len(list) == 0 {
		return "-"
	}
	return strings.Join(list, ", ")
}

func formatOptionalTime(t *time.Time, none string) string {
	if t == nil {
		return none
	}
	return formatTime(*t)
}
