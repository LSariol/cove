package vault

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	valid := []string{
		"a",
		"MYAPP_API_KEY",
		"myapp.database_url",
		"lighthouse-github-pat",
		"v1.2.3",
		strings.Repeat("k", maxKeyLength),
	}
	for _, key := range valid {
		if err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q) = %v, want nil", key, err)
		}
	}

	invalid := []string{
		"has space",
		"db:url",
		"github/token",
		"query?x",
		"frag#x",
		"ünicode",
		strings.Repeat("k", maxKeyLength+1),
	}
	for _, key := range invalid {
		if err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) = nil, want an error", key)
		}
	}
}
