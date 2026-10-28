from pathlib import Path
import time
import pytest
from clawbridge.inbound.verifier import WebhookVerifier, WebhookVerificationError
from clawbridge.vault.db import DatabaseManager


@pytest.fixture
def db_mgr(tmp_path: Path):
    db_file = tmp_path / "test_inbound.db"
    mgr = DatabaseManager(db_file)
    mgr.initialize()
    return mgr


def test_slack_hmac_sha256_verification_success(db_mgr):
    verifier = WebhookVerifier(db_mgr)
    secret = "slack_signing_secret_123"
    timestamp = str(int(time.time()))
    body = b'{"type":"event_callback","event":{"type":"message","text":"hello"}}'

    import hmac
    import hashlib
    sig_basestring = f"v0:{timestamp}:".encode("utf-8") + body
    signature = "v0=" + hmac.new(secret.encode("utf-8"), sig_basestring, hashlib.sha256).hexdigest()

    result = verifier.verify_slack(
        body=body,
        signature=signature,
        timestamp_header=timestamp,
        secret=secret,
        delivery_id="slack_deliv_001",
    )
    assert result.is_valid
    assert result.channel == "slack"
    assert result.delivery_id == "slack_deliv_001"


def test_stale_timestamp_rejected_against_replay(db_mgr):
    """Stress-test against [HIGH] Inbound Webhook Replay: timestamp > 300s old."""
    verifier = WebhookVerifier(db_mgr)
    secret = "secret"
    stale_timestamp = str(int(time.time()) - 305)  # 305 seconds old
    body = b'{"hello":"world"}'

    import hmac
    import hashlib
    sig_basestring = f"v0:{stale_timestamp}:".encode("utf-8") + body
    signature = "v0=" + hmac.new(secret.encode("utf-8"), sig_basestring, hashlib.sha256).hexdigest()

    with pytest.raises(WebhookVerificationError) as exc_info:
        verifier.verify_slack(
            body=body,
            signature=signature,
            timestamp_header=stale_timestamp,
            secret=secret,
            delivery_id="stale_deliv_001",
        )
    assert "timestamp" in str(exc_info.value).lower()


def test_delivery_id_deduplication(db_mgr):
    """Stress-test against [HIGH] Inbound Webhook Replay: duplicate delivery ID."""
    verifier = WebhookVerifier(db_mgr)
    secret = "secret"
    timestamp = str(int(time.time()))
    body = b'{"hello":"world"}'

    import hmac
    import hashlib
    sig_basestring = f"v0:{timestamp}:".encode("utf-8") + body
    signature = "v0=" + hmac.new(secret.encode("utf-8"), sig_basestring, hashlib.sha256).hexdigest()

    # First delivery succeeds
    res1 = verifier.verify_slack(
        body=body,
        signature=signature,
        timestamp_header=timestamp,
        secret=secret,
        delivery_id="dup_deliv_001",
    )
    assert res1.is_valid

    # Replay of exact delivery_id raises duplicate/replay error
    with pytest.raises(WebhookVerificationError) as exc_info:
        verifier.verify_slack(
            body=body,
            signature=signature,
            timestamp_header=timestamp,
            secret=secret,
            delivery_id="dup_deliv_001",
        )
    assert "duplicate" in str(exc_info.value).lower()


def test_github_and_generic_hmac_sha256_verification(db_mgr):
    verifier = WebhookVerifier(db_mgr)
    secret = "github_secret"
    body = b'{"action":"opened","issue":{"number":1}}'

    import hmac
    import hashlib
    sig = "sha256=" + hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()

    res = verifier.verify_hmac_sha256(
        channel="github",
        body=body,
        signature_header=sig,
        secret=secret,
        delivery_id="gh_deliv_123",
        prefix="sha256=",
    )
    assert res.is_valid
    assert res.channel == "github"
