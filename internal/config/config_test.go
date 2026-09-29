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
	unsetEnv(t, "APP_ENV_PATH")
	writeEnvFile(t, dir, "APP_PORT 2101\n")

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded on an invalid .env")
	}
	if !strings.Contains(err.Error(), "load .env") || strings.Contains(err.Error(), "no .env file found") {
		t.Fatalf("Load error = %q, want the parse error for .env", err)
	}
}

func TestLoadReportsMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, "APP_ENV_PATH")
	envFiles = []string{".env"}
	t.Cleanup(func() { envFiles = []string{".env", "/app/vault/.env"} })

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "no .env file found") {
		t.Fatalf("Load error = %v, want no .env file found", err)
	}
}

func TestLoadReadsValues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	unsetEnv(t, "APP_PORT", "APP_ENV_PATH")
	writeEnvFile(t, dir, "APP_PORT=2101\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "2101" {
		t.Fatalf("Port = %q, want 2101", cfg.Port)
	}
	if cfg.EnvPath != ".env" {
		t.Fatalf("EnvPath = %q, want the loaded file .env", cfg.EnvPath)
	}
}

func TestLoadPrefersAppEnvPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	unsetEnv(t, "APP_PORT")
	writeEnvFile(t, dir, "APP_PORT=1111\n")

	other := filepath.Join(t.TempDir(), "cove.env")
	if err := os.WriteFile(other, []byte("APP_PORT=2222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV_PATH", other)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "2222" || cfg.EnvPath != other {
		t.Fatalf("Port = %q, EnvPath = %q; want the APP_ENV_PATH file", cfg.Port, cfg.EnvPath)
	}
}

func TestMarkerDir(t *testing.T) {
	unsetEnv(t, "APP_MARKER_PATH", "APP_MARKER_DIR")
	if got := fromEnv(".env").MarkerDir; got != defaultMarkerDir {
		t.Errorf("default MarkerDir = %q", got)
	}

	t.Setenv("APP_MARKER_DIR", "/old/name")
	if got := fromEnv(".env").MarkerDir; got != "/old/name" {
		t.Errorf("MarkerDir with APP_MARKER_DIR = %q", got)
	}

	t.Setenv("APP_MARKER_PATH", "/new/name")
	if got := fromEnv(".env").MarkerDir; got != "/new/name" {
		t.Errorf("APP_MARKER_PATH should win, got %q", got)
	}
}

func TestEnsureGeneratesAndUsesMissingSecrets(t *testing.T) {
	path := writeEnvFile(t, t.TempDir(), "APP_PORT=2101\n")

	cfg, err := Ensure(Config{EnvPath: path})
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.ClientSecret) != 32 || len(cfg.EncryptionKey) != 45 {
		t.Fatalf("generated lengths = %d / %d, want 32 / 45", len(cfg.ClientSecret), len(cfg.EncryptionKey))
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{cfg.ClientSecret, cfg.EncryptionKey, "APP_PORT"} {
		if !strings.Contains(string(saved), want) {
			t.Errorf(".env is missing %q", want)
		}
	}
}

func TestEnsureKeepsExistingSecrets(t *testing.T) {
	path := writeEnvFile(t, t.TempDir(), "")

	in := Config{EnvPath: path, ClientSecret: "existing-client-secret", EncryptionKey: "existing-encryption-key"}
	out, err := Ensure(in)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("Ensure changed existing values: %+v", out)
	}
}
