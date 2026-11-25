"""Tests for writing enforcement replies across the Redis instance switch."""

import fakeredis
import fakeredis.aioredis
import pytest
import structlog
from gram.risk.v1 import enforcement_reply_pb2
from redis import asyncio as redis_asyncio

from pystreams.risk.redisswitch import (
    DEFAULT_ROLE,
    HOLD_KEY,
    HOLD_TIMEOUT_SECONDS,
    RedisSwitch,
    SwitchHoldTimeoutError,
    state_key,
)
from pystreams.risk.replywriter import ReplyWriter, inbox_key

_REPLY_URN = "urn:gram:risk:enforce:replica-1:0198f1f4-0000-7000-8000-000000000000"
_SCAN_ID = "0198f1f4-0000-7000-8000-000000000000"
_STATE_KEY = state_key(DEFAULT_ROLE)
_INBOX = inbox_key("replica-1")


class _Clock:
    """A clock that only moves when the switch sleeps."""

    def __init__(self) -> None:
        self.now = 0.0
        self.sleeps = 0

    def __call__(self) -> float:
        return self.now

    async def sleep(self, seconds: float) -> None:
        self.now += seconds
        self.sleeps += 1


class _Fixture:
    def __init__(self) -> None:
        # Separate servers, so a write to one instance is invisible on the other.
        self.current_server = fakeredis.FakeServer()
        self.current = fakeredis.aioredis.FakeRedis(server=self.current_server)
        self.next = fakeredis.aioredis.FakeRedis(server=fakeredis.FakeServer())
        self.clock = _Clock()
        self.switch = RedisSwitch(
            self.current,
            self.next,
            structlog.get_logger(),
            clock=self.clock,
            sleep=self.clock.sleep,
        )
        self.writer = ReplyWriter(self.current, self.switch)

    async def write(self) -> None:
        reply = enforcement_reply_pb2.EnforcementReply(correlation_id=_SCAN_ID)
        await self.writer.write(_REPLY_URN, reply)

    async def lengths(self) -> tuple[int, int]:
        return await self.current.llen(_INBOX), await self.next.llen(_INBOX)


async def test_start_refuses_an_unarmed_instance():
    f = _Fixture()
    with pytest.raises(RuntimeError, match="not set"):
        await f.switch.start()


async def test_writes_to_current_while_it_is_authoritative():
    f = _Fixture()
    await f.current.set(_STATE_KEY, "current")
    await f.switch.start()

    await f.write()

    assert await f.lengths() == (1, 0)
    assert 0 < await f.current.ttl(_INBOX) <= 60


async def test_writes_to_next_once_the_instance_has_moved():
    f = _Fixture()
    await f.current.set(_STATE_KEY, "moved:run-1")
    await f.switch.start()

    await f.write()

    assert await f.lengths() == (0, 1)
    assert 0 < await f.next.ttl(_INBOX) <= 60


async def test_a_write_that_started_before_the_move_lands_on_next():
    f = _Fixture()
    await f.current.set(_STATE_KEY, "current")
    await f.switch.start()
    # The move happens after the process last looked: the write must still be
    # refused by the instance being left.
    await f.current.set(_STATE_KEY, "moved:run-1")

    await f.write()

    assert await f.lengths() == (0, 1)
    assert f.switch.moved


async def test_a_held_write_waits_and_never_reaches_current():
    f = _Fixture()
    await f.current.set(_STATE_KEY, "current")
    await f.current.set(HOLD_KEY, "run-1")
    await f.switch.start()

    with pytest.raises(SwitchHoldTimeoutError):
        await f.write()

    assert await f.lengths() == (0, 0)
    assert f.clock.now >= HOLD_TIMEOUT_SECONDS


async def test_a_held_write_resumes_on_current_when_the_hold_ends():
    f = _Fixture()
    await f.current.set(_STATE_KEY, "current")
    await f.current.set(HOLD_KEY, "run-1")
    await f.switch.start()

    async def release_then_sleep(seconds: float) -> None:
        await f.current.delete(HOLD_KEY)
        await f.clock.sleep(seconds)

    f.switch._sleep = release_then_sleep
    await f.write()

    assert await f.lengths() == (1, 0)


@pytest.mark.parametrize("value", [None, "nonsense"])
async def test_a_missing_or_unrecognised_state_key_holds(value: str | None):
    f = _Fixture()
    await f.current.set(_STATE_KEY, "current")
    await f.switch.start()
    if value is None:
        await f.current.delete(_STATE_KEY)
    else:
        await f.current.set(_STATE_KEY, value)

    with pytest.raises(SwitchHoldTimeoutError):
        await f.write()

    assert await f.lengths() == (0, 0)


async def test_a_hold_set_between_the_check_and_the_write_is_refused():
    f = _Fixture()
    await f.current.set(_STATE_KEY, "current")
    await f.switch.start()

    # The operator's job sets the hold from its own connection after the
    # transaction has read the keys but before it executes. Queuing the write
    # is the last thing the switch does before EXEC, so do it from there.
    operator = fakeredis.FakeRedis(server=f.current_server)
    attempts = 0

    def queue(pipe: redis_asyncio.client.Pipeline) -> None:
        nonlocal attempts
        attempts += 1
        operator.set(HOLD_KEY, "run-1")
        pipe.rpush(_INBOX, b"payload")

    with pytest.raises(SwitchHoldTimeoutError):
        await f.switch.execute(queue)

    assert attempts == 1, "later attempts see the hold and never queue the write"
    assert await f.lengths() == (0, 0)
