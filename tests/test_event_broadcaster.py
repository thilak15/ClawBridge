import asyncio
import pytest
from clawbridge.inbound.broadcaster import EventBroadcaster
from clawbridge.inbound.normalizer import InboundEvent


@pytest.mark.asyncio
async def test_subscriber_receives_matching_topic_events():
    broadcaster = EventBroadcaster()
    queue = await broadcaster.subscribe(topics=["slack:*", "jira:issue_created"])

    ev_slack = InboundEvent(
        event_id="ev_1",
        channel="slack",
        event_type="message",
        topic="slack:message",
        delivery_id="deliv_1",
        timestamp="2026-10-28T12:00:00Z",
        content="hello",
    )
    ev_twitter = InboundEvent(
        event_id="ev_2",
        channel="twitter",
        event_type="tweet",
        topic="twitter:tweet",
        delivery_id="deliv_2",
        timestamp="2026-10-28T12:00:01Z",
        content="tweet",
    )

    await broadcaster.publish(ev_slack)
    await broadcaster.publish(ev_twitter)

    # Should receive slack event, but not twitter
    received = await asyncio.wait_for(queue.get(), timeout=1.0)
    assert received.event_id == "ev_1"
    assert received.topic == "slack:message"
    assert queue.empty()


@pytest.mark.asyncio
async def test_sse_stream_formatting():
    broadcaster = EventBroadcaster()
    queue = await broadcaster.subscribe(topics=["*"])

    ev = InboundEvent(
        event_id="ev_99",
        channel="slack",
        event_type="message",
        topic="slack:message",
        delivery_id="deliv_99",
        timestamp="2026-10-28T12:00:00Z",
        content="hello stream",
    )
    await broadcaster.publish(ev)

    formatted = broadcaster.format_sse_message(ev)
    assert f"id: {ev.event_id}\n" in formatted
    assert f"event: {ev.topic}\n" in formatted
    assert "data: " in formatted


@pytest.mark.asyncio
async def test_unsubscribe_cleans_up_queue():
    broadcaster = EventBroadcaster()
    queue = await broadcaster.subscribe(topics=["*"])
    assert len(broadcaster._subscribers) == 1

    await broadcaster.unsubscribe(queue)
    assert len(broadcaster._subscribers) == 0

