// Package encryption encrypts secret values at rest and generates random secrets.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// Cipher encrypts and decrypts values with AES-256-GCM. The AES key is the
// SHA-256 hash of the configured VAULT_ENCRYPTION_KEY.
type Cipher struct {
	key [32]byte
}

// NewCipher returns a Cipher whose key is derived from secret.
func NewCipher(secret string) *Cipher {
	return &Cipher{key: sha256.Sum256([]byte(secret))}
}

// Fingerprint identifies the key without revealing it: the same key always
// gives the same fingerprint, and the key can't be worked out from it. Cove
// stores it to notice a vault being opened with the wrong key.
func (c *Cipher) Fingerprint() string {
	sum := sha256.Sum256(append([]byte("cove vault key fingerprint v1:"), c.key[:]...))
	return hex.EncodeToString(sum[:16])
}

func (c *Cipher) Encrypt(data string) (string, error) {

	// Generate AES cipher block from the encryption key
	block, err := aes.NewCipher(c.key[:])
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

func (c *Cipher) Decrypt(data string) (string, error) {

	//Decode the cipher from base64
	cipherText, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return "", err
	}

	// Create a GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Extract nonce and cipher text
	if len(cipherText) < gcm.NonceSize() {
		return "", errors.New("ciphertext is too short")
	}
	nonce, cipherText := cipherText[:gcm.NonceSize()], cipherText[gcm.NonceSize():]

	// decrypt the data
	plaintext, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil

}
