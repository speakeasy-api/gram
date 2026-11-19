"""Follow the Redis instance switch without restarting.

Python counterpart of ``tunnel/redisswitch``. Two keys on the current instance
decide where a reply may be written: the role's state key says whether the
instance is still authoritative, and the hold key exists while an operator is
copying the data. Keep the key names and their values in lockstep with the Go
package.

A poller alone could not make a write safe: the process could read "current",
stall, and write after the copy had started. So a write to the current instance
is a transaction that watches both keys and aborts if either changed: the
instance itself refuses it. Once the instance has moved, the next instance is
authoritative and takes plain writes.
"""

from __future__ import annotations

import time
from collections.abc import Awaitable, Callable
from typing import Final

import anyio
import structlog
from redis import asyncio as redis_asyncio
from redis.exceptions import ConnectionError as RedisConnectionError
from redis.exceptions import TimeoutError as RedisTimeoutError
from redis.exceptions import WatchError

STATE_KEY_PREFIX: Final = "gram:redis-switch:state:"
# The role of a process that keeps everything on one instance.
DEFAULT_ROLE: Final = "all"
HOLD_KEY: Final = "gram:redis-switch:hold"
# Named connections let the operator's job tell switch-aware clients from ones
# that would ignore a hold.
CLIENT_NAME: Final = "gram-redisswitch"

_VALUE_CURRENT: Final = b"current"
_MOVED_PREFIX: Final = b"moved:"

# How long a blocked write waits between attempts.
RETRY_INTERVAL_SECONDS: Final = 0.25
# Longest a write waits for a hold to end before failing. Holds are bounded by
# the operator's copy job, well under this.
HOLD_TIMEOUT_SECONDS: Final = 60.0

Write = Callable[[redis_asyncio.client.Pipeline], None]


def _as_bytes(value: bytes | str | None) -> bytes | None:
    """Normalise a reply, which is text on a client that decodes responses."""
    return value.encode() if isinstance(value, str) else value


def state_key(role: str) -> str:
    """The key that says which instance is authoritative for a role."""
    return STATE_KEY_PREFIX + role


class SwitchHoldTimeoutError(TimeoutError):
    """A write waited longer than the hold limit for the switch to allow it."""


class RedisSwitch:
    """Runs a write on whichever instance is authoritative."""

    def __init__(
        self,
        current: redis_asyncio.Redis,
        next_client: redis_asyncio.Redis,
        logger: structlog.stdlib.BoundLogger,
        *,
        role: str = DEFAULT_ROLE,
        clock: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], Awaitable[None]] = anyio.sleep,
    ) -> None:
        self._state_key = state_key(role)
        self._current = current
        self._next = next_client
        self._logger = logger
        self._clock = clock
        self._sleep = sleep
        self._moved = False

    @property
    def moved(self) -> bool:
        return self._moved

    async def start(self) -> None:
        """Check the current instance is armed; raises when it is not."""
        state = _as_bytes(await self._current.get(self._state_key))
        if state is None:
            raise RuntimeError(
                "redis switch key is not set on the current instance; "
                "an operator must arm it before a next instance is configured"
            )
        self._observe(state)

    async def execute(self, write: Write) -> None:
        """Queue ``write`` on a pipeline and run it atomically where allowed.

        Waits while the switch holds or cannot be read, for at most
        HOLD_TIMEOUT_SECONDS.
        """
        deadline = self._clock() + HOLD_TIMEOUT_SECONDS
        while True:
            if self._moved:
                async with self._next.pipeline(transaction=True) as pipe:
                    write(pipe)
                    await pipe.execute()
                return
            if await self._write_if_current(write):
                return
            if self._clock() >= deadline:
                raise SwitchHoldTimeoutError("redis switch did not allow the write")
            await self._sleep(RETRY_INTERVAL_SECONDS)

    async def _write_if_current(self, write: Write) -> bool:
        """Write to the current instance only if it is still authoritative."""
        async with self._current.pipeline(transaction=True) as pipe:
            try:
                await pipe.watch(self._state_key, HOLD_KEY)
                values = await pipe.mget(self._state_key, HOLD_KEY)
            except RedisConnectionError, RedisTimeoutError:
                # Nothing has been sent to write yet, so trying again is safe.
                # The operator's job closes connections during a hold.
                return False
            state, hold = _as_bytes(values[0]), values[1]
            self._observe(state)
            if state != _VALUE_CURRENT or hold is not None:
                return False
            try:
                pipe.multi()
                write(pipe)
                await pipe.execute()
            except WatchError:
                # A key changed between the read and the write: the instance
                # refused the transaction, so nothing was written.
                return False
            return True

    def _observe(self, state: bytes | None) -> None:
        if self._moved or state is None:
            return
        if state.startswith(_MOVED_PREFIX):
            self._moved = True
            self._logger.info("redis switch moved to the next instance")
