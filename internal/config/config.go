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
			return Config{}, fmt.Errorf("load %s: %w", path, err)
		}
		return fromEnv(path), nil
	}

	return Config{}, fmt.Errorf("no .env file found (looked for %s)", strings.Join(candidates, ", "))
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

// Store a new key value pair into the .env file
func Store(file string, key string, value string) error {

	envs, _ := godotenv.Read(file)
	envs[key] = value

	return godotenv.Write(envs, file)
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
			return cfg, err
		}
		cfg.ClientSecret = newValue
	}

	if cfg.EncryptionKey == "" {
		newValue, err := encryption.GenerateSecret(45)
		if err != nil {
			return cfg, err
		}

		if err := Store(cfg.EnvPath, "VAULT_ENCRYPTION_KEY", newValue); err != nil {
			return cfg, err
		}
		cfg.EncryptionKey = newValue
	}

	return cfg, nil
}
