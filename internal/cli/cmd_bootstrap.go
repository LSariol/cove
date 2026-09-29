package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func (c *CLI) bootstrapCmd(ctx context.Context, args []string) {
	if len(args) != 2 {
		usageLog("bootstrap <clear|lock>")
		return
	}

	switch strings.ToLower(args[1]) {
	case "clear":
		if err := c.bootstrap.Clear(); err != nil {
			errorLog(fmt.Sprintf("Couldn't open the bootstrap endpoint: %v", err))
			return
		}

		successLog("Bootstrap endpoint opened. The next request to /v0/bootstrap/lighthouse receives the client token.\n")

	case "lock":
		if err := c.bootstrap.Lock(); err != nil {
			if errors.Is(err, bootstrap.ErrLocked) {
				infoLog("The bootstrap endpoint is already locked.")
				return
			}
			errorLog(fmt.Sprintf("Couldn't lock the bootstrap endpoint: %v", err))
			return
		}

		successLog("Bootstrap endpoint locked.\n")

	default:
		warningLog(fmt.Sprintf("Unknown bootstrap option %q.", args[1]))
		usageLog("bootstrap <clear|lock>")
	}
}
