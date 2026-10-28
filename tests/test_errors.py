import pytest
from clawbridge.dispatch.errors import ErrorNormalizer, EgressReceipt
from clawbridge.dispatch.rate_limiter import RateLimitExceededError


def test_normalize_successful_http_response():
    receipt = ErrorNormalizer.normalize_response(
        channel="slack",
        status_code=200,
        body={"ok": True, "ts": "12345.678"},
        headers={"content-type": "application/json"}
    )
    assert receipt.status == "success"
    assert receipt.status_code == 200
    assert receipt.code is None
    assert receipt.retryable is False
    assert receipt.data == {"ok": True, "ts": "12345.678"}


def test_normalize_rate_limited_429_with_retry_after():
    receipt = ErrorNormalizer.normalize_response(
        channel="twitter",
        status_code=429,
        body={"errors": [{"message": "Rate limit exceeded"}]},
        headers={"retry-after": "15"}
    )
    assert receipt.status == "error"
    assert receipt.code == "RATE_LIMITED"
    assert receipt.status_code == 429
    assert receipt.retry_after_s == 15.0
    assert receipt.retryable is True


def test_normalize_client_error_400_is_not_retryable():
    receipt = ErrorNormalizer.normalize_response(
        channel="jira",
        status_code=400,
        body={"errorMessages": ["Field 'summary' cannot be empty"]},
        headers={}
    )
    assert receipt.status == "error"
    assert receipt.code == "BAD_REQUEST"
    assert receipt.retryable is False
    assert "summary" in receipt.message


def test_normalize_server_error_503_is_retryable():
    receipt = ErrorNormalizer.normalize_response(
        channel="slack",
        status_code=503,
        body={"error": "service_unavailable"},
        headers={"retry-after": "5"}
    )
    assert receipt.status == "error"
    assert receipt.code == "SERVICE_UNAVAILABLE"
    assert receipt.retryable is True
    assert receipt.retry_after_s == 5.0


def test_normalize_network_timeout_exception():
    import httpx
    exc = httpx.ReadTimeout("Socket timed out after 10.0s")
    receipt = ErrorNormalizer.normalize_exception("slack", exc)
    
    assert receipt.status == "error"
    assert receipt.code == "NETWORK_TIMEOUT"
    assert receipt.retryable is True
    assert "Socket timed out" in receipt.message


def test_normalize_internal_rate_limit_exception():
    exc = RateLimitExceededError(retry_after_s=2.5, channel="slack")
    receipt = ErrorNormalizer.normalize_exception("slack", exc)
    
    assert receipt.status == "error"
    assert receipt.code == "RATE_LIMITED"
    assert receipt.retry_after_s == 2.5
    assert receipt.retryable is True

