import pytest
from clawbridge.auth.tokens import TokenManager


def test_generate_agent_token_format_and_entropy():
    raw_token, token_hash = TokenManager.generate_token(agent_id="agent_nemoclaw_01")
    assert raw_token.startswith("clw_agent_")
    assert len(raw_token) >= 40
    assert token_hash != raw_token
    assert len(token_hash) == 64  # SHA-256 hex digest


def test_token_verification_success():
    raw_token, token_hash = TokenManager.generate_token(agent_id="agent_openclaw_01")
    assert TokenManager.verify_token(raw_token, token_hash) is True


def test_token_verification_failure_with_tampered_token():
    raw_token, token_hash = TokenManager.generate_token(agent_id="agent_openclaw_01")
    tampered = raw_token[:-1] + ("A" if raw_token[-1] != "A" else "B")
    assert TokenManager.verify_token(tampered, token_hash) is False


def test_token_verification_failure_with_wrong_hash():
    raw_token, _ = TokenManager.generate_token(agent_id="agent_muse_01")
    _, other_hash = TokenManager.generate_token(agent_id="agent_muse_02")
    assert TokenManager.verify_token(raw_token, other_hash) is False
