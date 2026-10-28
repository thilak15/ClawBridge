package inbound

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"clawbridge/pkg/vault"
)

// VerifiedWebhook contains payload and delivery metadata for a cryptographically verified webhook.
type VerifiedWebhook struct {
	Channel    string         `json:"channel"`
	DeliveryID string         `json:"delivery_id"`
	Payload    map[string]any `json:"payload"`
	IsValid    bool           `json:"is_valid"`
}

// WebhookVerifier validates HMAC signatures, checks freshness, and deduplicates delivery IDs.
type WebhookVerifier struct {
	dbMgr          *vault.DatabaseManager
	maxSkewSeconds int
}

// NewWebhookVerifier creates a new WebhookVerifier.
func NewWebhookVerifier(dbMgr *vault.DatabaseManager, maxSkewSeconds int) *WebhookVerifier {
	if maxSkewSeconds <= 0 {
		maxSkewSeconds = 300
	}
	return &WebhookVerifier{
		dbMgr:          dbMgr,
		maxSkewSeconds: maxSkewSeconds,
	}
}

func (v *WebhookVerifier) checkAndRecordDelivery(channel, deliveryID string, body []byte) error {
	if v.dbMgr == nil {
		return nil
	}

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	eventID := fmt.Sprintf("ev_%s", hex.EncodeToString(b))

	now := time.Now().UTC().Format(time.RFC3339)
	payloadStr := string(body)

	query := `
	INSERT INTO webhook_events (id, channel, delivery_id, payload_json, status, received_at)
	VALUES (?, ?, ?, ?, 'received', ?);
	`
	_, err := v.dbMgr.DB().Exec(query, eventID, channel, deliveryID, payloadStr, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") ||
			strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return fmt.Errorf("duplicate webhook delivery ID '%s' detected (replay attack rejected)", deliveryID)
		}
		return fmt.Errorf("failed to record webhook delivery in db: %w", err)
	}

	return nil
}

// VerifySlack verifies incoming Slack webhooks: signature v0=<hex>, 300s freshness, and deduplication.
func (v *WebhookVerifier) VerifySlack(
	body []byte,
	signature string,
	timestampHeader string,
	secret string,
	deliveryID string,
) (*VerifiedWebhook, error) {
	if timestampHeader == "" {
		return nil, fmt.Errorf("missing slack timestamp header")
	}

	tsInt, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid slack timestamp header '%s': %w", timestampHeader, err)
	}

	// 300s freshness window
	now := time.Now().Unix()
	if math.Abs(float64(now-tsInt)) > float64(v.maxSkewSeconds) {
		return nil, fmt.Errorf("slack webhook timestamp %s outside %ds freshness window", timestampHeader, v.maxSkewSeconds)
	}

	// Compute signature: v0=HMAC-SHA256(secret, "v0:<timestamp>:<bodyBytes>")
	basestring := fmt.Sprintf("v0:%s:", timestampHeader)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(basestring))
	mac.Write(body)
	computedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(computedSig)) {
		return nil, fmt.Errorf("slack hmac-sha256 signature mismatch")
	}

	// Resolve delivery ID if not provided
	resolvedID := deliveryID
	if resolvedID == "" {
		resolvedID = fmt.Sprintf("slack_%d_%s", tsInt, computedSig[len(computedSig)-12:])
	}

	// Check and record delivery ID against replay attacks
	if err := v.checkAndRecordDelivery("slack", resolvedID, body); err != nil {
		return nil, err
	}

	var payload map[string]any
	_ = json.Unmarshal(body, &payload)

	return &VerifiedWebhook{
		Channel:    "slack",
		DeliveryID: resolvedID,
		Payload:    payload,
		IsValid:    true,
	}, nil
}

// VerifyHMACSHA256 verifies generic or GitHub style HMAC-SHA256 webhook signatures.
func (v *WebhookVerifier) VerifyHMACSHA256(
	channel string,
	body []byte,
	signatureHeader string,
	secret string,
	deliveryID string,
	prefix string,
) (*VerifiedWebhook, error) {
	if signatureHeader == "" {
		return nil, fmt.Errorf("missing signature header for channel '%s'", channel)
	}

	sigVal := signatureHeader
	if prefix != "" && strings.HasPrefix(signatureHeader, prefix) {
		sigVal = signatureHeader[len(prefix):]
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	computedHex := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sigVal), []byte(computedHex)) {
		return nil, fmt.Errorf("hmac-sha256 signature mismatch for channel '%s'", channel)
	}

	if deliveryID != "" {
		if err := v.checkAndRecordDelivery(channel, deliveryID, body); err != nil {
			return nil, err
		}
	}

	var payload map[string]any
	_ = json.Unmarshal(body, &payload)

	return &VerifiedWebhook{
		Channel:    channel,
		DeliveryID: deliveryID,
		Payload:    payload,
		IsValid:    true,
	}, nil
}

// VerifyTelegramSecret validates incoming Telegram webhooks using the X-Telegram-Bot-Api-Secret-Token header.
func (v *WebhookVerifier) VerifyTelegramSecret(
	body []byte,
	secretTokenHeader string,
	expectedSecret string,
	deliveryID string,
) (*VerifiedWebhook, error) {
	if secretTokenHeader == "" {
		return nil, fmt.Errorf("missing X-Telegram-Bot-Api-Secret-Token header")
	}

	if secretTokenHeader != expectedSecret {
		return nil, fmt.Errorf("telegram secret token mismatch")
	}

	if deliveryID != "" {
		if err := v.checkAndRecordDelivery("telegram", deliveryID, body); err != nil {
			return nil, err
		}
	}

	var payload map[string]any
	_ = json.Unmarshal(body, &payload)

	return &VerifiedWebhook{
		Channel:    "telegram",
		DeliveryID: deliveryID,
		Payload:    payload,
		IsValid:    true,
	}, nil
}

// VerifyWhatsApp validates incoming WhatsApp Cloud API webhooks using Meta HMAC-SHA256 signature (X-Hub-Signature-256).
func (v *WebhookVerifier) VerifyWhatsApp(
	body []byte,
	signatureHeader string,
	appSecret string,
	deliveryID string,
) (*VerifiedWebhook, error) {
	return v.VerifyHMACSHA256("whatsapp", body, signatureHeader, appSecret, deliveryID, "sha256=")
}
