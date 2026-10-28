"""Unified outbound egress proxy dispatching authenticated requests to downstream platform APIs."""

import asyncio
from typing import Any, Awaitable, Callable, Mapping
import httpx

from clawbridge.auth.authenticator import (
    AgentAuthenticator,
    AuthenticationError,
    PermissionDeniedError,
)
from clawbridge.dispatch.errors import ErrorNormalizer, EgressReceipt
from clawbridge.dispatch.headers import HeaderInjector
from clawbridge.dispatch.rate_limiter import (
    TokenBucketLimiter,
    RateLimitExceededError,
)
from clawbridge.vault.vault import CredentialVault, OAuthTokenRecord, StoredCredential


class EgressDispatcher:
    """Dispatches outbound requests with JIT authentication, rate limiting, and error normalization."""

    def __init__(
        self,
        authenticator: AgentAuthenticator,
        vault: CredentialVault,
        rate_limiter: TokenBucketLimiter | None = None,
        http_client: httpx.AsyncClient | None = None,
    ):
        self.authenticator = authenticator
        self.vault = vault
        self.rate_limiter = rate_limiter or TokenBucketLimiter()
        self._client = http_client

    async def _get_client(self) -> httpx.AsyncClient:
        if self._client is None or self._client.is_closed:
            self._client = httpx.AsyncClient(timeout=30.0)
        return self._client

    async def dispatch(
        self,
        bearer_header: str | None,
        channel: str,
        action: str,
        method: str,
        url: str,
        credential_id: str | None = None,
        token_id: str | None = None,
        headers: Mapping[str, str] | None = None,
        json: dict[str, Any] | None = None,
        params: Mapping[str, Any] | None = None,
        refresh_fn: Callable[[OAuthTokenRecord], Awaitable[dict[str, Any]]] | None = None,
        max_retries: int = 2,
    ) -> EgressReceipt:
        """Authenticate agent, attach JIT credentials, rate-limit, and execute downstream request."""
        # 1. Zero-Trust Authentication & ACL authorization
        try:
            agent_ctx = self.authenticator.authenticate_and_authorize(
                bearer_header=bearer_header,
                channel=channel,
                action=action,
            )
        except AuthenticationError as exc:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=401,
                code="UNAUTHORIZED",
                message=str(exc),
                retry_after_s=None,
                retryable=False,
                data=None,
            )
        except PermissionDeniedError as exc:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=403,
                code="FORBIDDEN",
                message=str(exc),
                retry_after_s=None,
                retryable=False,
                data=None,
            )

        # 2. Channel Token-Bucket Rate Limiter
        try:
            self.rate_limiter.acquire(channel)
        except RateLimitExceededError as exc:
            return ErrorNormalizer.normalize_exception(channel, exc)

        # 3. Resolve JIT credentials or valid OAuth tokens from Vault
        cred_record: StoredCredential | OAuthTokenRecord | None = None
        if token_id:
            try:
                if refresh_fn:
                    cred_record = await self.vault.get_or_refresh_oauth_token(token_id, refresh_fn)
                else:
                    cred_record = self.vault.get_oauth_token(token_id)
            except Exception as exc:
                return ErrorNormalizer.normalize_exception(channel, exc)
        elif credential_id:
            try:
                cred_record = self.vault.get_credential(credential_id)
            except Exception as exc:
                return ErrorNormalizer.normalize_exception(channel, exc)

        # 4. Attach JIT headers
        out_headers: dict[str, str] = dict(headers or {})
        if cred_record is not None:
            out_headers = HeaderInjector.inject_auth(cred_record, out_headers)

        # 5. Execute HTTP dispatch with exponential backoff for transient 5xx/network errors
        client = await self._get_client()
        attempt = 0
        receipt: EgressReceipt | None = None

        while attempt <= max_retries:
            try:
                resp = await client.request(
                    method=method,
                    url=url,
                    headers=out_headers,
                    json=json,
                    params=params,
                )

                # Parse JSON payload safely
                try:
                    resp_body = resp.json()
                except Exception:
                    resp_body = {"text": resp.text} if resp.text else None

                receipt = ErrorNormalizer.normalize_response(
                    channel=channel,
                    status_code=resp.status_code,
                    body=resp_body,
                    headers=dict(resp.headers),
                )

                # Check downstream 429
                if resp.status_code == 429 and receipt.retry_after_s:
                    self.rate_limiter.record_throttle(channel, receipt.retry_after_s)
                    break

                # Transient 5xx retry
                if resp.status_code in (502, 503, 504) and attempt < max_retries:
                    attempt += 1
                    backoff = (2 ** attempt) * 0.1
                    await asyncio.sleep(backoff)
                    continue

                break

            except Exception as exc:
                if attempt < max_retries:
                    attempt += 1
                    await asyncio.sleep(0.1)
                    continue
                receipt = ErrorNormalizer.normalize_exception(channel, exc)
                break

        assert receipt is not None

        # 6. Record immutable audit log
        self.vault.record_audit_event(
            event_type="egress_dispatch",
            status=receipt.status,
            agent_id=agent_ctx.agent_id,
            channel=channel,
            details={
                "action": action,
                "url": url,
                "method": method,
                "status_code": receipt.status_code,
                "code": receipt.code,
            },
        )

        return receipt
