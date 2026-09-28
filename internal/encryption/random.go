package encryption

import (
	"crypto/rand"
	"math/big"
)

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"0123456789"

// GenerateSecret returns a cryptographically secure random string
// of the given length, using characters from the predefined charset.
// It is suitable for generating API keys, client secrets, or encryption keys.
func GenerateSecret(length int) (string, error) {
	clientSecret := make([]byte, length)
	for i := range clientSecret {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		clientSecret[i] = charset[n.Int64()]
	}

	return string(clientSecret), nil
}
