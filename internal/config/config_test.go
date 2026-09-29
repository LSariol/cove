package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unsetEnv clears keys for the duration of the test. godotenv never overrides
// a variable that is already set, even to "", so they must be truly unset.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "") // registers restoring the original value
		os.Unsetenv(key)
	}
}

func writeEnvFile(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReportsParseErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeEnvFile(t, dir, "APP_PORT 2101\n")

	err := Load()
	if err == nil {
		t.Fatal("Load succeeded on an invalid .env")
	}
	if !strings.Contains(err.Error(), "load .env") || strings.Contains(err.Error(), "no .env file found") {
		t.Fatalf("Load error = %q, want the parse error for .env", err)
	}
}

func TestLoadReportsMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	envFiles = []string{".env"}
	t.Cleanup(func() { envFiles = []string{".env", "/app/vault/.env"} })

	err := Load()
	if err == nil || !strings.Contains(err.Error(), "no .env file found") {
		t.Fatalf("Load error = %v, want no .env file found", err)
	}
}

func TestLoadReadsValues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	unsetEnv(t, "APP_PORT")
	writeEnvFile(t, dir, "APP_PORT=2101\n")

	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("APP_PORT"); got != "2101" {
		t.Fatalf("APP_PORT = %q, want 2101", got)
	}
}
