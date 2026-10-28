"""Single-flight call deduplication to prevent OAuth token refresh stampedes."""

import asyncio
from typing import Any, Awaitable, Callable, TypeVar

T = TypeVar("T")


class SingleFlightGroup:
    """Coordinates concurrent calls for the same key so only one execution runs."""

    def __init__(self) -> None:
        self._in_flight: dict[str, asyncio.Future[Any]] = {}
        self._lock = asyncio.Lock()

    async def execute(self, key: str, fn: Callable[[], Awaitable[T]]) -> T:
        """Execute `fn` if not already running for `key`; otherwise await the existing flight."""
        async with self._lock:
            if key in self._in_flight:
                future = self._in_flight[key]
                # Await outside the lock
                is_leader = False
            else:
                loop = asyncio.get_running_loop()
                future = loop.create_future()
                self._in_flight[key] = future
                is_leader = True

        if not is_leader:
            return await future

        try:
            result = await fn()
            if not future.done():
                future.set_result(result)
            return result
        except BaseException as exc:
            if not future.done():
                future.set_exception(exc)
            raise
        finally:
            async with self._lock:
                self._in_flight.pop(key, None)

