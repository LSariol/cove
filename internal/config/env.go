package config

import (
	"fmt"
	"os"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/joho/godotenv"
)

// Loads environment variables regardless of dev or prod environments.
func Load() error {

	if err := godotenv.Load(".env"); err == nil {
		return nil
	}

	if err := godotenv.Load("/app/vault/.env"); err == nil {
		return nil
	}

	return fmt.Errorf("no .env file found")
}

// Store a new key value pair into the .env file
func Store(file string, key string, value string) error {

	envs, _ := godotenv.Read(file)
	envs[key] = value

	return godotenv.Write(envs, file)
}

// Ensure checks that all required environment variables such as
// COVE_CLIENT_SECRET and VAULT_ENCRYPTION_KEY are present.
// If any are missing, it will attempt to generate and store them.
func Ensure() error {

	clientSecretKey := "COVE_CLIENT_SECRET"
	encryptionSecretKey := "VAULT_ENCRYPTION_KEY"
	fp := os.Getenv("APP_ENV_PATH")

	clientSecret := os.Getenv(clientSecretKey)
	if clientSecret == "" {
		newValue, err := encryption.GenerateSecret(32)
		if err != nil {
			return err
		}

		if err := Store(fp, clientSecretKey, newValue); err != nil {
			return err
		}
	}

	encryptionKey := os.Getenv(encryptionSecretKey)
	if encryptionKey == "" {
		newValue, err := encryption.GenerateSecret(45)
		if err != nil {
			return err
		}

		if err := Store(fp, encryptionSecretKey, newValue); err != nil {
			return err
		}
	}

	return nil
}
