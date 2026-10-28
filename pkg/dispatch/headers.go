package dispatch

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"clawbridge/pkg/vault"
)

// InjectBearer sets an Authorization: Bearer <token> header.
func InjectBearer(req *http.Request, token string) {
	clean := strings.TrimPrefix(token, "Bearer ")
	clean = strings.TrimSpace(clean)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", clean))
}

// InjectBasicAuth sets an Authorization: Basic <base64> header.
func InjectBasicAuth(req *http.Request, username, password string) {
	creds := fmt.Sprintf("%s:%s", username, password)
	encoded := base64.StdEncoding.EncodeToString([]byte(creds))
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", encoded))
}

// InjectAPIKey sets a custom header key-value pair.
func InjectAPIKey(req *http.Request, headerName, apiKey string) {
	req.Header.Set(headerName, apiKey)
}

// InjectFromCredential attaches authentication headers based on the stored credential type.
func InjectFromCredential(req *http.Request, cred *vault.StoredCredential) error {
	switch strings.ToLower(cred.AuthType) {
	case "basic_auth":
		user, _ := cred.Data["email"].(string)
		if user == "" {
			user, _ = cred.Data["username"].(string)
		}
		pass, _ := cred.Data["token"].(string)
		if pass == "" {
			pass, _ = cred.Data["password"].(string)
		}
		if user == "" || pass == "" {
			return fmt.Errorf("basic_auth requires email/username and token/password")
		}
		InjectBasicAuth(req, user, pass)
		return nil

	case "bot_token", "bearer", "token":
		token, _ := cred.Data["token"].(string)
		if token == "" {
			token, _ = cred.Data["access_token"].(string)
		}
		if token == "" {
			return fmt.Errorf("bearer/bot_token requires 'token' or 'access_token' in credential data")
		}
		InjectBearer(req, token)
		return nil

	case "api_key":
		header, _ := cred.Data["header"].(string)
		if header == "" {
			header = "X-API-Key"
		}
		key, _ := cred.Data["key"].(string)
		if key == "" {
			key, _ = cred.Data["token"].(string)
		}
		if key == "" {
			return fmt.Errorf("api_key requires 'key' or 'token' in credential data")
		}
		InjectAPIKey(req, header, key)
		return nil

	default:
		return fmt.Errorf("unsupported auth_type '%s'", cred.AuthType)
	}
}

// InjectFromOAuthToken attaches Bearer header from an OAuth access token record.
func InjectFromOAuthToken(req *http.Request, rec *vault.OAuthTokenRecord) error {
	token, _ := rec.TokenData["access_token"].(string)
	if token == "" {
		return fmt.Errorf("oauth token record '%s' missing 'access_token'", rec.ID)
	}
	InjectBearer(req, token)
	return nil
}
