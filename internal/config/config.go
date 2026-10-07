// Package config loads Cove's settings from the environment and the .env file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/joho/godotenv"
)

// Config holds every setting Cove reads from the environment.
type Config struct {
	DatabaseURL        string // COVE_DATABASE_URL: runtime role (cove_app)
	MigrateDatabaseURL string // COVE_MIGRATE_DATABASE_URL: migrator role; empty disables migrations
	EncryptionKey      string // VAULT_ENCRYPTION_KEY: source of the AES key
	Port               string // APP_PORT
	EnvPath            string // APP_ENV_PATH: file that generated secrets are written to (default: the .env file that was loaded)
	MarkerDir          string // APP_MARKER_PATH: bootstrap state directory
	Env                string // APP_ENV: "DEV" or "PROD", shown in the CLI prompt

	// NewEncryptionKey (VAULT_NEW_ENCRYPTION_KEY) is only used by
	// `cove rotate-key`: the key to re-encrypt the vault with.
	NewEncryptionKey string

	// EventLogRetentionDays (COVE_EVENT_LOG_RETENTION_DAYS) removes read events
	// older than this many days, daily. Empty means keep everything.
	EventLogRetentionDays string

	// BootstrapAllowedCIDRs (COVE_BOOTSTRAP_ALLOWED_CIDRS) limits which
	// addresses may use the bootstrap endpoint: a comma-separated list of
	// networks and/or addresses, e.g. "172.18.0.0/16". Empty allows any.
	BootstrapAllowedCIDRs string
}

const defaultMarkerDir = "/app/vault/markers"

// minKeyLength is the shortest VAULT_ENCRYPTION_KEY that doesn't get a
// warning. A generated one is 45 characters.
const minKeyLength = 24

// envFiles are tried in order by Load, after APP_ENV_PATH; the first one that
// exists is loaded.
var envFiles = []string{".env", "/app/vault/.env"}

// permissionError explains a .env Cove isn't allowed to read. In Docker, Cove
// runs as user 10001, so a file created by root on the host is off limits
// until it's handed over.
func permissionError(path string) error {
	uid := os.Getuid()
	if uid < 0 { // Windows has no uids
		return fmt.Errorf("can't read %s: permission denied", path)
	}
	return fmt.Errorf("can't read %s: permission denied. Cove runs as user %d, which must own it. "+
		"In Docker, run this on the host, for the folder mounted at %s (e.g. /srv/server/storage/cove): chown -R %d:%d <folder>",
		path, uid, filepath.Dir(path), uid, os.Getgid())
}

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
			if errors.Is(err, fs.ErrPermission) {
				return Config{}, permissionError(path)
			}
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
		EncryptionKey:      os.Getenv("VAULT_ENCRYPTION_KEY"),
		NewEncryptionKey:   os.Getenv("VAULT_NEW_ENCRYPTION_KEY"),
		Port:               os.Getenv("APP_PORT"),
		EnvPath:            os.Getenv("APP_ENV_PATH"),
		MarkerDir:          os.Getenv("APP_MARKER_PATH"),
		Env:                os.Getenv("APP_ENV"),

		BootstrapAllowedCIDRs: os.Getenv("COVE_BOOTSTRAP_ALLOWED_CIDRS"),
		EventLogRetentionDays: os.Getenv("COVE_EVENT_LOG_RETENTION_DAYS"),
	}

	if cfg.EnvPath == "" {
		cfg.EnvPath = envFile
	}

	if cfg.MarkerDir == "" {
		cfg.MarkerDir = defaultMarkerDir
	}

	return cfg
}

// Validate returns an error for settings Cove must not run with.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("COVE_DATABASE_URL is not set. Set it to the cove_app connection string, e.g. postgres://cove_app:password@host:5432/cove_db")
	}
	if c.Port == "" {
		return errors.New("APP_PORT is not set. Set it to the port the API should listen on, e.g. 2100")
	}
	if _, err := c.BootstrapAllowed(); err != nil {
		return err
	}
	if _, err := c.RetentionDays(); err != nil {
		return err
	}
	return nil
}

// RetentionDays parses EventLogRetentionDays. 0 means retention is off.
func (c Config) RetentionDays() (int, error) {
	s := strings.TrimSpace(c.EventLogRetentionDays)
	if s == "" {
		return 0, nil
	}

	days, err := strconv.Atoi(s)
	if err != nil || days < 1 {
		return 0, fmt.Errorf("COVE_EVENT_LOG_RETENTION_DAYS must be a whole number of days (1 or more), got %q", s)
	}
	return days, nil
}

// BootstrapAllowed parses BootstrapAllowedCIDRs. A plain address counts as a
// network of one (e.g. 172.18.0.5 means 172.18.0.5/32). Empty means any
// address, and returns nil.
func (c Config) BootstrapAllowed() ([]netip.Prefix, error) {
	var prefixes []netip.Prefix

	for _, entry := range strings.Split(c.BootstrapAllowedCIDRs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if prefix, err := netip.ParsePrefix(entry); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		return nil, fmt.Errorf("COVE_BOOTSTRAP_ALLOWED_CIDRS: %q isn't a network or address (e.g. 172.18.0.0/16 or 172.18.0.5)", entry)
	}
	return prefixes, nil
}

// Warnings returns problems that are risky but shouldn't stop Cove. A short
// VAULT_ENCRYPTION_KEY is only a warning: refusing to start would lock an
// existing vault out of its own data. `cove rotate-key` replaces it.
func (c Config) Warnings() []string {
	var warnings []string
	if len(c.EncryptionKey) < minKeyLength {
		warnings = append(warnings, fmt.Sprintf("VAULT_ENCRYPTION_KEY is only %d characters; a key this short is guessable. "+
			"Replace it with `cove rotate-key` (see DOCUMENTATION.md, Rotating the vault key)", len(c.EncryptionKey)))
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

// Ensure makes sure VAULT_ENCRYPTION_KEY is set. If it's missing, one is
// generated, saved to the file at cfg.EnvPath, and set in the returned Config,
// so this run uses it straight away.
func Ensure(cfg Config) (Config, error) {

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

// saveError explains a failed save of a generated value. In Docker the .env
// file is mounted read-only, so the fix is to set the value in the file.
func saveError(name string, path string, err error) error {
	return fmt.Errorf("%s is not set, and a generated value couldn't be saved to %s: %w\n"+
		"Set %s in that file (in Docker, the host file mounted there), or make it writable", name, path, err, name)
}
