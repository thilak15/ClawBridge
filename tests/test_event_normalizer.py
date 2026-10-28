from pathlib import Path
import pytest
from clawbridge.inbound.normalizer import EventNormalizer
from clawbridge.inbound.verifier import VerifiedWebhook


def test_slack_message_event_normalization():
    normalizer = EventNormalizer()
    webhook = VerifiedWebhook(
        channel="slack",
        delivery_id="deliv_123",
        payload={
            "type": "event_callback",
            "event": {
                "type": "message",
                "user": "U12345",
                "text": "Hello bot!",
                "channel": "C999",
                "ts": "1700000000.123",
            },
        },
    )
    event = normalizer.normalize(webhook)
    assert event.topic == "slack:message"
    assert event.channel == "slack"
    assert event.event_type == "message"
    assert event.author == "U12345"
    assert event.content == "Hello bot!"
    assert event.delivery_id == "deliv_123"


def test_slack_url_verification_normalization():
    normalizer = EventNormalizer()
    webhook = VerifiedWebhook(
        channel="slack",
        delivery_id="deliv_challenge",
        payload={
            "type": "url_verification",
            "challenge": "test_challenge_token_abc",
        },
    )
    event = normalizer.normalize(webhook)
    assert event.topic == "slack:system"
    assert event.event_type == "url_verification"
    assert event.data.get("challenge") == "test_challenge_token_abc"


def test_jira_issue_event_normalization():
    normalizer = EventNormalizer()
    webhook = VerifiedWebhook(
        channel="jira",
        delivery_id="jira_deliv_456",
        payload={
            "webhookEvent": "jira:issue_created",
            "issue": {
                "key": "PROJ-101",
                "fields": {
                    "summary": "Fix login crash",
                    "creator": {"name": "alice"},
                },
            },
        },
    )
    event = normalizer.normalize(webhook)
    assert event.topic == "jira:issue_created"
    assert event.channel == "jira"
    assert event.event_type == "issue_created"
    assert event.author == "alice"
    assert event.content == "Fix login crash"


def test_generic_event_normalization():
    normalizer = EventNormalizer()
    webhook = VerifiedWebhook(
        channel="custom",
        delivery_id="cust_789",
        payload={"action": "ping", "user": "bob"},
    )
    event = normalizer.normalize(webhook)
    assert event.topic == "custom:ping"
    assert event.channel == "custom"
    assert event.event_type == "ping"
