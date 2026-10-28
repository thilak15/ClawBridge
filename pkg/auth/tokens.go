package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateAgentToken creates a high-entropy bearer token for an agent and its SHA-256 hash.
func GenerateAgentToken() (rawToken, tokenHash string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate random token bytes: %w", err)
	}

	rawToken = fmt.Sprintf("clw_agent_%s", hex.EncodeToString(b))
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash = hex.EncodeToString(hash[:])
	return rawToken, tokenHash, nil
}

// VerifyAgentToken performs constant-time verification of a raw agent token against a stored hash.
func VerifyAgentToken(rawToken, tokenHash string) bool {
	hash := sha256.Sum256([]byte(rawToken))
	computedHex := hex.EncodeToString(hash[:])
	return hmac.Equal([]byte(computedHex), []byte(tokenHash))
}
