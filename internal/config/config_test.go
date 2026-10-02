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
	if !strings.Contains(err.Error(), "can't read .env") || strings.Contains(err.Error(), "no .env file found") {
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
	unsetEnv(t, "APP_MARKER_PATH")
	if got := fromEnv(".env").MarkerDir; got != defaultMarkerDir {
		t.Errorf("default MarkerDir = %q", got)
	}

	t.Setenv("APP_MARKER_PATH", "/new/name")
	if got := fromEnv(".env").MarkerDir; got != "/new/name" {
		t.Errorf("MarkerDir with APP_MARKER_PATH = %q", got)
	}
}

func TestEnsureGeneratesAndUsesMissingSecrets(t *testing.T) {
	path := writeEnvFile(t, t.TempDir(), "APP_PORT=2101\n")

	cfg, err := Ensure(Config{EnvPath: path})
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.EncryptionKey) != 45 {
		t.Fatalf("generated key length = %d, want 45", len(cfg.EncryptionKey))
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{cfg.EncryptionKey, "APP_PORT"} {
		if !strings.Contains(string(saved), want) {
			t.Errorf(".env is missing %q", want)
		}
	}
}

func TestEnsureKeepsExistingSecrets(t *testing.T) {
	path := writeEnvFile(t, t.TempDir(), "")

	in := Config{EnvPath: path, EncryptionKey: "existing-encryption-key"}
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

	_, err := Ensure(Config{EnvPath: path})
	if err == nil {
		t.Fatal("Ensure succeeded with a read-only .env")
	}
	for _, want := range []string{"VAULT_ENCRYPTION_KEY is not set", path, "Set VAULT_ENCRYPTION_KEY in that file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't mention %q", err, want)
		}
	}
}

// validConfig returns a Config that passes Validate with no warnings.
func validConfig() Config {
	return Config{
		DatabaseURL:   "postgres://cove_app:pass@localhost:5432/cove_db",
		Port:          "2100",
		EncryptionKey: strings.Repeat("k", 45),
	}
}

func TestValidateRequiresDatabaseURLAndPort(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	noDB := validConfig()
	noDB.DatabaseURL = ""
	if err := noDB.Validate(); err == nil || !strings.Contains(err.Error(), "COVE_DATABASE_URL") {
		t.Errorf("missing COVE_DATABASE_URL: %v", err)
	}

	noPort := validConfig()
	noPort.Port = ""
	if err := noPort.Validate(); err == nil || !strings.Contains(err.Error(), "APP_PORT") {
		t.Errorf("missing APP_PORT: %v", err)
	}
}

func TestShortEncryptionKeyIsOnlyAWarning(t *testing.T) {
	cfg := validConfig()
	cfg.EncryptionKey = "Kept Empty"
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

func TestBootstrapAllowed(t *testing.T) {
	cfg := validConfig()

	if got, err := cfg.BootstrapAllowed(); err != nil || got != nil {
		t.Fatalf("empty = %v, %v; want nil (any address)", got, err)
	}

	cfg.BootstrapAllowedCIDRs = " 172.18.0.0/16, 10.0.0.159 ,fd00::/8"
	got, err := cfg.BootstrapAllowed()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"172.18.0.0/16", "10.0.0.159/32", "fd00::/8"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}

	cfg.BootstrapAllowedCIDRs = "172.18.0.0/16,docker-network"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "docker-network") {
		t.Fatalf("Validate with a bad entry = %v", err)
	}
}

func TestRetentionDays(t *testing.T) {
	cfg := validConfig()
	if days, err := cfg.RetentionDays(); err != nil || days != 0 {
		t.Fatalf("empty = %d, %v; want 0 (off)", days, err)
	}

	cfg.EventLogRetentionDays = " 90 "
	if days, err := cfg.RetentionDays(); err != nil || days != 90 {
		t.Fatalf("90 = %d, %v", days, err)
	}

	for _, bad := range []string{"0", "-5", "90d", "ninety"} {
		cfg.EventLogRetentionDays = bad
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate accepted retention %q", bad)
		}
	}
}
