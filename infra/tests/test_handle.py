"""Subscriber message-handling and teardown tests (no live broker).

Ported from infra/pkg/gcp/subscriber_test.go. Exercises the ack/nack and logging
behavior of Subscriber._handle_message against a fake message, plus the
scheduler's teardown disposition (undispatched messages are nacked, shutdown
waits for in-flight handlers, terminal stream errors surface) — so the core
delivery logic is covered without an emulator.
"""

from __future__ import annotations

import asyncio
import concurrent.futures
import logging
import threading
from typing import Any, cast

import anyio
import anyio.to_thread
import pytest
from google.api_core.exceptions import NotFound

from conftest import FakeMessage, FakeSubscriberClient
from gram.ping.v1 import ping_pb2
from gram_infra.pubsub import MessageMetadata, Subscriber, SubscriberHandle
from gram_infra.pubsub.subscriber import _PortalScheduler

TOPIC_PROTO = "gram.ping.v1.Message"
SUB_PROTO = "gram.ping.v1.Processor"


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


async def test_unmarshal_non_decode_error_is_nacked_and_skips_callback() -> None:
    # Under the pure-Python protobuf backend a malformed payload can raise
    # UnicodeDecodeError (not DecodeError) out of ParseFromString. Any such
    # exception must nack the message, not tear down the receive loop — one
    # malformed publish must never become a poison-message crash loop.
    class ExplodingProto:
        def ParseFromString(self, data) -> None:
            raise UnicodeDecodeError("utf-8", b"\xff", 0, 1, "invalid start byte")

    subscriber = Subscriber(
        cast(SubscriberHandle, None),
        cast(Any, ExplodingProto),
        logger=logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )
    message = FakeMessage(b"\x0a\x02\xff\xfe", message_id="msg-poison")

    called = False

    async def callback(msg, meta) -> None:
        nonlocal called
        called = True

    await subscriber._handle_message(message, callback)

    assert message.nacked is True
    assert message.acked is False
    assert not called


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


def make_client_subscriber(messages) -> tuple[FakeSubscriberClient, Subscriber]:
    client = FakeSubscriberClient(messages)
    subscriber = Subscriber(
        SubscriberHandle(cast(Any, client), "subscriptions/test"),
        ping_pb2.Message,
        logger=logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )
    return client, subscriber


async def test_receive_processes_messages_concurrently() -> None:
    messages = [
        FakeMessage(ping_pb2.Message(id="one").SerializeToString(), message_id="one"),
        FakeMessage(ping_pb2.Message(id="two").SerializeToString(), message_id="two"),
    ]
    _, subscriber = make_client_subscriber(messages)

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


async def test_handler_cancelled_while_queued_on_limiter_is_nacked() -> None:
    # A message popped off the stream but still waiting behind the concurrency
    # bound never reached a handler; cancellation must nack it (immediate
    # redelivery) rather than leave it leased until the ack deadline lapses.
    # A message cancelled mid-handler stays undisposed by design.
    limiter = anyio.CapacityLimiter(1)
    blocked = FakeMessage(ping_pb2.Message(id="a").SerializeToString(), message_id="a")
    queued = FakeMessage(ping_pb2.Message(id="b").SerializeToString(), message_id="b")
    subscriber = make_subscriber()

    started = anyio.Event()

    async def blocking_callback(msg, meta) -> None:
        started.set()
        await anyio.sleep_forever()

    async with anyio.create_task_group() as tg:
        tg.start_soon(subscriber._handle_message, blocked, blocking_callback, limiter)
        with anyio.fail_after(2):
            await started.wait()
        tg.start_soon(subscriber._handle_message, queued, blocking_callback, limiter)
        await anyio.sleep(0.05)  # let the second message queue on the limiter
        tg.cancel_scope.cancel()

    assert queued.nacked is True
    assert queued.acked is False
    assert blocked.acked is False and blocked.nacked is False


async def test_scheduler_shutdown_waits_for_inflight_handlers() -> None:
    # The manager's shutdown thread calls scheduler.shutdown() and stops its
    # Dispatcher right after it returns; any ack enqueued later is silently
    # dropped. shutdown(await_msg_callbacks=True) must therefore block until
    # in-flight handlers finish, so their acks are dispatched first (the stock
    # ThreadScheduler blocks the same way via executor.shutdown(wait=True)).
    message = FakeMessage(ping_pb2.Message(id="x").SerializeToString(), message_id="x")
    client, subscriber = make_client_subscriber([message])

    started = anyio.Event()
    release = anyio.Event()

    async def callback(msg, meta) -> None:
        started.set()
        await release.wait()

    shutdown_returned = threading.Event()

    def call_shutdown() -> None:
        client.scheduler.shutdown(await_msg_callbacks=True)
        shutdown_returned.set()

    async with anyio.create_task_group() as tg:
        tg.start_soon(subscriber.receive, callback)
        with anyio.fail_after(2):
            await started.wait()

        threading.Thread(target=call_shutdown, daemon=True).start()
        await anyio.sleep(0.1)
        assert not shutdown_returned.is_set()  # handler still in flight

        release.set()
        with anyio.fail_after(2):
            await anyio.to_thread.run_sync(shutdown_returned.wait)
        # The handler completed (and acked) before shutdown returned, i.e.
        # before the library would stop its Dispatcher.
        assert message.acked is True
        tg.cancel_scope.cancel()


async def test_receive_raises_terminal_stream_error() -> None:
    # A dead subscription must surface out of receive(), the way Go's Receive
    # returns the error — unwrapped, so callers can catch NotFound directly.
    client, subscriber = make_client_subscriber([])

    async def callback(msg, meta) -> None:
        pass

    def fail_stream() -> None:
        # Mimic the library's shutdown thread: stop the scheduler, then resolve
        # the streaming-pull future with the terminal error.
        client.scheduler.shutdown(await_msg_callbacks=True)
        client.future.fail(NotFound("subscription deleted"))

    async def trigger() -> None:
        while client.scheduler is None:
            await asyncio.sleep(0)
        await anyio.to_thread.run_sync(fail_stream)

    trigger_task = asyncio.create_task(trigger())
    with pytest.raises(NotFound):
        await asyncio.wait_for(subscriber.receive(callback), timeout=2)
    await trigger_task


def make_stream_subscriber(messages) -> Subscriber:
    _, subscriber = make_client_subscriber(messages)
    return subscriber


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


async def test_stream_teardown_nacks_undelivered_messages() -> None:
    # Messages the library delivered but the consumer never pulled must be
    # nacked at teardown so the broker redelivers them immediately — not left
    # leased (neither acked nor nacked) until the ack deadline lapses, burning
    # a delivery_attempt toward the DLQ threshold on every restart.
    messages = [
        FakeMessage(ping_pb2.Message(id=str(i)).SerializeToString(), message_id=str(i))
        for i in range(3)
    ]
    subscriber = make_stream_subscriber(messages)

    async with subscriber.stream() as stream:
        received = await stream.__anext__()
        received.ack()
        # Wait until the two remaining messages are buffered, so the teardown
        # below deterministically finds them undispatched.
        with anyio.fail_after(2):
            while stream._recv.statistics().current_buffer_used < 2:
                await anyio.sleep(0.01)

    consumed = next(m for m in messages if m.message_id == received.metadata.id)
    rest = [m for m in messages if m is not consumed]
    assert consumed.acked is True
    assert all(m.nacked for m in rest)
    assert not any(m.acked for m in rest)


async def test_stream_raises_terminal_stream_error() -> None:
    # A terminal subscription failure must end the async-for with the error,
    # not as a clean, silent stop the consumer cannot distinguish from a drain.
    message = FakeMessage(ping_pb2.Message(id="x").SerializeToString(), message_id="x")
    client = FakeSubscriberClient([message])
    subscriber = Subscriber(
        SubscriberHandle(cast(Any, client), "subscriptions/test"),
        ping_pb2.Message,
        logger=logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )

    def fail_stream() -> None:
        client.scheduler.shutdown(await_msg_callbacks=True)
        client.future.fail(NotFound("subscription deleted"))

    async with subscriber.stream() as stream:
        received = await stream.__anext__()
        received.ack()
        await anyio.to_thread.run_sync(fail_stream)
        with pytest.raises(NotFound):
            with anyio.fail_after(2):
                await stream.__anext__()


class _RaisingPortal:
    """Portal stand-in whose calls always raise.

    Models the event loop being gone or mid-teardown when the library invokes the
    scheduler from its own background shutdown thread (the Ctrl-C race).
    """

    def __init__(self, exc: BaseException) -> None:
        self._exc = exc

    def call(self, func, *args):
        raise self._exc

    def start_task_soon(self, func, *args):
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
    scheduler = _PortalScheduler(
        cast(Any, _RaisingPortal(exc)), cast(Any, None), cast(Any, None)
    )

    assert scheduler.shutdown() == []

    message = FakeMessage(b"", message_id="x")
    scheduler.schedule(lambda m: None, message)
    assert message.nacked is True


async def test_scheduler_close_nacks_buffered_and_is_idempotent() -> None:
    # close() is the single loop-side teardown primitive: it must hand every
    # buffered-but-undispatched message a nack (the manager's own shutdown nacks
    # only what shutdown() returns, which is nothing here) and stay safe to call
    # again from any of the teardown paths.
    send, recv = anyio.create_memory_object_stream[object](
        max_buffer_size=float("inf")
    )
    scheduler = _PortalScheduler(cast(Any, None), send, recv)

    buffered = [FakeMessage(b"", message_id=str(i)) for i in range(3)]
    for message in buffered:
        send.send_nowait(message)

    scheduler.close()
    assert all(message.nacked for message in buffered)
    assert not any(message.acked for message in buffered)

    scheduler.close()  # idempotent

    # New deliveries racing the close are nacked, not stranded on a dead stream.
    late = FakeMessage(b"", message_id="late")
    scheduler._enqueue(late)
    assert late.nacked is True

    recv.close()


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


async def test_message_scheduled_after_teardown_is_nacked() -> None:
    # The library can keep dispatching from its background threads while the
    # consumer tears down; once the portal is gone the message must be nacked
    # for redelivery, never silently dropped.
    client, subscriber = make_client_subscriber([])

    async def consume() -> None:
        async with subscriber.stream():
            await anyio.sleep_forever()

    task = asyncio.create_task(consume())
    while client.scheduler is None:
        await asyncio.sleep(0)
    task.cancel()
    try:
        await asyncio.wait_for(task, timeout=1)
    except asyncio.CancelledError:
        pass

    late = FakeMessage(b"", message_id="late")
    client.scheduler.schedule(lambda m: None, late)
    assert late.nacked is True


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
