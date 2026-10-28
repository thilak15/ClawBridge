"""Token-bucket rate limiter with per-channel burst capacity and downstream backoff."""

import threading
import time
from dataclasses import dataclass


@dataclass(frozen=True)
class ChannelRate:
    """Configured rate and burst capacity for a channel."""
    rate_per_second: float
    capacity: float


class RateLimitExceededError(Exception):
    """Raised when an egress tool invocation exceeds available token bucket capacity."""

    def __init__(self, retry_after_s: float, channel: str | None = None):
        self.retry_after_s = retry_after_s
        self.channel = channel
        super().__init__(f"Rate limit exceeded for channel '{channel}'. Retry after {retry_after_s:.2f}s")


class _BucketState:
    def __init__(self, capacity: float):
        self.tokens: float = capacity
        self.last_update: float = time.time()
        self.throttled_until: float = 0.0


class TokenBucketLimiter:
    """Thread-safe per-channel token bucket rate limiter with downstream backoff window."""

    DEFAULT_RATES: dict[str, ChannelRate] = {
        "slack": ChannelRate(rate_per_second=1.0, capacity=5.0),
        "twitter": ChannelRate(rate_per_second=0.5, capacity=3.0),
        "jira": ChannelRate(rate_per_second=2.0, capacity=10.0),
        "discord": ChannelRate(rate_per_second=2.0, capacity=5.0),
        "default": ChannelRate(rate_per_second=5.0, capacity=10.0),
    }

    def __init__(
        self,
        rates: dict[str, ChannelRate] | None = None,
        default_rate: ChannelRate | None = None,
    ):
        self._rates = dict(self.DEFAULT_RATES)
        if rates:
            self._rates.update(rates)
        self._default_rate = default_rate or self._rates.get("default", ChannelRate(5.0, 10.0))
        self._buckets: dict[str, _BucketState] = {}
        self._lock = threading.Lock()

    def _get_bucket_and_rate(self, channel: str) -> tuple[_BucketState, ChannelRate]:
        ch_key = channel.strip().lower()
        rate = self._rates.get(ch_key, self._default_rate)
        if ch_key not in self._buckets:
            self._buckets[ch_key] = _BucketState(capacity=rate.capacity)
        return self._buckets[ch_key], rate

    def _refill(self, state: _BucketState, rate: ChannelRate, now: float) -> None:
        elapsed = now - state.last_update
        if elapsed > 0:
            state.tokens = min(rate.capacity, state.tokens + (elapsed * rate.rate_per_second))
            state.last_update = now

    def record_throttle(self, channel: str, retry_after_s: float) -> None:
        """Pause bucket acquisition until a downstream 429 Retry-After window lapses."""
        with self._lock:
            state, _ = self._get_bucket_and_rate(channel)
            state.throttled_until = max(state.throttled_until, time.time() + retry_after_s)

    def get_wait_time(self, channel: str, cost: float = 1.0) -> float:
        """Calculate seconds required before `cost` tokens can be acquired."""
        with self._lock:
            now = time.time()
            state, rate = self._get_bucket_and_rate(channel)
            self._refill(state, rate, now)

            # Check if downstream throttle window is active
            throttle_wait = max(0.0, state.throttled_until - now)
            if throttle_wait > 0:
                return throttle_wait

            if state.tokens >= cost:
                return 0.0

            needed = cost - state.tokens
            return needed / rate.rate_per_second

    def is_available(self, channel: str, cost: float = 1.0) -> bool:
        """Check if tokens are currently available without consuming them."""
        return self.get_wait_time(channel, cost) <= 0.0

    def acquire(self, channel: str, cost: float = 1.0) -> None:
        """Consume `cost` tokens or raise RateLimitExceededError if capacity is depleted."""
        with self._lock:
            now = time.time()
            state, rate = self._get_bucket_and_rate(channel)
            self._refill(state, rate, now)

            # Check downstream throttle pause
            if now < state.throttled_until:
                retry_after = state.throttled_until - now
                raise RateLimitExceededError(retry_after_s=retry_after, channel=channel)

            if state.tokens < cost:
                needed = cost - state.tokens
                wait_time = needed / rate.rate_per_second
                raise RateLimitExceededError(retry_after_s=wait_time, channel=channel)

            state.tokens -= cost
