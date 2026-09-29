// Package config loads Cove's settings from the environment and the .env file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/joho/godotenv"
)

// Config holds every setting Cove reads from the environment. It is read once
// at startup and passed to the packages that need it.
type Config struct {
	DatabaseURL        string // COVE_DATABASE_URL: runtime role (cove_app)
	MigrateDatabaseURL string // COVE_MIGRATE_DATABASE_URL: migrator role; empty disables migrations
	ClientSecret       string // COVE_CLIENT_SECRET: bearer token clients must send
	EncryptionKey      string // VAULT_ENCRYPTION_KEY: source of the AES key
	Port               string // APP_PORT
	EnvPath            string // APP_ENV_PATH: file that generated secrets are written to (default: the .env file that was loaded)
	MarkerDir          string // APP_MARKER_PATH (or APP_MARKER_DIR): bootstrap marker directory
}

const defaultMarkerDir = "/app/vault/markers"

// minSecretLength is the shortest COVE_CLIENT_SECRET / VAULT_ENCRYPTION_KEY
// accepted. Generated values are 32 and 45 characters.
const minSecretLength = 24

// envFiles are tried in order by Load, after APP_ENV_PATH; the first one that
// exists is loaded.
var envFiles = []string{".env", "/app/vault/.env"}

// Load reads the .env file and returns the Config.
//
// The file is APP_ENV_PATH if that is set in the environment (as in
// docker-compose), otherwise the first of ./.env and /app/vault/.env that
// exists. A file that exists but can't be read or parsed is an error; Load
// doesn't silently move on to the next one.
func Load() (Config, error) {
	candidates := envFiles
	if path := os.Getenv("APP_ENV_PATH"); path != "" {
		candidates = append([]string{path}, envFiles...)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err := godotenv.Load(path); err != nil {
			return Config{}, fmt.Errorf("can't read %s: %w (each line must be NAME=value)", path, err)
		}
		return fromEnv(path), nil
	}

	return Config{}, fmt.Errorf("no .env file found (looked for %s). Copy .env.example to .env to get started", strings.Join(candidates, ", "))
}

// fromEnv reads the Config from environment variables. envFile is the .env
// file that was loaded, used when APP_ENV_PATH isn't set.
func fromEnv(envFile string) Config {
	cfg := Config{
		DatabaseURL:        os.Getenv("COVE_DATABASE_URL"),
		MigrateDatabaseURL: os.Getenv("COVE_MIGRATE_DATABASE_URL"),
		ClientSecret:       os.Getenv("COVE_CLIENT_SECRET"),
		EncryptionKey:      os.Getenv("VAULT_ENCRYPTION_KEY"),
		Port:               os.Getenv("APP_PORT"),
		EnvPath:            os.Getenv("APP_ENV_PATH"),
		MarkerDir:          os.Getenv("APP_MARKER_PATH"),
	}

	if cfg.EnvPath == "" {
		cfg.EnvPath = envFile
	}

	// APP_MARKER_DIR is an older name that docker-compose.yml used to set.
	if cfg.MarkerDir == "" {
		cfg.MarkerDir = os.Getenv("APP_MARKER_DIR")
	}
	if cfg.MarkerDir == "" {
		cfg.MarkerDir = defaultMarkerDir
	}

	return cfg
}

// Validate returns an error for settings Cove must not run with. Call it after
// Ensure, which fills in missing secrets.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("COVE_DATABASE_URL is not set. Set it to the cove_app connection string, e.g. postgres://cove_app:password@host:5432/cove_db")
	}
	if c.Port == "" {
		return errors.New("APP_PORT is not set. Set it to the port the API should listen on, e.g. 2100")
	}
	if len(c.ClientSecret) < minSecretLength {
		return fmt.Errorf("COVE_CLIENT_SECRET is too short (%d characters, need at least %d). "+
			"Leave it empty to have Cove generate one, then update your clients", len(c.ClientSecret), minSecretLength)
	}
	return nil
}

// Warnings returns problems that are risky but shouldn't stop Cove. A short
// VAULT_ENCRYPTION_KEY is only a warning: refusing to start would lock an
// existing vault out of its own data, and the key can't be changed without
// re-encrypting every secret.
func (c Config) Warnings() []string {
	var warnings []string
	if len(c.EncryptionKey) < minSecretLength {
		warnings = append(warnings, fmt.Sprintf("VAULT_ENCRYPTION_KEY is only %d characters; a key this short is guessable. "+
			"For a new vault, leave it empty so Cove generates one. For an existing vault, don't change it until key rotation exists", len(c.EncryptionKey)))
	}
	return warnings
}

// Store sets key in the .env file at path, keeping its other values. The file
// holds the vault key, so it is left readable by its owner only (0600). A file
// that exists but can't be parsed is not overwritten.
func Store(path string, key string, value string) error {

	envs, err := godotenv.Read(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("can't parse %s, so it was left unchanged: %w", path, err)
	}
	if envs == nil {
		envs = make(map[string]string)
	}
	envs[key] = value

	content, err := godotenv.Marshal(envs)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// OpenFile's mode only applies to new files; tighten an existing one too.
	return os.Chmod(path, 0o600)
}

// Ensure makes sure COVE_CLIENT_SECRET and VAULT_ENCRYPTION_KEY are set. Any
// that are missing are generated, saved to the file at cfg.EnvPath, and set in
// the returned Config, so this run uses them straight away.
func Ensure(cfg Config) (Config, error) {

	if cfg.ClientSecret == "" {
		newValue, err := encryption.GenerateSecret(32)
		if err != nil {
			return cfg, err
		}

		if err := Store(cfg.EnvPath, "COVE_CLIENT_SECRET", newValue); err != nil {
			return cfg, saveError("COVE_CLIENT_SECRET", cfg.EnvPath, err)
		}
		cfg.ClientSecret = newValue
	}

	if cfg.EncryptionKey == "" {
		newValue, err := encryption.GenerateSecret(45)
		if err != nil {
			return cfg, err
		}

		if err := Store(cfg.EnvPath, "VAULT_ENCRYPTION_KEY", newValue); err != nil {
			return cfg, saveError("VAULT_ENCRYPTION_KEY", cfg.EnvPath, err)
		}
		cfg.EncryptionKey = newValue
	}

	return cfg, nil
}

// saveError explains a failed save of a generated secret. In Docker the .env
// file is mounted read-only, so the fix is to set the value in the file.
func saveError(name string, path string, err error) error {
	return fmt.Errorf("%s is not set, and a generated value couldn't be saved to %s: %w\n"+
		"Set %s in that file (in Docker, the host file mounted there), or make it writable", name, path, err, name)
}
