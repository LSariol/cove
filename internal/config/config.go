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
	EnvPath            string // APP_ENV_PATH: file that generated secrets are written to
	MarkerDir          string // APP_MARKER_PATH: bootstrap marker directory
}

const defaultMarkerDir = "/app/vault/markers"

// envFiles are tried in order by Load; the first one that exists is loaded.
var envFiles = []string{".env", "/app/vault/.env"}

// Load loads environment variables from the first .env file that exists, for
// both dev (./.env) and Docker (/app/vault/.env). A file that exists but can't
// be read or parsed is an error; Load doesn't silently move on to the next one.
func Load() error {
	for _, path := range envFiles {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err := godotenv.Load(path); err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		return nil
	}

	return fmt.Errorf("no .env file found (looked for %s)", strings.Join(envFiles, ", "))
}

// FromEnv reads the Config from environment variables. Call Load first so the
// values from the .env file are included.
func FromEnv() Config {
	cfg := Config{
		DatabaseURL:        os.Getenv("COVE_DATABASE_URL"),
		MigrateDatabaseURL: os.Getenv("COVE_MIGRATE_DATABASE_URL"),
		ClientSecret:       os.Getenv("COVE_CLIENT_SECRET"),
		EncryptionKey:      os.Getenv("VAULT_ENCRYPTION_KEY"),
		Port:               os.Getenv("APP_PORT"),
		EnvPath:            os.Getenv("APP_ENV_PATH"),
		MarkerDir:          os.Getenv("APP_MARKER_PATH"),
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

// Ensure checks that COVE_CLIENT_SECRET and VAULT_ENCRYPTION_KEY are present.
// If any are missing, it will attempt to generate them and store them in the
// file at cfg.EnvPath.
func Ensure(cfg Config) error {

	if cfg.ClientSecret == "" {
		newValue, err := encryption.GenerateSecret(32)
		if err != nil {
			return err
		}

		if err := Store(cfg.EnvPath, "COVE_CLIENT_SECRET", newValue); err != nil {
			return err
		}
	}

	if cfg.EncryptionKey == "" {
		newValue, err := encryption.GenerateSecret(45)
		if err != nil {
			return err
		}

		if err := Store(cfg.EnvPath, "VAULT_ENCRYPTION_KEY", newValue); err != nil {
			return err
		}
	}

	return nil
}
