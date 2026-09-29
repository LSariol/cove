package cli

import (
	"context"
	"testing"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func TestTabCompletion(t *testing.T) {
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("test-vault-key"))
	for _, key := range []string{"MYAPP_API_KEY", "MYAPP_DB_URL", "LIGHTHOUSE_GITHUB_PAT"} {
		if err := v.Create(context.Background(), key, "x", "test"); err != nil {
			t.Fatal(err)
		}
	}
	c := New(v, nil, Options{})

	cases := []struct {
		line string
		want string // "" means no completion
	}{
		{"gen", "generate "}, // command name
		{"ge", ""},           // get and generate: nothing more to complete (Tab again lists them)
		{"get LIGH", "get LIGHTHOUSE_GITHUB_PAT "}, // single key match
		{"get MY", "get MYAPP_"},                   // common prefix of two keys
		{"delete MYAPP_D", "delete MYAPP_DB_URL "}, // other key commands
		{"bootstrap o", "bootstrap open "},         // fixed options
		{"help up", "help update "},                // command names for help
		{"create MY", ""},                          // create takes a new key: no completion
		{"get NOPE", ""},                           // no match
		{"get MYAPP_API_KEY ", ""},                 // only the first argument completes
	}

	for _, tc := range cases {
		got, pos, ok := c.complete(tc.line, len(tc.line), '\t')
		if tc.want == "" {
			if ok {
				t.Errorf("complete(%q) = %q, want no completion", tc.line, got)
			}
			continue
		}
		if !ok || got != tc.want || pos != len(tc.want) {
			t.Errorf("complete(%q) = %q (pos %d, ok %v), want %q", tc.line, got, pos, ok, tc.want)
		}
	}

	if _, _, ok := c.complete("ge", 2, 'x'); ok {
		t.Error("a key other than Tab triggered completion")
	}
}
