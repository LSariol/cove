package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStoreKeepsOtherValuesAndRestrictsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("APP_PORT=2101\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Store(path, "COVE_CLIENT_SECRET", "abc"); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(path)
	for _, want := range []string{"APP_PORT", "2101", "COVE_CLIENT_SECRET", "abc"} {
		if !strings.Contains(string(content), want) {
			t.Errorf(".env is missing %q:\n%s", want, content)
		}
	}

	// Windows has no owner/group/other permission bits to check.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %o, want 600", perm)
		}
	}
}

func TestStoreCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := Store(path, "VAULT_ENCRYPTION_KEY", "xyz"); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(path); !strings.Contains(string(content), "xyz") {
		t.Fatalf(".env = %q", content)
	}
}

func TestStoreDoesNotOverwriteUnparsableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "APP_PORT 2101\nCOVE_DATABASE_URL=postgres://keep-me\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Store(path, "COVE_CLIENT_SECRET", "abc"); err == nil {
		t.Fatal("Store succeeded on an unparsable file")
	}
	if content, _ := os.ReadFile(path); string(content) != original {
		t.Fatalf("file was changed:\n%s", content)
	}
}
