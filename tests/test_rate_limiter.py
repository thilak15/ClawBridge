import time
import pytest
from clawbridge.dispatch.rate_limiter import (
    TokenBucketLimiter,
    ChannelRate,
    RateLimitExceededError,
)


def test_token_bucket_initial_burst_capacity():
    limiter = TokenBucketLimiter({
        "slack": ChannelRate(rate_per_second=1.0, capacity=3.0)
    })
    
    # 3 immediate acquires should succeed using burst capacity
    limiter.acquire("slack")
    limiter.acquire("slack")
    limiter.acquire("slack")
    
    # 4th immediate acquire should raise RateLimitExceededError
    with pytest.raises(RateLimitExceededError) as exc_info:
        limiter.acquire("slack")
    assert exc_info.value.retry_after_s > 0.0


def test_token_bucket_refills_over_time():
    limiter = TokenBucketLimiter({
        "fast": ChannelRate(rate_per_second=10.0, capacity=2.0)
    })
    limiter.acquire("fast")
    limiter.acquire("fast")
    
    assert limiter.get_wait_time("fast") > 0.0
    
    # Sleep to allow partial refill (0.15s should refill ~1.5 tokens)
    time.sleep(0.15)
    
    # Should now be acquirable without error
    limiter.acquire("fast")


def test_downstream_throttle_window_blocks_acquisition():
    limiter = TokenBucketLimiter({
        "twitter": ChannelRate(rate_per_second=5.0, capacity=10.0)
    })
    
    # Record downstream HTTP 429 Retry-After: 2 seconds
    limiter.record_throttle("twitter", retry_after_s=2.0)
    
    assert limiter.is_available("twitter") is False
    with pytest.raises(RateLimitExceededError) as exc_info:
        limiter.acquire("twitter")
    
    # Retry-after should reflect the throttle window
    assert 1.5 <= exc_info.value.retry_after_s <= 2.1


def test_channel_isolation():
    limiter = TokenBucketLimiter({
        "ch1": ChannelRate(rate_per_second=1.0, capacity=1.0),
        "ch2": ChannelRate(rate_per_second=10.0, capacity=10.0),
    })
    
    limiter.acquire("ch1")
    # ch1 is exhausted
    assert limiter.is_available("ch1") is False
    
    # ch2 is unaffected and still available
    assert limiter.is_available("ch2") is True
    limiter.acquire("ch2")
