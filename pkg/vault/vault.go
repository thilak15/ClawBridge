package vault

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StoredCredential represents a decrypted static credential record.
type StoredCredential struct {
	ID        string         `json:"id"`
	Channel   string         `json:"channel"`
	AuthType  string         `json:"auth_type"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// OAuthTokenRecord represents a decrypted OAuth token record with expiration tracking.
type OAuthTokenRecord struct {
	ID        string         `json:"id"`
	Channel   string         `json:"channel"`
	ClientID  string         `json:"client_id"`
	TokenData map[string]any `json:"token_data"`
	ExpiresAt *time.Time     `json:"expires_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// IsExpired checks if the OAuth token has expired (or is missing expiration).
func (r *OAuthTokenRecord) IsExpired() bool {
	if r.ExpiresAt == nil {
		return false
	}
	// Expired if current time is after expiration
	return time.Now().After(*r.ExpiresAt)
}

// CredentialVault provides encrypted storage and single-flight token management.
type CredentialVault struct {
	db     *DatabaseManager
	cipher *EnvelopeCipher
	flight *SingleFlightGroup
}

// NewCredentialVault initializes a new CredentialVault.
func NewCredentialVault(db *DatabaseManager, cipher *EnvelopeCipher) *CredentialVault {
	return &CredentialVault{
		db:     db,
		cipher: cipher,
		flight: NewSingleFlightGroup(),
	}
}

func generateID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}

func (v *CredentialVault) encryptPayload(data any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	ciphertext, nonce, err := v.cipher.Encrypt(raw)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %w", err)
	}

	encodedNonce := base64.StdEncoding.EncodeToString(nonce)
	encodedCipher := base64.StdEncoding.EncodeToString(ciphertext)
	return fmt.Sprintf("%s:%s", encodedNonce, encodedCipher), nil
}

func (v *CredentialVault) decryptPayload(encryptedBlob string, target any) error {
	parts := strings.Split(encryptedBlob, ":")
	if len(parts) != 2 {
		return fmt.Errorf("malformed encrypted blob format")
	}

	nonce, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("failed to decode nonce: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	plaintext, err := v.cipher.Decrypt(ciphertext, nonce)
	if err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}

	if err := json.Unmarshal(plaintext, target); err != nil {
		return fmt.Errorf("failed to unmarshal decrypted json: %w", err)
	}

	return nil
}

// StoreCredential encrypts and saves static credentials in SQLite.
func (v *CredentialVault) StoreCredential(channel, authType string, data map[string]any) (string, error) {
	id := generateID("cred")
	encryptedBlob, err := v.encryptPayload(data)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	query := `
	INSERT INTO credentials (id, channel, auth_type, encrypted_blob, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?);
	`
	_, err = v.db.DB().Exec(query, id, channel, authType, encryptedBlob, now, now)
	if err != nil {
		return "", fmt.Errorf("failed to insert credential into db: %w", err)
	}

	return id, nil
}

// GetCredential retrieves and decrypts static credentials from SQLite.
func (v *CredentialVault) GetCredential(id string) (*StoredCredential, error) {
	query := `
	SELECT id, channel, auth_type, encrypted_blob, created_at, updated_at
	FROM credentials WHERE id = ?;
	`
	var cred StoredCredential
	var encryptedBlob, createdAtStr, updatedAtStr string

	err := v.db.DB().QueryRow(query, id).Scan(
		&cred.ID,
		&cred.Channel,
		&cred.AuthType,
		&encryptedBlob,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("credential '%s' not found", id)
		}
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	cred.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	cred.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)

	if err := v.decryptPayload(encryptedBlob, &cred.Data); err != nil {
		return nil, err
	}

	return &cred, nil
}

// StoreOAuthToken encrypts and saves an OAuth token record with TTL in seconds.
func (v *CredentialVault) StoreOAuthToken(channel, clientID string, tokenData map[string]any, expiresIn int) (string, error) {
	id := generateID("tok")
	encryptedBlob, err := v.encryptPayload(tokenData)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	var expiresAt *float64
	if expiresIn != 0 {
		expTime := float64(now.Add(time.Duration(expiresIn) * time.Second).Unix())
		expiresAt = &expTime
	}

	query := `
	INSERT INTO oauth_tokens (id, channel, client_id, encrypted_blob, expires_at, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	_, err = v.db.DB().Exec(query, id, channel, clientID, encryptedBlob, expiresAt, nowStr, nowStr)
	if err != nil {
		return "", fmt.Errorf("failed to insert oauth token into db: %w", err)
	}

	return id, nil
}

// GetOAuthToken retrieves and decrypts an OAuth token record from SQLite.
func (v *CredentialVault) GetOAuthToken(id string) (*OAuthTokenRecord, error) {
	query := `
	SELECT id, channel, client_id, encrypted_blob, expires_at, created_at, updated_at
	FROM oauth_tokens WHERE id = ?;
	`
	var rec OAuthTokenRecord
	var encryptedBlob, createdAtStr, updatedAtStr string
	var expiresAtFloat *float64

	err := v.db.DB().QueryRow(query, id).Scan(
		&rec.ID,
		&rec.Channel,
		&rec.ClientID,
		&encryptedBlob,
		&expiresAtFloat,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("oauth token '%s' not found", id)
		}
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	if expiresAtFloat != nil {
		expTime := time.Unix(int64(*expiresAtFloat), 0).UTC()
		rec.ExpiresAt = &expTime
	}

	if err := v.decryptPayload(encryptedBlob, &rec.TokenData); err != nil {
		return nil, err
	}

	return &rec, nil
}

// UpdateOAuthToken updates token data and expiration for an existing token ID.
func (v *CredentialVault) UpdateOAuthToken(id string, tokenData map[string]any, expiresIn int) error {
	encryptedBlob, err := v.encryptPayload(tokenData)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	var expiresAt *float64
	if expiresIn != 0 {
		expTime := float64(now.Add(time.Duration(expiresIn) * time.Second).Unix())
		expiresAt = &expTime
	}

	query := `
	UPDATE oauth_tokens
	SET encrypted_blob = ?, expires_at = ?, updated_at = ?
	WHERE id = ?;
	`
	_, err = v.db.DB().Exec(query, encryptedBlob, expiresAt, nowStr, id)
	if err != nil {
		return fmt.Errorf("failed to update oauth token in db: %w", err)
	}

	return nil
}

// GetOrRefreshOAuthToken returns a valid token or executes single-flight refresh if expired.
func (v *CredentialVault) GetOrRefreshOAuthToken(
	id string,
	refreshFn func(rec *OAuthTokenRecord) (map[string]any, error),
) (*OAuthTokenRecord, error) {
	rec, err := v.GetOAuthToken(id)
	if err != nil {
		return nil, err
	}

	if !rec.IsExpired() {
		return rec, nil
	}

	// Token is expired: single-flight deduplication across concurrent callers
	freshResult, err := v.flight.Execute(id, func() (any, error) {
		// Re-check token in case another caller already refreshed it while we waited
		latest, err := v.GetOAuthToken(id)
		if err == nil && !latest.IsExpired() {
			return latest, nil
		}

		newTokens, err := refreshFn(rec)
		if err != nil {
			return nil, fmt.Errorf("oauth refresh function failed: %w", err)
		}

		expiresIn := 3600
		if exp, ok := newTokens["expires_in"].(float64); ok {
			expiresIn = int(exp)
		} else if expInt, ok := newTokens["expires_in"].(int); ok {
			expiresIn = expInt
		}

		if err := v.UpdateOAuthToken(id, newTokens, expiresIn); err != nil {
			return nil, fmt.Errorf("failed to store refreshed token: %w", err)
		}

		return v.GetOAuthToken(id)
	})

	if err != nil {
		return nil, err
	}

	return freshResult.(*OAuthTokenRecord), nil
}
