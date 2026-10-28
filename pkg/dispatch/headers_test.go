package dispatch_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/vault"
)

func TestHeaderInjector_InjectBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://api.slack.com", nil)
	dispatch.InjectBearer(req, "xoxb-12345-token")

	auth := req.Header.Get("Authorization")
	if auth != "Bearer xoxb-12345-token" {
		t.Fatalf("expected 'Bearer xoxb-12345-token', got '%s'", auth)
	}
}

func TestHeaderInjector_InjectBasicAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://jira.company.com", nil)
	dispatch.InjectBasicAuth(req, "bot@company.com", "secret-token")

	auth := req.Header.Get("Authorization")
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@company.com:secret-token"))
	if auth != expected {
		t.Fatalf("expected '%s', got '%s'", expected, auth)
	}
}

func TestHeaderInjector_InjectAPIKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://api.x.com", nil)
	dispatch.InjectAPIKey(req, "X-API-Key", "api-key-999")

	if val := req.Header.Get("X-API-Key"); val != "api-key-999" {
		t.Fatalf("expected 'api-key-999', got '%s'", val)
	}
}

func TestHeaderInjector_InjectFromStoredCredential_BasicAuth(t *testing.T) {
	cred := &vault.StoredCredential{
		ID:       "cred_01",
		Channel:  "jira",
		AuthType: "basic_auth",
		Data: map[string]any{
			"email": "user@atlassian.net",
			"token": "api-token-xyz",
		},
	}

	req := httptest.NewRequest(http.MethodPost, "https://atlassian.net", nil)
	err := dispatch.InjectFromCredential(req, cred)
	if err != nil {
		t.Fatalf("unexpected error injecting cred: %v", err)
	}

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Fatalf("expected Basic auth header, got '%s'", auth)
	}
}

func TestHeaderInjector_InjectFromOAuthToken(t *testing.T) {
	rec := &vault.OAuthTokenRecord{
		ID:       "tok_01",
		Channel:  "slack",
		ClientID: "client_slack_001",
		TokenData: map[string]any{
			"access_token": "xoxb-real-slack-access-token",
		},
	}

	req := httptest.NewRequest(http.MethodPost, "https://slack.com", nil)
	err := dispatch.InjectFromOAuthToken(req, rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	auth := req.Header.Get("Authorization")
	if auth != "Bearer xoxb-real-slack-access-token" {
		t.Fatalf("expected 'Bearer xoxb-real-slack-access-token', got '%s'", auth)
	}
}
