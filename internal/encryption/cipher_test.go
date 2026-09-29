package encryption

import (
	"strings"
	"testing"
)

func TestCipherRoundTrip(t *testing.T) {
	c := NewCipher("test-vault-key")

	for _, plaintext := range []string{"", "hello", "a value with spaces", strings.Repeat("x", 10_000)} {
		encrypted, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if plaintext != "" && strings.Contains(encrypted, plaintext) {
			t.Fatalf("Encrypt(%q) output contains the plaintext", plaintext)
		}

		decrypted, err := c.Decrypt(encrypted)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if decrypted != plaintext {
			t.Fatalf("round trip = %q, want %q", decrypted, plaintext)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	c := NewCipher("test-vault-key")

	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("encrypting the same value twice gave identical output")
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	encrypted, err := NewCipher("right-key").Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewCipher("wrong-key").Decrypt(encrypted); err == nil {
		t.Fatal("Decrypt with the wrong key succeeded")
	}
}

func TestGenerateSecret(t *testing.T) {
	s, err := GenerateSecret(45)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 45 {
		t.Fatalf("len = %d, want 45", len(s))
	}
	for _, r := range s {
		if !strings.ContainsRune(charset, r) {
			t.Fatalf("unexpected character %q", r)
		}
	}
}
