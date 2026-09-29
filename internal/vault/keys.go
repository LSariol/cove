package vault

import "fmt"

const maxKeyLength = 256

// ValidateKey enforces that a secret key is not empty, contains only safe
// characters, and is within the length limit. The database enforces the same
// rule (secrets_key_format_check). Vault.Create checks it, so keys created from
// any entry point can always be read through the API.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key must not be empty")
	}
	if len(key) > maxKeyLength {
		return fmt.Errorf("key exceeds the maximum length of %d characters", maxKeyLength)
	}
	for _, c := range key {
		if !isValidKeyChar(c) {
			return fmt.Errorf("key contains invalid character %q; only letters, digits, hyphens, underscores, and dots are allowed", c)
		}
	}
	return nil
}

func isValidKeyChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.'
}
