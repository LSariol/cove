package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/LSariol/Cove/internal/config"
	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
)

// minKeyLength is the shortest new vault key rotate-key accepts.
const minKeyLength = 24

// runRotateKey handles `cove rotate-key`: it re-encrypts every stored value
// from VAULT_ENCRYPTION_KEY to VAULT_NEW_ENCRYPTION_KEY in one transaction.
// Both keys come from the .env file, so the new key is saved on disk before
// anything is encrypted with it.
func runRotateKey(cfg config.Config) {
	ctx := context.Background()

	switch {
	case cfg.MigrateDatabaseURL == "":
		fatal(errors.New("rotate-key needs COVE_MIGRATE_DATABASE_URL (the cove_migrator connection): the event log can only be rewritten by the schema owner"))
	case cfg.EncryptionKey == "":
		fatal(errors.New("VAULT_ENCRYPTION_KEY is not set"))
	case cfg.NewEncryptionKey == "":
		suggestion, err := encryption.GenerateSecret(48)
		if err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, `cove rotate-key: VAULT_NEW_ENCRYPTION_KEY is not set.

1. Add this line to Cove's .env file (keep VAULT_ENCRYPTION_KEY as it is):

VAULT_NEW_ENCRYPTION_KEY=%s

2. Run cove rotate-key again.
`, suggestion)
		os.Exit(1)
	case len(cfg.NewEncryptionKey) < minKeyLength:
		fatal(fmt.Errorf("VAULT_NEW_ENCRYPTION_KEY is only %d characters; use at least %d", len(cfg.NewEncryptionKey), minKeyLength))
	case cfg.NewEncryptionKey == cfg.EncryptionKey:
		fatal(errors.New("VAULT_NEW_ENCRYPTION_KEY is the same as VAULT_ENCRYPTION_KEY"))
	}

	oldCipher := encryption.NewCipher(cfg.EncryptionKey)
	newCipher := encryption.NewCipher(cfg.NewEncryptionKey)

	result, err := database.RotateKey(ctx, cfg.MigrateDatabaseURL, oldCipher.Fingerprint(), newCipher.Fingerprint(),
		func(value string) (string, error) {
			plain, err := oldCipher.Decrypt(value)
			if err != nil {
				return "", err
			}
			return newCipher.Encrypt(plain)
		})
	if err != nil {
		fatal(fmt.Errorf("rotate-key: %w", err))
	}

	fmt.Fprintf(os.Stderr, `✓ Re-encrypted %d secrets and %d event log values with VAULT_NEW_ENCRYPTION_KEY.

Now, in Cove's .env file:
  1. Set VAULT_ENCRYPTION_KEY to the value of VAULT_NEW_ENCRYPTION_KEY.
  2. Delete the VAULT_NEW_ENCRYPTION_KEY line.
Then restart Cove (docker compose up -d --force-recreate cove).

Until then, a running Cove can't read or write secrets. The old key no longer
opens this vault; keep it only with the backup you took before rotating.
`, result.Secrets, result.EventValues)
}

// keyError explains why the server won't start with this key.
func keyError(cfg config.Config, db *database.Database, err error) error {
	if !errors.Is(err, vault.ErrWrongKey) {
		return err
	}

	// Rotated, but the .env wasn't updated yet?
	if cfg.NewEncryptionKey != "" {
		if k, found, _ := db.VaultKey(context.Background()); found && k.Fingerprint == encryption.NewCipher(cfg.NewEncryptionKey).Fingerprint() {
			return errors.New("the vault was re-encrypted with VAULT_NEW_ENCRYPTION_KEY: in the .env file, set VAULT_ENCRYPTION_KEY to that value and delete the VAULT_NEW_ENCRYPTION_KEY line, then start Cove again")
		}
	}
	return errors.New("VAULT_ENCRYPTION_KEY isn't the key this vault is encrypted with. Check the .env file: it must be the key the vault was created with (or last rotated to)")
}
