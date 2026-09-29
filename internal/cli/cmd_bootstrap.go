package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func (c *CLI) bootstrapCmd(ctx context.Context, args []string) {
	if len(args) != 2 {
		warningLog("Bootstrap requires 1 additional argument.")
		infoLog("bootstrap <clear/lock>")
		return
	}

	mode := strings.ToLower(args[1])

	switch mode {
	case "clear":
		if err := c.bootstrap.Clear(); err != nil {
			errorLog(err.Error())
			return
		}

		successLog("Bootstrap marker cleared.\n")

	case "lock":
		if err := c.bootstrap.Lock(); err != nil {
			if errors.Is(err, bootstrap.ErrLocked) {
				infoLog("Bootstrap is already locked.")
				return
			}
			errorLog(err.Error())
			return
		}

		successLog("Bootstrap marker created.\n")

	default:
		warningLog("Invalid bootstrap argument; expected 'clear' or 'lock'")
		infoLog("bootstrap <clear|lock>")
	}
}
