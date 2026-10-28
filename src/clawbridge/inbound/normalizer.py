"""Normalizes raw inbound webhooks from diverse platforms into uniform ClawBridge events."""

import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any
from clawbridge.inbound.verifier import VerifiedWebhook


@dataclass(frozen=True)
class InboundEvent:
    """Canonical normalized inbound event dispatched to agent event streams."""
    event_id: str
    channel: str
    event_type: str
    topic: str
    delivery_id: str
    timestamp: str
    author: str | None = None
    content: str | None = None
    data: dict[str, Any] = field(default_factory=dict)


class EventNormalizer:
    """Transforms verified platform webhooks into structured InboundEvent instances."""

    def normalize(self, webhook: VerifiedWebhook) -> InboundEvent:
        now = datetime.now(timezone.utc).isoformat()
        event_id = f"evt_{uuid.uuid4().hex[:16]}"
        channel = webhook.channel.lower()
        payload = webhook.payload

        if channel == "slack":
            return self._normalize_slack(event_id, webhook.delivery_id, now, payload)
        elif channel == "jira":
            return self._normalize_jira(event_id, webhook.delivery_id, now, payload)
        else:
            return self._normalize_generic(channel, event_id, webhook.delivery_id, now, payload)

    def _normalize_slack(
        self,
        event_id: str,
        delivery_id: str,
        timestamp: str,
        payload: dict[str, Any],
    ) -> InboundEvent:
        p_type = payload.get("type", "unknown")
        if p_type == "url_verification":
            return InboundEvent(
                event_id=event_id,
                channel="slack",
                event_type="url_verification",
                topic="slack:system",
                delivery_id=delivery_id,
                timestamp=timestamp,
                author=None,
                content=None,
                data=payload,
            )

        if p_type == "event_callback":
            ev = payload.get("event", {})
            sub_type = ev.get("type", "event")
            return InboundEvent(
                event_id=event_id,
                channel="slack",
                event_type=sub_type,
                topic=f"slack:{sub_type}",
                delivery_id=delivery_id,
                timestamp=timestamp,
                author=ev.get("user"),
                content=ev.get("text"),
                data=payload,
            )

        return InboundEvent(
            event_id=event_id,
            channel="slack",
            event_type=p_type,
            topic=f"slack:{p_type}",
            delivery_id=delivery_id,
            timestamp=timestamp,
            data=payload,
        )

    def _normalize_jira(
        self,
        event_id: str,
        delivery_id: str,
        timestamp: str,
        payload: dict[str, Any],
    ) -> InboundEvent:
        raw_event = payload.get("webhookEvent", "jira:issue_updated")
        event_type = raw_event.split(":")[-1] if ":" in raw_event else raw_event
        issue = payload.get("issue", {})
        fields = issue.get("fields", {})

        author = None
        if "creator" in fields and isinstance(fields["creator"], dict):
            author = fields["creator"].get("name") or fields["creator"].get("displayName")

        content = fields.get("summary")

        return InboundEvent(
            event_id=event_id,
            channel="jira",
            event_type=event_type,
            topic=f"jira:{event_type}",
            delivery_id=delivery_id,
            timestamp=timestamp,
            author=author,
            content=content,
            data=payload,
        )

    def _normalize_generic(
        self,
        channel: str,
        event_id: str,
        delivery_id: str,
        timestamp: str,
        payload: dict[str, Any],
    ) -> InboundEvent:
        event_type = payload.get("action") or payload.get("event") or payload.get("type") or "event"
        return InboundEvent(
            event_id=event_id,
            channel=channel,
            event_type=str(event_type),
            topic=f"{channel}:{event_type}",
            delivery_id=delivery_id,
            timestamp=timestamp,
            author=payload.get("user") or payload.get("sender"),
            content=payload.get("text") or payload.get("message"),
            data=payload,
        )
