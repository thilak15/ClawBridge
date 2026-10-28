import base64
import pytest
from clawbridge.dispatch.headers import HeaderInjector
from clawbridge.vault.vault import StoredCredential, OAuthTokenRecord
from authlib.oauth2.rfc6749 import OAuth2Token  # type: ignore[import-untyped]


def test_inject_oauth_bearer_token():
    authlib_tok = OAuth2Token({"access_token": "slack_xoxb_12345", "token_type": "Bearer"})
    token_rec = OAuthTokenRecord(
        id="tok_1",
        channel="slack",
        client_id="slack_app",
        token=authlib_tok,
        created_at="2026-10-28",
        updated_at="2026-10-28"
    )
    
    headers = HeaderInjector.inject_auth(token_rec, {"Content-Type": "application/json"})
    assert headers["Authorization"] == "Bearer slack_xoxb_12345"
    assert headers["Content-Type"] == "application/json"
    assert "User-Agent" in headers


def test_inject_jira_basic_auth():
    cred = StoredCredential(
        id="cred_jira",
        channel="jira",
        credential_type="basic_auth",
        payload={"email": "agent@corp.com", "token": "jira_secret_token"},
        metadata={},
        created_at="2026-10-28",
        updated_at="2026-10-28"
    )
    
    headers = HeaderInjector.inject_auth(cred, {})
    expected_b64 = base64.b64encode(b"agent@corp.com:jira_secret_token").decode("ascii")
    assert headers["Authorization"] == f"Basic {expected_b64}"


def test_inject_custom_api_key():
    cred = StoredCredential(
        id="cred_custom",
        channel="custom_api",
        credential_type="api_key",
        payload={"header_name": "X-API-Key", "api_key": "custom_key_val"},
        metadata={},
        created_at="2026-10-28",
        updated_at="2026-10-28"
    )
    
    headers = HeaderInjector.inject_auth(cred, {"Accept": "application/json"})
    assert headers["X-API-Key"] == "custom_key_val"
    assert headers["Accept"] == "application/json"


def test_caller_headers_not_mutated():
    original = {"User-Data": "intact"}
    token_rec = OAuthTokenRecord(
        id="tok_1",
        channel="slack",
        client_id="slack_app",
        token=OAuth2Token({"access_token": "abc"}),
        created_at="2026-10-28",
        updated_at="2026-10-28"
    )
    res = HeaderInjector.inject_auth(token_rec, original)
    assert "Authorization" not in original
    assert "Authorization" in res
