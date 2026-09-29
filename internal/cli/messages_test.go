package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/vault"
)

func TestSecretErrorExplainsKnownCases(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("read secret: %w", vault.ErrNotFound), `No secret named "app.key".`},
		{fmt.Errorf("insert secret: %w", vault.ErrAlreadyExists), `already exists. Use "update"`},
		{fmt.Errorf("get secret: %w", vault.ErrDecrypt), "VAULT_ENCRYPTION_KEY may have changed"},
		{errors.New("connection refused"), `Couldn't get "app.key": connection refused`},
	}

	for _, tc := range cases {
		if got := secretError("get", "app.key", tc.err).Error(); !strings.Contains(got, tc.want) {
			t.Errorf("secretError(%v) = %q, want it to contain %q", tc.err, got, tc.want)
		}
	}
}

func TestHelpUsesKeyAndValue(t *testing.T) {
	c := New(nil, nil, Options{})
	for _, cmd := range c.commands {
		for _, u := range cmd.usages {
			for _, form := range u.forms {
				if strings.Contains(form, "<secret") || strings.Contains(form, "_value") {
					t.Errorf("%s uses old placeholder %q", cmd.names[0], form)
				}
			}
		}
	}
}
