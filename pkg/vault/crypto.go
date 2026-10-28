package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	KeyLength   = 32 // AES-256
	NonceLength = 12 // Standard GCM nonce
)

// EnvelopeCipher handles AES-256-GCM encryption and decryption.
type EnvelopeCipher struct {
	aead cipher.AEAD
}

// GenerateKey creates a cryptographically secure 32-byte key for AES-256.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeyLength)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}
	return key, nil
}

// NewEnvelopeCipher initializes an EnvelopeCipher with a 32-byte master key.
func NewEnvelopeCipher(key []byte) (*EnvelopeCipher, error) {
	if len(key) != KeyLength {
		return nil, errors.New("key must be exactly 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher block: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM cipher: %w", err)
	}

	return &EnvelopeCipher{aead: aead}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a freshly generated random nonce.
func (c *EnvelopeCipher) Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, err error) {
	nonce = make([]byte, NonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext = c.aead.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt authenticates and decrypts ciphertext using AES-256-GCM with the provided nonce.
func (c *EnvelopeCipher) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != NonceLength {
		return nil, fmt.Errorf("invalid nonce length: expected %d, got %d", NonceLength, len(nonce))
	}

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption/authentication failed: %w", err)
	}

	return plaintext, nil
}
