"""Asynchronous publish-subscribe event broadcaster and SSE stream formatter."""

import asyncio
import fnmatch
import json
from dataclasses import asdict
from typing import Sequence
from clawbridge.inbound.normalizer import InboundEvent


class EventBroadcaster:
    """Manages agent SSE topic subscriptions and event dispatching."""

    def __init__(self) -> None:
        # Map of asyncio.Queue to set of subscribed topic patterns
        self._subscribers: dict[asyncio.Queue[InboundEvent], set[str]] = {}
        self._lock = asyncio.Lock()

    async def subscribe(self, topics: Sequence[str] | None = None) -> asyncio.Queue[InboundEvent]:
        """Subscribe to specific topics or wildcards (e.g., 'slack:*', '*')."""
        topic_set = set(topics or ["*"])
        queue: asyncio.Queue[InboundEvent] = asyncio.Queue()
        async with self._lock:
            self._subscribers[queue] = topic_set
        return queue

    async def unsubscribe(self, queue: asyncio.Queue[InboundEvent]) -> None:
        """Remove a subscriber queue."""
        async with self._lock:
            self._subscribers.pop(queue, None)

    def _matches(self, topic: str, patterns: set[str]) -> bool:
        """Evaluate topic against subscribed glob patterns."""
        for pattern in patterns:
            if pattern == "*" or pattern == topic:
                return True
            if fnmatch.fnmatch(topic, pattern):
                return True
        return False

    async def publish(self, event: InboundEvent) -> int:
        """Dispatch an event to all matching subscriber queues. Returns count of recipients."""
        delivered = 0
        async with self._lock:
            subscribers_snapshot = list(self._subscribers.items())

        for queue, patterns in subscribers_snapshot:
            if self._matches(event.topic, patterns):
                try:
                    queue.put_nowait(event)
                    delivered += 1
                except asyncio.QueueFull:
                    pass
        return delivered

    def format_sse_message(self, event: InboundEvent) -> str:
        """Format an InboundEvent as a Server-Sent Events (SSE) compliant message block."""
        data_json = json.dumps(asdict(event))
        return f"id: {event.event_id}\nevent: {event.topic}\ndata: {data_json}\n\n"
