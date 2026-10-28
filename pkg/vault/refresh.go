package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// HTTPClient is an interface that matches *http.Client for test mockability.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// RefreshHTTPClient is the client used by DefaultTokenRefreshFn. Can be mocked in tests.
var RefreshHTTPClient HTTPClient = http.DefaultClient

// DefaultTokenRefreshFn handles proactive and JIT refreshing of OAuth tokens for supported channels.
func DefaultTokenRefreshFn(rec *OAuthTokenRecord) (map[string]any, error) {
	if rec == nil {
		return nil, fmt.Errorf("token record is nil")
	}

	channel := strings.ToLower(rec.Channel)
	switch channel {
	case "google", "calendar", "gmail":
		refreshToken, _ := rec.TokenData["refresh_token"].(string)
		if refreshToken == "" {
			return nil, fmt.Errorf("no refresh_token found for %s", channel)
		}
		clientID, _ := rec.TokenData["client_id"].(string)
		if clientID == "" {
			clientID = rec.ClientID
		}
		clientSecret, _ := rec.TokenData["client_secret"].(string)
		if clientSecret == "" {
			clientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		newTokens, err := RefreshGoogleOAuthToken(ctx, RefreshHTTPClient, clientID, clientSecret, refreshToken)
		if err != nil {
			return nil, err
		}

		// Preserve existing metadata (client_id, client_secret, refresh_token)
		for k, v := range rec.TokenData {
			if _, exists := newTokens[k]; !exists {
				newTokens[k] = v
			}
		}
		return newTokens, nil

	case "meta", "instagram", "facebook":
		clientID, _ := rec.TokenData["client_id"].(string)
		if clientID == "" {
			clientID = rec.ClientID
		}
		clientSecret, _ := rec.TokenData["client_secret"].(string)
		if clientSecret == "" {
			clientSecret = os.Getenv("META_APP_SECRET")
		}
		accessToken, _ := rec.TokenData["access_token"].(string)
		if accessToken == "" {
			return nil, fmt.Errorf("no access_token found for %s", channel)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		newTokens, err := RefreshMetaOAuthToken(ctx, RefreshHTTPClient, clientID, clientSecret, accessToken)
		if err != nil {
			return nil, err
		}
		for k, v := range rec.TokenData {
			if _, exists := newTokens[k]; !exists {
				newTokens[k] = v
			}
		}
		return newTokens, nil

	default:
		return rec.TokenData, nil
	}
}

// RefreshGoogleOAuthToken contacts Google token endpoint to refresh an access token.
func RefreshGoogleOAuthToken(ctx context.Context, client HTTPClient, clientID, clientSecret, refreshToken string) (map[string]any, error) {
	if client == nil {
		client = http.DefaultClient
	}

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read google refresh response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google refresh returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse google refresh response: %w", err)
	}

	return result, nil
}

// RefreshMetaOAuthToken contacts Meta Graph API to exchange for a new long-lived token.
func RefreshMetaOAuthToken(ctx context.Context, client HTTPClient, clientID, clientSecret, accessToken string) (map[string]any, error) {
	if client == nil {
		client = http.DefaultClient
	}

	u, err := url.Parse("https://graph.facebook.com/v21.0/oauth/access_token")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("grant_type", "fb_exchange_token")
	q.Set("client_id", clientID)
	q.Set("client_secret", clientSecret)
	q.Set("fb_exchange_token", accessToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read meta token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meta token refresh returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse meta token response: %w", err)
	}

	return result, nil
}

