"""Subscriber message-handling tests (no live broker).

Ported from infra/pkg/gcp/subscriber_test.go. Exercises the ack/nack and logging
behavior of Subscriber._process_message against a fake message, so the core
delivery logic is covered without an emulator.
"""

from __future__ import annotations

import asyncio
import concurrent.futures
import logging
import threading
from typing import Any, cast

import pytest

from gram.ping.v1 import ping_pb2
from gram_infra.pubsub import MessageMetadata, Subscriber, SubscriberHandle
from gram_infra.pubsub.subscriber import _PortalScheduler

TOPIC_PROTO = "gram.ping.v1.Message"
SUB_PROTO = "gram.ping.v1.Processor"


class FakeMessage:
    """Duck-typed stand-in for a google-cloud-pubsub Message."""

    def __init__(
        self, data, *, message_id="msg-id", attributes=None, delivery_attempt=None
    ):
        self.data = data
        self.message_id = message_id
        self.attributes = attributes or {}
        self.delivery_attempt = delivery_attempt
        self.acked = False
        self.nacked = False

    def ack(self):
        self.acked = True

    def nack(self):
        self.nacked = True


def make_subscriber(logger=None) -> Subscriber:
    # _handle_message never touches the handle (only receive() does), so a
    # typed-None placeholder is sufficient for these unit tests.
    return Subscriber(
        cast(SubscriberHandle, None),
        ping_pb2.Message,
        logger=logger or logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )


async def test_success_is_acked_only() -> None:
    data = ping_pb2.Message(id="abc", type="t").SerializeToString()
    message = FakeMessage(
        data, message_id="msg-ok", attributes={"content-type": "application/x-protobuf"}
    )

    seen: dict = {}

    async def callback(msg, meta: MessageMetadata) -> None:
        seen["msg"] = msg
        seen["meta"] = meta

    await make_subscriber()._handle_message(message, callback)

    assert message.acked is True
    assert message.nacked is False
    assert seen["msg"].id == "abc"
    assert seen["meta"].id == "msg-ok"
    assert seen["meta"].attributes == {"content-type": "application/x-protobuf"}


async def test_unmarshal_error_is_nacked_and_skips_callback() -> None:
    # Field 1 (string) declares a 5-byte length but supplies 2 bytes -> truncated.
    message = FakeMessage(b"\x0a\x05ab", message_id="msg-bad")

    class Receiver:
        called = False

        async def callback(self, msg, meta) -> None:
            self.called = True

    receiver = Receiver()
    await make_subscriber()._handle_message(message, receiver.callback)

    assert message.nacked is True
    assert message.acked is False
    assert receiver.called is False


async def test_callback_exception_is_logged_and_nacked(caplog) -> None:
    data = ping_pb2.Message(id="x").SerializeToString()
    message = FakeMessage(data, message_id="msg-123", delivery_attempt=3)

    async def callback(msg, meta) -> None:
        raise RuntimeError("boom")

    with caplog.at_level(logging.ERROR, logger="gram_infra.test"):
        await make_subscriber()._handle_message(message, callback)

    assert message.nacked is True
    assert message.acked is False

    record = next(
        r for r in caplog.records if r.msg == "error processing pubsub message"
    )
    assert "boom" in record.exc_text
    assert record.topic_proto_name == TOPIC_PROTO
    assert record.subscription_proto_name == SUB_PROTO
    assert record.message_id == "msg-123"
    assert record.delivery_attempt == 3


async def test_nil_delivery_attempt_logs_zero(caplog) -> None:
    data = ping_pb2.Message(id="x").SerializeToString()
    message = FakeMessage(data, message_id="msg-nil", delivery_attempt=None)

    async def callback(msg, meta) -> None:
        raise RuntimeError("kaboom")

    with caplog.at_level(logging.ERROR, logger="gram_infra.test"):
        await make_subscriber()._handle_message(message, callback)

    assert message.nacked is True
    record = next(
        r for r in caplog.records if r.msg == "error processing pubsub message"
    )
    assert record.delivery_attempt == 0


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
    """Mimics the real client: it schedules messages from a background thread.

    The production google-cloud-pubsub client invokes ``scheduler.schedule`` from
    its own background threads, never from the event-loop thread. The scheduler
    bridges those threads to the loop via a BlockingPortal, so the fake must
    schedule off-thread too — scheduling synchronously on the loop thread would
    deadlock the portal.
    """

    def __init__(self, messages) -> None:
        self._messages = messages
        self.future = FakeStreamingFuture()
        self._thread: threading.Thread | None = None

    def subscribe(self, subscription_path, callback, *, scheduler, **kwargs):
        def deliver() -> None:
            for message in self._messages:
                scheduler.schedule(callback, message)

        self._thread = threading.Thread(target=deliver, daemon=True)
        self._thread.start()
        return self.future


async def test_receive_processes_messages_concurrently() -> None:
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

    started = 0
    all_started = asyncio.Event()
    release = asyncio.Event()

    async def callback(msg, meta) -> None:
        nonlocal started
        started += 1
        if started == len(messages):
            all_started.set()
        await release.wait()

    receive_task = asyncio.create_task(subscriber.receive(callback))
    try:
        await asyncio.wait_for(all_started.wait(), timeout=1)

        assert all(not message.acked for message in messages)
        release.set()

        async def wait_for_acks() -> None:
            while not all(message.acked for message in messages):
                await asyncio.sleep(0)

        await asyncio.wait_for(wait_for_acks(), timeout=1)
        assert all(not message.nacked for message in messages)
    finally:
        receive_task.cancel()
        try:
            await receive_task
        except asyncio.CancelledError:
            pass


def make_stream_subscriber(messages) -> Subscriber:
    return Subscriber(
        SubscriberHandle(cast(Any, FakeSubscriberClient(messages)), "subscriptions/test"),
        ping_pb2.Message,
        logger=logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )


async def test_stream_yields_messages_with_explicit_ack() -> None:
    messages = [
        FakeMessage(ping_pb2.Message(id="one").SerializeToString(), message_id="one"),
        FakeMessage(ping_pb2.Message(id="two").SerializeToString(), message_id="two"),
    ]
    subscriber = make_stream_subscriber(messages)

    seen: list[str] = []
    async with subscriber.stream() as stream:
        async for received in stream:
            seen.append(received.message.id)
            received.ack()
            if len(seen) == len(messages):
                break

    assert seen == ["one", "two"]
    assert all(message.acked for message in messages)
    assert not any(message.nacked for message in messages)


async def test_stream_explicit_nack() -> None:
    message = FakeMessage(
        ping_pb2.Message(id="x").SerializeToString(), message_id="x"
    )
    subscriber = make_stream_subscriber([message])

    async with subscriber.stream() as stream:
        async for received in stream:
            received.nack()
            break

    assert message.nacked is True
    assert message.acked is False


async def test_stream_skips_undecodable_message() -> None:
    # First message is truncated (declares 5 bytes, supplies 2) -> nacked + skipped;
    # the second decodes and is yielded.
    bad = FakeMessage(b"\x0a\x05ab", message_id="bad")
    good = FakeMessage(
        ping_pb2.Message(id="good").SerializeToString(), message_id="good"
    )
    subscriber = make_stream_subscriber([bad, good])

    seen: list[str] = []
    async with subscriber.stream() as stream:
        async for received in stream:
            seen.append(received.message.id)
            received.ack()
            break

    assert seen == ["good"]
    assert bad.nacked is True
    assert bad.acked is False
    assert good.acked is True


class _RaisingPortal:
    """Portal stand-in whose ``call`` always raises.

    Models the event loop being gone or mid-teardown when the library invokes the
    scheduler from its own background shutdown thread (the Ctrl-C race).
    """

    def __init__(self, exc: BaseException) -> None:
        self._exc = exc

    def call(self, func, *args):
        raise self._exc


@pytest.mark.parametrize(
    "exc",
    [RuntimeError("portal is not running"), concurrent.futures.CancelledError()],
)
def test_portal_scheduler_degrades_when_portal_unavailable(exc: BaseException) -> None:
    # The library calls schedule()/shutdown() from background threads; if the
    # portal is gone or being cancelled, those must degrade quietly (nack / []),
    # never raise back into the library thread (which prints an unhandled
    # "Exception in thread ..." traceback on shutdown).
    scheduler = _PortalScheduler(cast(Any, _RaisingPortal(exc)), cast(Any, None))

    assert scheduler.shutdown() == []

    message = FakeMessage(b"", message_id="x")
    scheduler.schedule(lambda m: None, message)
    assert message.nacked is True


async def test_stream_tears_down_cleanly_on_cancel() -> None:
    # Mirrors the streaming demo's shutdown: a stream consumed inside a task that
    # is cancelled must unwind through `async with`/`async for` without hanging.
    message = FakeMessage(
        ping_pb2.Message(id="x").SerializeToString(), message_id="x"
    )
    subscriber = make_stream_subscriber([message])

    seen = asyncio.Event()

    async def consume() -> None:
        async with subscriber.stream() as stream:
            async for received in stream:
                received.ack()
                seen.set()
                # Stay in the loop awaiting more messages (none come) so the
                # cancellation lands while blocked inside the stream.

    task = asyncio.create_task(consume())
    await asyncio.wait_for(seen.wait(), timeout=1)
    task.cancel()
    # Clean teardown means the task settles promptly rather than hanging.
    try:
        await asyncio.wait_for(task, timeout=1)
    except asyncio.CancelledError:
        pass
    assert message.acked is True


async def test_received_message_disposition_is_idempotent() -> None:
    message = FakeMessage(
        ping_pb2.Message(id="x").SerializeToString(), message_id="x"
    )
    subscriber = make_stream_subscriber([message])

    async with subscriber.stream() as stream:
        async for received in stream:
            received.ack()
            received.nack()  # second call is a no-op; first disposition wins
            break

    assert message.acked is True
    assert message.nacked is False
