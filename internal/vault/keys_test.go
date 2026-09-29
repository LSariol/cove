package vault

import (
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/tokens"
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
		"",
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

// A token pattern without "*" is a key, so the tokens package must accept
// exactly the keys the vault does.
func TestTokenPatternsFollowTheKeyRule(t *testing.T) {
	for _, key := range []string{"a", "MYAPP_KEY", "x.y-z", "", "has space", "a/b", "a:b", "é", strings.Repeat("k", 256), strings.Repeat("k", 257)} {
		keyOK := ValidateKey(key) == nil
		patternOK := tokens.ValidatePattern(key) == nil
		if keyOK != patternOK {
			t.Errorf("%q: valid key = %v, valid pattern = %v", key, keyOK, patternOK)
		}
	}
}
