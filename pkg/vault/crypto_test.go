package vault_test

import (
	"bytes"
	"testing"

	"clawbridge/pkg/vault"
)

func TestEnvelopeCipher_GenerateKey(t *testing.T) {
	key, err := vault.GenerateKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}

	key2, err := vault.GenerateKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}
	if bytes.Equal(key, key2) {
		t.Fatalf("expected keys to be distinct and high-entropy")
	}
}

func TestEnvelopeCipher_EncryptDecryptRoundtrip(t *testing.T) {
	key, err := vault.GenerateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cipher, err := vault.NewEnvelopeCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	original := []byte("super-secret-slack-token-xoxb-12345")
	ciphertext, nonce, err := cipher.Encrypt(original)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if bytes.Equal(original, ciphertext) {
		t.Fatalf("ciphertext matches original plaintext")
	}
	if len(nonce) != 12 {
		t.Fatalf("expected 12-byte GCM nonce, got %d", len(nonce))
	}

	decrypted, err := cipher.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(original, decrypted) {
		t.Fatalf("expected decrypted %s, got %s", original, decrypted)
	}
}

func TestEnvelopeCipher_TamperedCiphertextFails(t *testing.T) {
	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)

	original := []byte("confidential-jira-token")
	ciphertext, nonce, _ := cipher.Encrypt(original)

	// Tamper with last byte of ciphertext
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xFF

	_, err := cipher.Decrypt(tampered, nonce)
	if err == nil {
		t.Fatalf("expected decryption error on tampered ciphertext, got nil")
	}
}

func TestEnvelopeCipher_InvalidKeyLength(t *testing.T) {
	shortKey := []byte("too-short")
	_, err := vault.NewEnvelopeCipher(shortKey)
	if err == nil {
		t.Fatalf("expected error for invalid key length, got nil")
	}
}
