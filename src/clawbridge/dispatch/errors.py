"""Normalized error taxonomy and execution receipt formatting."""

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class EgressReceipt:
    """Standardized result receipt returned to calling agents for all outbound tool calls."""
    status: str  # "success" or "error"
    channel: str
    status_code: int | None
    code: str | None  # e.g. RATE_LIMITED, UNAUTHORIZED, BAD_REQUEST, SERVICE_UNAVAILABLE
    message: str
    retry_after_s: float | None
    retryable: bool
    data: dict[str, Any] | None


class ErrorNormalizer:
    """Transforms raw HTTP responses and network exceptions into structured agent receipts."""

    @classmethod
    def _parse_retry_after(cls, headers: dict[str, str] | None) -> float | None:
        if not headers:
            return None
        # Normalize header keys to lowercase
        norm = {k.lower(): v for k, v in headers.items()}
        val = norm.get("retry-after")
        if not val:
            return None
        try:
            return float(val)
        except ValueError:
            return 30.0  # Fallback for complex RFC dates

    @classmethod
    def normalize_response(
        cls,
        channel: str,
        status_code: int,
        body: dict[str, Any] | None,
        headers: dict[str, str] | None = None,
    ) -> EgressReceipt:
        """Map downstream HTTP response to standard EgressReceipt."""
        retry_after = cls._parse_retry_after(headers)

        if 200 <= status_code < 300:
            return EgressReceipt(
                status="success",
                channel=channel,
                status_code=status_code,
                code=None,
                message=f"Request to channel '{channel}' succeeded with HTTP {status_code}",
                retry_after_s=None,
                retryable=False,
                data=body,
            )

        # Extract message from common API error shapes
        msg = f"Downstream '{channel}' returned HTTP {status_code}"
        if isinstance(body, dict):
            if "message" in body:
                msg = str(body["message"])
            elif "error" in body:
                msg = str(body["error"])
            elif "errorMessages" in body and body["errorMessages"]:
                msg = "; ".join(body["errorMessages"])
            elif "errors" in body and body["errors"] and isinstance(body["errors"][0], dict):
                msg = body["errors"][0].get("message", msg)

        if status_code == 429:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=status_code,
                code="RATE_LIMITED",
                message=msg,
                retry_after_s=retry_after or 10.0,
                retryable=True,
                data=body,
            )

        if status_code in (401, 403):
            code = "UNAUTHORIZED" if status_code == 401 else "FORBIDDEN"
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=status_code,
                code=code,
                message=msg,
                retry_after_s=None,
                retryable=False,
                data=body,
            )

        if status_code == 404:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=status_code,
                code="NOT_FOUND",
                message=msg,
                retry_after_s=None,
                retryable=False,
                data=body,
            )

        if 400 <= status_code < 500:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=status_code,
                code="BAD_REQUEST",
                message=msg,
                retry_after_s=None,
                retryable=False,
                data=body,
            )

        if 500 <= status_code < 600:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=status_code,
                code="SERVICE_UNAVAILABLE",
                message=msg,
                retry_after_s=retry_after or 5.0,
                retryable=True,
                data=body,
            )

        return EgressReceipt(
            status="error",
            channel=channel,
            status_code=status_code,
            code="DOWNSTREAM_ERROR",
            message=msg,
            retry_after_s=retry_after,
            retryable=False,
            data=body,
        )

    @classmethod
    def normalize_exception(cls, channel: str, exc: Exception) -> EgressReceipt:
        """Map network and library exceptions to standard EgressReceipt."""
        from clawbridge.dispatch.rate_limiter import RateLimitExceededError

        if isinstance(exc, RateLimitExceededError):
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=429,
                code="RATE_LIMITED",
                message=str(exc),
                retry_after_s=exc.retry_after_s,
                retryable=True,
                data=None,
            )

        exc_name = type(exc).__name__.lower()
        if "timeout" in exc_name:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=504,
                code="NETWORK_TIMEOUT",
                message=f"Network timeout contacting '{channel}': {exc}",
                retry_after_s=2.0,
                retryable=True,
                data=None,
            )

        if "connect" in exc_name or "network" in exc_name:
            return EgressReceipt(
                status="error",
                channel=channel,
                status_code=502,
                code="GATEWAY_ERROR",
                message=f"Failed to connect to '{channel}': {exc}",
                retry_after_s=5.0,
                retryable=True,
                data=None,
            )

        return EgressReceipt(
            status="error",
            channel=channel,
            status_code=500,
            code="INTERNAL_ERROR",
            message=f"Unexpected error executing tool on '{channel}': {exc}",
            retry_after_s=None,
            retryable=False,
            data=None,
        )
