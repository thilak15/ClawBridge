"""Just-In-Time credential header injection for outbound HTTP requests."""

import base64
from typing import Mapping
from clawbridge.vault.vault import StoredCredential, OAuthTokenRecord


class HeaderInjector:
    """Safely attaches authentication headers from decrypted vault records without mutating inputs."""

    USER_AGENT = "ClawBridge/0.1.0 (Autonomous Agent Gateway; +https://github.com/thilak15/ClawBridge)"

    @classmethod
    def inject_auth(
        cls,
        credential_or_token: StoredCredential | OAuthTokenRecord,
        headers: Mapping[str, str] | None = None,
    ) -> dict[str, str]:
        """Produce a new headers dictionary containing the necessary downstream Authorization headers."""
        out = dict(headers or {})
        out.setdefault("User-Agent", cls.USER_AGENT)

        if isinstance(credential_or_token, OAuthTokenRecord):
            token_type = credential_or_token.token_type or "Bearer"
            out["Authorization"] = f"{token_type} {credential_or_token.access_token}"
            return out

        # Handling StoredCredential
        payload = credential_or_token.payload
        cred_type = credential_or_token.credential_type.lower()

        if cred_type == "basic_auth" or ("email" in payload and "token" in payload):
            email = payload.get("email", "")
            token = payload.get("token", "")
            raw = f"{email}:{token}".encode("utf-8")
            b64_val = base64.b64encode(raw).decode("ascii")
            out["Authorization"] = f"Basic {b64_val}"
        elif cred_type == "api_key" and "header_name" in payload:
            header_name = payload["header_name"]
            api_key = payload.get("api_key", payload.get("token", ""))
            out[header_name] = api_key
        elif "token" in payload:
            out["Authorization"] = f"Bearer {payload['token']}"
        elif "bearer_token" in payload:
            out["Authorization"] = f"Bearer {payload['bearer_token']}"

        return out
