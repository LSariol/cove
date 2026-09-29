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

func TestEnsureExplainsReadOnlyEnvFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write read-only files")
	}
	path := writeEnvFile(t, t.TempDir(), "")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	_, err := Ensure(Config{EnvPath: path, EncryptionKey: "existing-encryption-key"})
	if err == nil {
		t.Fatal("Ensure succeeded with a read-only .env")
	}
	for _, want := range []string{"COVE_CLIENT_SECRET is not set", path, "Set COVE_CLIENT_SECRET in that file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't mention %q", err, want)
		}
	}
}

func TestValidateRejectsShortClientSecret(t *testing.T) {
	for _, secret := range []string{"Kept Empty", "short", strings.Repeat("x", minSecretLength-1)} {
		if err := (Config{ClientSecret: secret}).Validate(); err == nil {
			t.Errorf("Validate accepted client secret %q", secret)
		}
	}
	if err := (Config{ClientSecret: strings.Repeat("x", 32)}).Validate(); err != nil {
		t.Errorf("Validate rejected a 32-character client secret: %v", err)
	}
}

func TestShortEncryptionKeyIsOnlyAWarning(t *testing.T) {
	cfg := Config{ClientSecret: strings.Repeat("x", 32), EncryptionKey: "Kept Empty"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a short encryption key must not stop Cove: %v", err)
	}
	if len(cfg.Warnings()) != 1 {
		t.Fatalf("Warnings = %v, want one warning", cfg.Warnings())
	}

	cfg.EncryptionKey = strings.Repeat("k", 45)
	if len(cfg.Warnings()) != 0 {
		t.Fatalf("Warnings = %v for a 45-character key", cfg.Warnings())
	}
}
