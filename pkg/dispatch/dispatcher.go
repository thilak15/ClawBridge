package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"clawbridge/pkg/vault"
)

// DispatchRequest contains all information needed to execute an outbound request.
type DispatchRequest struct {
	Channel      string                                                    `json:"channel"`
	Action       string                                                    `json:"action"`
	Method       string                                                    `json:"method"`
	URL          string                                                    `json:"url"`
	CredentialID string                                                    `json:"credential_id,omitempty"`
	TokenID      string                                                    `json:"token_id,omitempty"`
	Headers      map[string]string                                         `json:"headers,omitempty"`
	Body         any                                                       `json:"body,omitempty"`
	RefreshFn    func(rec *vault.OAuthTokenRecord) (map[string]any, error) `json:"-"`
	MaxRetries   int                                                       `json:"max_retries,omitempty"`
}

// EgressDispatcher coordinates rate limiting, JIT credential injection, HTTP execution, and retries.
type EgressDispatcher struct {
	vault   *vault.CredentialVault
	limiter *TokenBucketLimiter
	client  *http.Client
}

// NewEgressDispatcher creates a new EgressDispatcher.
func NewEgressDispatcher(
	vault *vault.CredentialVault,
	limiter *TokenBucketLimiter,
	client *http.Client,
) *EgressDispatcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if limiter == nil {
		limiter = NewTokenBucketLimiter(nil)
	}

	return &EgressDispatcher{
		vault:   vault,
		limiter: limiter,
		client:  client,
	}
}

// Dispatch executes an outbound request with rate limiting, JIT credentials, and error recovery.
func (d *EgressDispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*EgressReceipt, error) {
	// 1. Check Rate Limiter
	allowed, retryAfter := d.limiter.Acquire(req.Channel)
	if !allowed {
		waitSec := retryAfter.Seconds()
		if waitSec < 1.0 {
			waitSec = 1.0
		}
		return &EgressReceipt{
			Status:      "error",
			Channel:     req.Channel,
			StatusCode:  http.StatusTooManyRequests,
			Code:        "RATE_LIMITED",
			Message:     fmt.Sprintf("Rate limit exceeded for channel '%s'. Retry after %.1fs", req.Channel, waitSec),
			RetryAfterS: waitSec,
			Retryable:   true,
			Data:        nil,
		}, nil
	}

	// 2. Resolve JIT Credentials from Vault
	var credRecord *vault.StoredCredential
	var tokenRecord *vault.OAuthTokenRecord
	var err error

	if req.TokenID != "" && d.vault != nil {
		if req.RefreshFn != nil {
			tokenRecord, err = d.vault.GetOrRefreshOAuthToken(req.TokenID, req.RefreshFn)
		} else {
			tokenRecord, err = d.vault.GetOAuthToken(req.TokenID)
		}
		if err != nil {
			return &EgressReceipt{
				Status:     "error",
				Channel:    req.Channel,
				StatusCode: http.StatusUnauthorized,
				Code:       "UNAUTHORIZED",
				Message:    fmt.Sprintf("Failed to resolve OAuth token: %v", err),
				Retryable:  false,
			}, nil
		}
	} else if req.CredentialID != "" && d.vault != nil {
		credRecord, err = d.vault.GetCredential(req.CredentialID)
		if err != nil {
			return &EgressReceipt{
				Status:     "error",
				Channel:    req.Channel,
				StatusCode: http.StatusUnauthorized,
				Code:       "UNAUTHORIZED",
				Message:    fmt.Sprintf("Failed to resolve credential: %v", err),
				Retryable:  false,
			}, nil
		}
	}

	// 3. Serialize Body
	var bodyBytes []byte
	if req.Body != nil {
		if b, ok := req.Body.([]byte); ok {
			bodyBytes = b
		} else if s, ok := req.Body.(string); ok {
			bodyBytes = []byte(s)
		} else {
			bodyBytes, err = json.Marshal(req.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
		}
	}

	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	// 4. Execution loop with exponential backoff & jitter
	var lastReceipt *EgressReceipt

	for attempt := 0; attempt < maxRetries; attempt++ {
		var bodyReader io.Reader
		if len(bodyBytes) > 0 {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create http request: %w", err)
		}

		if len(bodyBytes) > 0 && httpReq.Header.Get("Content-Type") == "" {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		// Inject JIT Authentication
		if tokenRecord != nil {
			_ = InjectFromOAuthToken(httpReq, tokenRecord)
		} else if credRecord != nil {
			_ = InjectFromCredential(httpReq, credRecord)
		}

		// Inject Custom Request Headers
		for k, v := range req.Headers {
			httpReq.Header.Set(k, v)
		}

		// Execute HTTP Call
		resp, err := d.client.Do(httpReq)
		if err != nil {
			lastReceipt = NormalizeError(req.Channel, err)
			if attempt < maxRetries-1 {
				jitter := time.Duration(rand.Intn(20)) * time.Millisecond
				backoff := time.Duration(10*(1<<attempt))*time.Millisecond + jitter
				time.Sleep(backoff)
				continue
			}
			return lastReceipt, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		receipt := NormalizeHTTPResponse(req.Channel, resp, respBody)

		// If downstream returned 429, record throttle in limiter
		if receipt.StatusCode == http.StatusTooManyRequests {
			d.limiter.RecordThrottle(req.Channel, time.Duration(receipt.RetryAfterS*float64(time.Second)))
		}

		// If transient server error (502, 503, 504), retry with backoff
		if receipt.StatusCode >= 500 && attempt < maxRetries-1 {
			lastReceipt = receipt
			jitter := time.Duration(rand.Intn(20)) * time.Millisecond
			backoff := time.Duration(15*(1<<attempt))*time.Millisecond + jitter
			time.Sleep(backoff)
			continue
		}

		return receipt, nil
	}

	return lastReceipt, nil
}
