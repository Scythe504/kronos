package utils_test

import (
	"os"
	"testing"

	"github.com/scythe504/kronos/internal/utils"
)

func TestEncryptDecryptEnv_Success(t *testing.T) {
	key := utils.GetEncryptionKey()
	plaintext := "API_KEY=secret_123456789\nDATABASE_URL=postgres://user:pass@localhost:5432/db"

	encrypted, err := utils.EncryptEnv(plaintext, key)
	if err != nil {
		t.Fatalf("unexpected encryption error: %v", err)
	}

	if encrypted == "" {
		t.Fatalf("expected non-empty ciphertext")
	}

	decrypted, err := utils.DecryptEnv(encrypted, key)
	if err != nil {
		t.Fatalf("unexpected decryption error: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("expected decrypted text '%s', got '%s'", plaintext, decrypted)
	}
}

func TestGetEncryptionKey_Length(t *testing.T) {
	key := utils.GetEncryptionKey()
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key length, got %d", len(key))
	}

	// Test with explicit environment variable set
	os.Setenv("SECRET_KEY", "short_key")
	defer os.Unsetenv("SECRET_KEY")

	envKey := utils.GetEncryptionKey()
	if len(envKey) != 32 {
		t.Fatalf("expected padded 32-byte key length, got %d", len(envKey))
	}
}

func TestDecryptEnv_InvalidBase64(t *testing.T) {
	key := utils.GetEncryptionKey()
	_, err := utils.DecryptEnv("invalid-base64!!!", key)
	if err == nil {
		t.Fatalf("expected error when decrypting invalid base64 string, got nil")
	}
}

func TestDecryptEnv_CiphertextTooShort(t *testing.T) {
	key := utils.GetEncryptionKey()
	// Valid base64, but decoded payload is shorter than nonce size (12 bytes)
	_, err := utils.DecryptEnv("aGVsbG8=", key) // "hello" in base64 (5 bytes)
	if err == nil {
		t.Fatalf("expected error for ciphertext shorter than nonce size, got nil")
	}
}

func TestDecryptEnv_MismatchedKey(t *testing.T) {
	key1 := []byte("01234567890123456789012345678901")
	key2 := []byte("99999999999999999999999999999999")
	plaintext := "SECRET_TOKEN=super_secret"

	encrypted, err := utils.EncryptEnv(plaintext, key1)
	if err != nil {
		t.Fatalf("unexpected encryption error: %v", err)
	}

	_, err = utils.DecryptEnv(encrypted, key2)
	if err == nil {
		t.Fatalf("expected error when decrypting with mismatched key, got nil")
	}
}

func TestEncryptEnv_InvalidKeyLength(t *testing.T) {
	invalidKey := []byte("short")
	_, err := utils.EncryptEnv("test", invalidKey)
	if err == nil {
		t.Fatalf("expected error for invalid AES key length, got nil")
	}
}
