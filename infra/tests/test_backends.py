"""Backend-agnostic tests: the publisher and subscriber must work under both
anyio backends, not just asyncio.

These are written as *sync* tests that drive an async scenario via
``anyio.run(..., backend=...)``, parametrized over asyncio and trio. That keeps
them independent of the pytest-asyncio auto-mode plugin the rest of the suite
uses, and proves the library does not secretly depend on asyncio internals
(e.g. ``asyncio.wrap_future`` would fail under trio).
"""

from __future__ import annotations

import concurrent.futures as cf
import logging
import threading
from typing import Any, cast

import anyio
import pytest

from datetime import timedelta

from google.protobuf.duration_pb2 import Duration

from gram.ping.v1 import ping_pb2
from gram_infra.pubsub import (
    Publisher,
    PublisherHandle,
    Subscriber,
    SubscriberHandle,
)
from gram_infra.pubsub.broker import (
    _duration_to_seconds,
    _validate_backoff,
    _validate_expiration_ttl,
    _validate_max_delivery_attempts,
    _validate_retention,
)


def test_negative_duration_rounds_toward_zero() -> None:
    """``Duration.ToTimedelta`` rounds nanos toward zero, unlike a naive
    ``nanos // 1000`` floor which biases negative values toward -inf."""
    d = Duration()
    d.FromTimedelta(timedelta(microseconds=-500))
    # microseconds component is -500 (toward zero), not -1000 (floored).
    assert d.ToTimedelta() == timedelta(microseconds=-500)


def test_duration_to_seconds_rounds_half_up_and_clamps() -> None:
    assert _duration_to_seconds(Duration(seconds=30)) == 30
    assert _duration_to_seconds(Duration(seconds=0, nanos=500_000_000)) == 1
    assert _duration_to_seconds(Duration(seconds=-5)) == 0
    assert _duration_to_seconds(Duration(seconds=2**40)) == 2**31 - 1


def test_validate_retention_bounds() -> None:
    _validate_retention(Duration(seconds=600), "topic-x")  # exactly 10 minutes: ok
    with pytest.raises(ValueError, match="message retention"):
        _validate_retention(Duration(seconds=60), "topic-x")


def test_validate_expiration_ttl_bounds() -> None:
    _validate_expiration_ttl(Duration(seconds=86400), "sub-x")  # 1 day: ok
    with pytest.raises(ValueError, match="expiration TTL"):
        _validate_expiration_ttl(Duration(seconds=3600), "sub-x")


def test_validate_backoff_bounds() -> None:
    _validate_backoff(Duration(seconds=600), "maximum backoff", "sub-x")  # cap: ok
    with pytest.raises(ValueError, match="maximum backoff"):
        _validate_backoff(Duration(seconds=601), "maximum backoff", "sub-x")


def test_validate_max_delivery_attempts_bounds() -> None:
    _validate_max_delivery_attempts(5, "sub-x")
    _validate_max_delivery_attempts(100, "sub-x")
    with pytest.raises(ValueError, match="max delivery attempts"):
        _validate_max_delivery_attempts(4, "sub-x")

# Both anyio backends. trio is a dev dependency (anyio[trio]).
BACKENDS = ["asyncio", "trio"]

TOPIC_PROTO = "gram.ping.v1.Message"
SUB_PROTO = "gram.ping.v1.Processor"


class FakePublisherClient:
    """Mimics the google-cloud-pubsub publish path: returns a
    ``concurrent.futures.Future`` resolved on a background thread."""

    def __init__(self) -> None:
        self.published: list[tuple[str, bytes, dict[str, str]]] = []

    def publish(self, topic_path, data, **attributes):
        self.published.append((topic_path, data, attributes))
        future: cf.Future = cf.Future()
        threading.Thread(
            target=lambda: future.set_result("server-msg-id"), daemon=True
        ).start()
        return future


@pytest.mark.parametrize("backend", BACKENDS)
def test_publish_awaits_future_on_backend(backend) -> None:
    client = FakePublisherClient()
    publisher = Publisher(
        PublisherHandle(cast(Any, client), "topics/test"), TOPIC_PROTO
    )

    async def scenario() -> Any:
        return await publisher.publish(ping_pb2.Message(id="x", type="t"))

    result = anyio.run(scenario, backend=backend)

    assert result == "server-msg-id"
    assert len(client.published) == 1
    _, _, attributes = client.published[0]
    assert attributes["schema"] == TOPIC_PROTO
    assert attributes["content-type"] == "application/x-protobuf"


class FakeMessage:
    """Duck-typed stand-in for a google-cloud-pubsub Message."""

    def __init__(self, data, *, message_id="msg-id") -> None:
        self.data = data
        self.message_id = message_id
        self.attributes: dict[str, str] = {}
        self.delivery_attempt = None
        self.acked = False
        self.nacked = False

    def ack(self) -> None:
        self.acked = True

    def nack(self) -> None:
        self.nacked = True


class FakeStreamingFuture:
    def __init__(self) -> None:
        self._cancelled = threading.Event()

    def result(self) -> None:
        self._cancelled.wait()

    def cancel(self) -> None:
        self._cancelled.set()

    def done(self) -> bool:
        return self._cancelled.is_set()


class FakeSubscriberClient:
    """Schedules messages from a background thread, like the real library —
    so the scheduler's thread-to-loop bridge is genuinely exercised."""

    def __init__(self, messages) -> None:
        self._messages = messages
        self.future = FakeStreamingFuture()

    def subscribe(self, subscription_path, callback, *, scheduler, **kwargs):
        def pump() -> None:
            for message in self._messages:
                scheduler.schedule(callback, message)

        threading.Thread(target=pump, daemon=True).start()
        return self.future


@pytest.mark.parametrize("backend", BACKENDS)
def test_receive_acks_messages_on_backend(backend) -> None:
    messages = [
        FakeMessage(ping_pb2.Message(id="one").SerializeToString(), message_id="one"),
        FakeMessage(ping_pb2.Message(id="two").SerializeToString(), message_id="two"),
    ]
    client = FakeSubscriberClient(messages)
    subscriber = Subscriber(
        SubscriberHandle(cast(Any, client), "subscriptions/test"),
        ping_pb2.Message,
        logger=logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )

    async def scenario() -> list[str]:
        seen: list[str] = []
        done = anyio.Event()

        async def callback(message, meta) -> None:
            seen.append(message.id)
            if len(seen) == len(messages):
                done.set()

        async with anyio.create_task_group() as tg:
            tg.start_soon(subscriber.receive, callback)
            with anyio.fail_after(5):
                await done.wait()
                # Let the in-flight handlers ack before we tear down.
                while not all(message.acked for message in messages):
                    await anyio.sleep(0)
            tg.cancel_scope.cancel()

        return seen

    seen = anyio.run(scenario, backend=backend)

    assert sorted(seen) == ["one", "two"]
    assert all(message.acked for message in messages)
    assert not any(message.nacked for message in messages)
