package daemon

import (
	"context"
	"fmt"
	"time"

	"clawbridge/pkg/vault"
)

const (
	DefaultRefreshInterval  = 10 * time.Minute
	DefaultRefreshThreshold = 5 * time.Minute
)

// TokenRefresher handles proactive background refreshing of OAuth tokens before expiration.
type TokenRefresher struct {
	vault     *vault.CredentialVault
	interval  time.Duration
	threshold time.Duration
	refreshFn func(rec *vault.OAuthTokenRecord) (map[string]any, error)
}

// NewTokenRefresher creates a new TokenRefresher instance.
func NewTokenRefresher(
	v *vault.CredentialVault,
	interval, threshold time.Duration,
	refreshFn func(rec *vault.OAuthTokenRecord) (map[string]any, error),
) *TokenRefresher {
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	if threshold <= 0 {
		threshold = DefaultRefreshThreshold
	}
	return &TokenRefresher{
		vault:     v,
		interval:  interval,
		threshold: threshold,
		refreshFn: refreshFn,
	}
}

// CheckAndRefreshOnce scans for expiring tokens and refreshes them. Returns refreshed count.
func (r *TokenRefresher) CheckAndRefreshOnce(ctx context.Context) (int, error) {
	if r.vault == nil || r.refreshFn == nil {
		return 0, nil
	}

	expiring, err := r.vault.ListExpiringTokens(r.threshold)
	if err != nil {
		return 0, fmt.Errorf("failed to list expiring tokens: %w", err)
	}

	refreshedCount := 0
	for _, tok := range expiring {
		select {
		case <-ctx.Done():
			return refreshedCount, ctx.Err()
		default:
		}

		_, err := r.vault.ForceRefreshOAuthToken(tok.ID, r.refreshFn)
		if err == nil {
			refreshedCount++
		}
	}

	return refreshedCount, nil
}

// Start runs the proactive token refresher background ticker.
func (r *TokenRefresher) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = r.CheckAndRefreshOnce(ctx)
			}
		}
	}()
}
