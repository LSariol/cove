package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func (c *CLI) bootstrapCmd(ctx context.Context, args []string) error {
	const form = "bootstrap <clear|lock>"
	if len(args) != 2 {
		return usageError{form: form}
	}

	switch strings.ToLower(args[1]) {
	case "clear":
		if err := c.bootstrap.Clear(); err != nil {
			return fmt.Errorf("Couldn't open the bootstrap endpoint: %v", err)
		}

		successLog("Bootstrap endpoint opened. The next request to /v0/bootstrap/lighthouse receives the client token.\n")

	case "lock":
		if err := c.bootstrap.Lock(); err != nil {
			if errors.Is(err, bootstrap.ErrLocked) {
				infoLog("The bootstrap endpoint is already locked.")
				return nil
			}
			return fmt.Errorf("Couldn't lock the bootstrap endpoint: %v", err)
		}

		successLog("Bootstrap endpoint locked.\n")

	default:
		return usageError{reason: fmt.Sprintf("Unknown bootstrap option %q.", args[1]), form: form}
	}

	return nil
}
