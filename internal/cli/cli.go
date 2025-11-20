package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/server"
)

type CLI struct {
	DB *database.Database
}

func NewCLI(db *database.Database) *CLI {
	return &CLI{
		DB: db,
	}
}

func (c *CLI) StartCLI(ctx context.Context) {

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("Cove CLI> ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		c.parseCLI(ctx, strings.Fields(input))
	}
}

func (c *CLI) parseCLI(ctx context.Context, args []string) {

	if len(args) == 0 {
		return
	}

	switch args[0] {
	case "exit", "quit":
		fmt.Println("Shutting down Cove...")
		os.Exit(0)

	case "get", "g":

		if len(args) != 2 {
			warningLog("Get requires 1 additional argument.")
			infoLog("get <secret>")
			return
		}

		res, err := c.DB.GetSecret(ctx, args[1])
		if err != nil {
			errorLog(fmt.Sprintf("error getting %q: %v", args[1], err))
			return
		}

		successLog(fmt.Sprintf("%s : %s\n", res.Key, res.Value))
		return

	case "create", "c":

		if len(args) != 3 {
			warningLog("Create requires 2 additional arguments.")
			infoLog("create <secretName> <value>")
			return
		}

		var newSecret database.Secret = database.Secret{
			Key:   args[1],
			Value: args[2],
		}

		secret, err := c.DB.CreateSecret(ctx, newSecret)
		if err != nil {
			errorLog(err.Error())
			return
		}

		successLog(fmt.Sprintf("%s has been created at %q\n", secret.Key, secret.DateAdded))
		return

	case "delete", "d":

		if len(args) != 2 {
			warningLog("Delete requires 1 additional argument.")
			infoLog("delete <secretName>")
			return
		}

		secretName := args[1]
		warningLog(fmt.Sprintf("Are you sure you want to delete %q (y/N)", secretName))
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print("Cove CLI> ")

		if !scanner.Scan() {
			warningLog("Delete Cancelled")
			return
		}

		response := strings.ToLower(strings.TrimSpace(scanner.Text()))

		if response != "y" && response != "yes" {
			infoLog("Delete cancelled.")
			return
		}

		err := c.DB.DeleteSecret(ctx, args[1])
		if err != nil {
			errorLog(err.Error())
			return
		}

		successLog("Secret has been removed\n")
		return

	case "update", "u":

		if len(args) != 3 {
			warningLog("Update requires 2 additional arguments.")
			infoLog("update <secretName> <newValue>")
			return
		}

		var newSecret database.Secret = database.Secret{
			Key:   args[1],
			Value: args[2],
		}

		err := c.DB.UpdateSecret(ctx, newSecret)
		if err != nil {
			errorLog(err.Error())
			return
		}

		successLog("Secret has been updated.\n")
		return

	case "list", "l":

		switch len(args) {

		case 1:
			c.displayPublicVault(ctx, "", "all")
		case 2:
			c.displayPublicVault(ctx, args[1], "prefix")
		case 3:
			mode := strings.ToLower(args[2])
			if mode == "fuzzy" || mode == "f" {
				c.displayPublicVault(ctx, args[1], "fuzzy")
			} else {
				warningLog("List third argument must be 'fuzzy' or 'f'.")
				infoLog("list [term] [fuzzy|f]")
			}
		default:
			warningLog("List takes at most 2 additional arguments.")
			infoLog("list [term] [fuzzy|f]")
			return
		}

	case "bootstrap", "b":

		if len(args) != 2 {
			warningLog("Bootstrap requires 1 additional argument.")
			infoLog("bootstrap <clear/lock>")
			return
		}

		mode := strings.ToLower(args[1])

		switch mode {
		case "clear":
			if err := server.DeleteBootstrapMarker(); err != nil {
				errorLog(err.Error())
				return
			}

			successLog("Bootstrap marker cleared.\n")
			return

		case "lock":
			if err := server.CreateBootstrapMarker(); err != nil {
				errorLog(err.Error())
				return
			}

			successLog("Bootstrap marker created.\n")
			return

		default:
			warningLog("Invalid bootstrap argument; expected 'clear' or 'lock'")
			infoLog("bootstrap <clear|lock>")
			return
		}

	case "help", "h":

		s := `Available Commands:

  exit, quit
      Shuts down the program.

  get, g <secret>
      Displays the value of the specified secret.

  create, c <secret> <value>
      Creates a new secret and value to the vault.

  delete, d <secret>
      Removes the specified secret from the vault.

  update, u <secret> <new_value>
      Updates an existing secret in the vault.

  list, l
      Lists all secrets in the public vault.

  list, l <term>
      Lists secrets whose keys start with <term>.

  list, l <term> fuzzy
  list, l <term> f
      Lists secrets containing <term> (substring match).

  bootstrap, b <clear|lock>
      Enters or exits bootstrapping mode. This allows Lighthouse to obtain a
      one-time-use password without authenticating first.

  help, h
      Displays this help information.`

		infoLog("\n" + s)
		return

	default:
		warningLog(fmt.Sprintf("Unknown command %q", args[0]))
		infoLog("Type 'help' to see available commands.")
		return
	}

}

func (c *CLI) displayPublicVault(ctx context.Context, term string, mode string) {

	publicVault, err := c.DB.GetAllKeys(ctx)

	if err != nil {
		errorLog(fmt.Sprintf("GetAllKeys: %v", err))
		return
	}

	term = strings.ToLower(term)

	filteredPublicVault := publicVault[:0]

	for _, entry := range publicVault {
		keyLower := strings.ToLower(entry.Key)

		switch mode {
		case "all", "":
			filteredPublicVault = append(filteredPublicVault, entry)
		case "prefix":
			if strings.HasPrefix(keyLower, term) {
				filteredPublicVault = append(filteredPublicVault, entry)
			}
		case "fuzzy":
			if strings.Contains(keyLower, term) {
				filteredPublicVault = append(filteredPublicVault, entry)
			}
		default:
			filteredPublicVault = append(filteredPublicVault, entry)
		}
	}

	if len(filteredPublicVault) == 0 {
		infoLog("No secrets matched your query.")
		return
	}

	const (
		keyW    = 30
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

	for _, entry := range filteredPublicVault {
		row := fmt.Sprintf(
			"%-*s | %-*s | %-*s | %-*d | %-*d\n",
			keyW, entry.Key,
			dateW, formatTime(entry.DateAdded),
			dateW, formatTime(entry.LastModified),
			versW, entry.Version,
			pulledW, entry.TimesPulled,
		)
		plainLog(row)
	}
}

func plainLog(s string) {
	fmt.Print(s)
}

// Green
func successLog(s string) {
	fmt.Print("\033[32mCove CLI> " + s + "\033[0m")
}

// Yellow
func warningLog(s string) {
	fmt.Println("\033[33mCove CLI> " + s + "\033[0m")
}

// Red
func errorLog(s string) {
	fmt.Println("\033[31mCove CLI> " + s + "\033[0m")
}

// Cyan
func infoLog(s string) {
	fmt.Println("\033[36mCove CLI> " + s + "\033[0m")
}
