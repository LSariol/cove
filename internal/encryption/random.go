package encryption

import (
	"crypto/rand"
	"math/big"
)

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"0123456789"

// GenerateSecret returns length random letters and digits from crypto/rand,
// for vault keys and `cove generate`.
func GenerateSecret(length int) (string, error) {
	secret := make([]byte, length)
	for i := range secret {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		secret[i] = charset[n.Int64()]
	}
	return string(secret), nil
}
