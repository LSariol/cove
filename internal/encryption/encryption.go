package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
)

func getEncryptionKey() string {

	return os.Getenv("VAULT_ENCRYPTION_KEY")

}

func Encrypt(data string) (string, error) {
	encryptionKey := getEncryptionKey()

	// Generate AES cipher block from the encryption key
	key := sha256.Sum256([]byte(encryptionKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("new block: %w", err)
	}

	//create a GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}

	// Generate a random nonce (number used once) for GCM
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("make nonce: %w", err)
	}

	//Encrypt the data
	cipherText := gcm.Seal(nonce, nonce, []byte(data), nil)

	return base64.URLEncoding.EncodeToString(cipherText), nil

}

func Decrypt(data string) (string, error) {

	encryptionKey := getEncryptionKey()

	//Decode the cipher from base64
	cipherText, _ := base64.URLEncoding.DecodeString(data)

	// Extract the nonce from the beginning of the ciphertext
	key := sha256.Sum256([]byte(encryptionKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	// Create a GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Extract nonce and cipher text
	nonce, cipherText := cipherText[:gcm.NonceSize()], cipherText[gcm.NonceSize():]

	// decrypt the data
	plaintext, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil

}
