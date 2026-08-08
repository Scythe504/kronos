package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// EncryptEnv encrypts plaintext using AES-256-GCM, returning a base64-encoded ciphertext.
func EncryptEnv(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to initialize cipher while encrypting: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to initialize gcm while encrypting: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	cipherText := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(cipherText), nil
}

// DecryptEnv decrypts a base64-encoded AES-256-GCM ciphertext, returning the original plaintext string.
func DecryptEnv(cryptoText string, key []byte) (string, error) {
	cipherText, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to initialize cipher while decrypting: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to initialize gcm while decrypting: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(cipherText) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, actualCipherText := cipherText[:nonceSize], cipherText[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCipherText, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt to plaintext: %w", err)
	}

	decryptedStr := string(plaintext)
	// If decrypted string is valid key=val or json object, return directly
	if strings.Contains(decryptedStr, "=") || strings.HasPrefix(decryptedStr, "{") {
		return decryptedStr, nil
	}
	// If decrypted string is itself base64-encoded (from double encoding), decode it once more
	if decoded, b64Err := base64.StdEncoding.DecodeString(decryptedStr); b64Err == nil && len(decoded) > 0 {
		return string(decoded), nil
	}

	return decryptedStr, nil
}

// GetEncryptionKey derives a 32-byte AES-256 key from the SECRET_KEY environment variable
// using SHA-256 so that keys of any length produce a valid fixed-size key.
// It panics if SECRET_KEY is not set, as operating without a real key is insecure.
func GetEncryptionKey() []byte {
	secret := os.Getenv("SECRET_KEY")
	if secret == "" {
		panic("SECRET_KEY environment variable is not set — cannot safely derive encryption key")
	}
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}