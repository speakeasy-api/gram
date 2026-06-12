"""Type-safe Pub/Sub subscriber over a broker-resolved subscription.

Unmarshals each delivered message back into a fresh instance of the topic's
proto type and surfaces it together with its delivery metadata. Two consumption
styles are available:

- :meth:`Subscriber.receive` — the callback form. Returning from the callback
  acks the message; **raising** nacks it (triggering redelivery and eventual
  dead-lettering when the subscription declares a ``dead_letter`` policy).
  Handlers run concurrently up to a configurable bound.
- :meth:`Subscriber.stream` — the async-iterator form. Each item is a
  :class:`ReceivedMessage` that the caller acks or nacks explicitly.

A message that fails to unmarshal is nacked without reaching the consumer. In
the callback form, an exception raised by a handler is logged with diagnostic
context and nacked, so a single bad message surfaces instead of silently looping
and never tears down the receiver.
"""

from __future__ import annotations

import logging
import queue as queue_module
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from typing import Any, AsyncIterator, Awaitable, Callable, Generic, Optional, TypeVar

import anyio
import anyio.from_thread
import anyio.to_thread
from anyio.streams.memory import MemoryObjectReceiveStream, MemoryObjectSendStream
from google.cloud.pubsub_v1.subscriber.scheduler import Scheduler
from google.protobuf.message import DecodeError, Message

from .broker import SubscriberBroker, SubscriberHandle


__all__ = [
    "MessageMetadata",
    "ReceivedMessage",
    "Subscriber",
    "MessageCallback",
    "pubsub_subscriber_for_message",
]

M = TypeVar("M", bound=Message)

# Default ceiling on concurrently-executing handler tasks. The library's own
# flow control admits up to ~1000 outstanding messages by default; without an
# app-level cap a backlog of slow handlers would spawn that many concurrent
# tasks. A modest default keeps fan-out bounded while leaving plenty of
# parallelism for typical I/O-bound handlers.
_DEFAULT_MAX_CONCURRENCY = 50


@dataclass
class MessageMetadata:
    """Delivery metadata carried alongside a received message."""

    # Broker-assigned unique identifier for the message.
    id: str
    # Attributes carried with the payload (includes content-type and schema).
    attributes: dict[str, str] = field(default_factory=dict)
    # Number of delivery attempts. Set (starting at 1) only when dead-lettering
    # is enabled for the subscription; otherwise None.
    delivery_attempt: Optional[int] = None


# A callback returns None to ack; raising any exception nacks the message.
MessageCallback = Callable[[M, MessageMetadata], Awaitable[None]]


@dataclass
class ReceivedMessage(Generic[M]):
    """A message delivered by :meth:`Subscriber.stream`, with explicit disposition.

    The callback form (:meth:`Subscriber.receive`) ties ack/nack to the callback's
    outcome — returning acks, raising nacks. The streaming form can't: a raised
    exception unwinds the caller's ``async for`` rather than nacking one message,
    so disposition is handed to the caller instead. Call :meth:`ack` once the
    message is processed, or :meth:`nack` to trigger redelivery (and eventual
    dead-lettering when the subscription declares a ``dead_letter`` policy). A
    message left undisposed is redelivered after its ack deadline expires. Both
    methods are idempotent; the first call wins.
    """

    message: M
    metadata: MessageMetadata
    # The underlying google-cloud-pubsub Message (duck-typed: ``.ack()``/``.nack()``).
    _raw: Any = field(repr=False)
    _disposed: bool = field(default=False, repr=False)

    def ack(self) -> None:
        """Acknowledge the message so the broker stops redelivering it."""
        if self._disposed:
            return
        self._disposed = True
        self._raw.ack()

    def nack(self) -> None:
        """Negatively acknowledge the message, triggering redelivery."""
        if self._disposed:
            return
        self._disposed = True
        self._raw.nack()


class _PortalScheduler(Scheduler):
    """Pub/Sub scheduler that bridges library threads to the event loop.

    The google-cloud-pubsub library invokes ``schedule`` from its own background
    threads, but the receive loop runs on the event loop. Rather than parking a
    dedicated worker thread blocked in ``queue.get`` (which would pin an anyio
    thread-pool slot for the lifetime of every subscriber), this scheduler uses
    an :class:`anyio.from_thread.BlockingPortal` to hand each message directly to
    an anyio memory object stream that the receive loop owns. Library threads
    call into the portal; the event loop drains the stream — no extra worker
    thread is involved.

    ``queue`` exposes a plain thread-safe ``Queue`` that the library wires into
    its Dispatcher and stamps onto every ``Message`` as the request queue used by
    ``ack()``/``nack()``. That queue is load-bearing for ack/nack delivery and is
    owned end-to-end by the library; we merely supply it.
    """

    def __init__(
        self,
        portal: anyio.from_thread.BlockingPortal,
        send_stream: MemoryObjectSendStream,
    ) -> None:
        # Back-channel queue the library's Dispatcher consumes to deliver
        # ack/nack RPCs. We only construct it; the library owns its contents.
        self._queue: queue_module.Queue = queue_module.Queue()
        self._portal = portal
        self._send = send_stream
        # Guards the closed-check + enqueue against the shutdown path so a
        # message scheduled concurrently with shutdown is never stranded on a
        # closed stream (it is nacked instead).
        self._lock = anyio.Lock()
        self._closed = False

    @property
    def queue(self) -> queue_module.Queue:
        return self._queue

    def schedule(self, callback, *args, **kwargs) -> None:
        message = args[0] if args else None
        if message is None:
            return
        # ``schedule`` runs on a library thread; route the message onto the
        # event loop via the portal. ``_enqueue`` performs the closed-check and
        # the send atomically so there is no TOCTOU window in which a message
        # gets put onto a stream that shutdown has already closed.
        try:
            self._portal.call(self._enqueue, message)
        except BaseException:  # noqa: BLE001 - never raise back into the library thread
            # The portal call can fail if the event loop is already gone
            # (RuntimeError) or being torn down (the scheduled call is cancelled,
            # surfacing CancelledError). Either way nack so the broker redelivers
            # rather than dropping the message; raising here would only crash the
            # library's background dispatch thread.
            message.nack()

    async def _enqueue(self, message) -> None:
        async with self._lock:
            if self._closed:
                message.nack()
                return
            await self._send.send(message)

    def shutdown(self, await_msg_callbacks: bool = True):
        """Close the message stream and return any buffered, undispatched messages.

        The stock ``ThreadScheduler.shutdown`` returns its queued-but-undispatched
        messages so the streaming-pull manager can nack them; returning ``[]``
        would leak any still-buffered message (neither acked nor nacked),
        delaying redelivery and inflating ``delivery_attempt`` toward the DLQ
        threshold on every restart. Drain the buffer and hand the messages back
        so the manager nacks them.

        ``await_msg_callbacks`` is honored by the receive loop itself: it passes
        ``await_callbacks_on_shutdown=True`` to ``subscribe`` and only returns
        from its task group once in-flight handlers have drained.
        """
        try:
            return self._portal.call(self._drain_and_close)
        except BaseException:  # noqa: BLE001 - never raise back into the library thread
            # The library calls this from a background shutdown thread, which can
            # race our own teardown: the portal may already be gone (RuntimeError)
            # or mid-cancellation (CancelledError) when we cancel the streaming
            # pull from __aexit__. The loop side closes the stream there anyway,
            # so report no dropped messages rather than letting the exception
            # escape and crash the library's shutdown thread.
            return []

    async def _drain_and_close(self):
        async with self._lock:
            self._closed = True
            self._send.close()
        # The receiver drains whatever it can off its end; anything still sitting
        # in the stream's buffer is returned here so the manager nacks it.
        drained: list = []
        receiver = getattr(self._send, "_state", None)
        if receiver is not None:
            buffer = getattr(receiver, "buffer", None)
            if buffer is not None:
                while buffer:
                    drained.append(buffer.popleft())
        return drained


class Subscriber(Generic[M]):
    """Receives messages of a single proto type from a fixed subscription."""

    def __init__(
        self,
        handle: SubscriberHandle,
        message_type: type[M],
        *,
        logger: logging.Logger,
        topic_proto_name: str,
        subscription_proto_name: str,
        max_concurrency: int = _DEFAULT_MAX_CONCURRENCY,
    ) -> None:
        self._handle = handle
        self._message_type = message_type
        self._logger = logger
        self._topic_proto_name = topic_proto_name
        self._subscription_proto_name = subscription_proto_name
        self._max_concurrency = max_concurrency

    async def receive(
        self,
        callback: MessageCallback[M],
        *,
        timeout: float | None = None,
        max_concurrency: int | None = None,
    ) -> None:
        """Receive messages, blocking until cancelled or ``timeout`` elapses.

        Library threads hand each message to a :class:`BlockingPortal`, which
        pushes it onto an anyio memory object stream this loop drains; a handler
        task is spawned per message into an anyio task group that tracks and
        drains them on a graceful stop. A capacity limiter bounds how many
        handlers run at once.

        The streaming-pull future is the one place we still park a worker thread:
        the synchronous subscriber client exposes only a blocking
        ``future.result()``, so a single ``to_thread`` slot stays parked per
        subscriber waiting for the stream to end. The previous design parked a
        *second* thread blocked on ``scheduler.get``; routing messages through
        the portal removes that one.
        """
        limit = max_concurrency if max_concurrency is not None else self._max_concurrency
        limiter = anyio.CapacityLimiter(limit) if limit > 0 else None

        send_stream, receive_stream = anyio.create_memory_object_stream[object](
            max_buffer_size=float("inf")
        )

        async with anyio.from_thread.BlockingPortal() as portal:
            scheduler, future = self._start_subscription(portal, send_stream)

            try:
                with anyio.move_on_after(timeout):  # None => no deadline
                    async with anyio.create_task_group() as tg:

                        async def watch_stream() -> None:
                            # The streaming pull blocks until the future is
                            # cancelled or errors; either way, stop the loop by
                            # closing the message stream from the scheduler side.
                            await anyio.to_thread.run_sync(
                                future.result, abandon_on_cancel=True
                            )
                            await scheduler._drain_and_close()

                        tg.start_soon(watch_stream)

                        async for message in receive_stream:
                            tg.start_soon(
                                self._handle_message, message, callback, limiter
                            )
            finally:
                future.cancel()
                # Close the stream so any in-flight ``schedule`` is nacked rather
                # than stranded, and so the receive loop terminates promptly. Safe
                # to call repeatedly.
                await scheduler._drain_and_close()

    @asynccontextmanager
    async def stream(self) -> AsyncIterator[_MessageIterator[M]]:
        """Receive messages as an async iterator instead of via a callback.

        Use as an async context manager wrapping an ``async for``::

            async with subscriber.stream() as messages:
                async for received in messages:
                    handle(received.message, received.metadata)
                    received.ack()  # or received.nack()

        Each item is a :class:`ReceivedMessage` carrying the decoded proto, its
        :class:`MessageMetadata`, and explicit ``ack``/``nack`` — unlike
        ``receive``, which acks on callback return and nacks on raise. Malformed
        payloads are nacked and skipped, never yielded.

        Iteration is single-consumer and processes one message at a time; for the
        concurrent, bounded fan-out of ``receive`` keep using that. Apply a
        deadline by wrapping the loop in ``anyio.move_on_after``/``fail_after``.

        Implemented with ``@asynccontextmanager`` rather than a hand-written
        ``__aenter__``/``__aexit__`` so the BlockingPortal's scope nests cleanly:
        holding it open across iteration via an ExitStack corrupts trio's nursery
        stack. There is no background watcher task — when the streaming pull ends
        the library calls the scheduler's ``shutdown``, which closes the stream
        and ends the iterator; a subscription error therefore stops the loop
        rather than surfacing as an exception.
        """
        send_stream, receive_stream = anyio.create_memory_object_stream[object](
            max_buffer_size=float("inf")
        )

        # The portal lets the scheduler hop messages from library threads onto
        # this loop. It is a properly nested ``async with`` here, so trio sees it
        # opened and closed on the consumer's own task.
        async with anyio.from_thread.BlockingPortal() as portal:
            scheduler, future = self._start_subscription(portal, send_stream)
            try:
                yield _MessageIterator(self, receive_stream)
            finally:
                # Stop the streaming pull, then close the stream. Shield the close
                # so it still runs when the loop is being cancelled (Ctrl-C);
                # it is idempotent with the scheduler's own shutdown-time close.
                future.cancel()
                with anyio.CancelScope(shield=True):
                    await scheduler._drain_and_close()

    async def _handle_message(
        self,
        message,
        callback: MessageCallback[M],
        limiter: anyio.CapacityLimiter | None = None,
    ) -> None:
        """Process one incoming message: unmarshal, dispatch, ack/nack.

        ``message`` duck-types the google-cloud-pubsub Message (``.data``,
        ``.attributes``, ``.message_id``, ``.delivery_attempt``, ``.ack()``,
        ``.nack()``), which keeps this logic unit-testable without a live broker.
        """
        if limiter is not None:
            async with limiter:
                await self._dispatch(message, callback)
        else:
            await self._dispatch(message, callback)

    def _start_subscription(
        self,
        portal: anyio.from_thread.BlockingPortal,
        send_stream: MemoryObjectSendStream,
    ) -> tuple[_PortalScheduler, Any]:
        """Wire a ``_PortalScheduler`` into ``client.subscribe`` and return both.

        Shared by ``receive`` and ``stream``: each opens its own portal + memory
        stream, then routes the library's streaming pull through the scheduler.
        """
        scheduler = _PortalScheduler(portal, send_stream)
        future = self._handle.client.subscribe(
            self._handle.subscription_path,
            callback=lambda message: None,  # dispatch flows through the scheduler
            # The stub types this as ThreadScheduler, but subscribe accepts any
            # Scheduler subclass (per its own docstring).
            scheduler=scheduler,  # pyrefly: ignore[bad-argument-type]
            await_callbacks_on_shutdown=True,
        )
        return scheduler, future

    def _parse(self, message) -> tuple[M, MessageMetadata] | None:
        """Unmarshal a raw message into its proto type + metadata.

        Returns ``None`` (after nacking) when the payload fails to decode, so a
        malformed message never reaches a callback or the stream consumer.
        """
        delivery_attempt = getattr(message, "delivery_attempt", None)

        instance = self._message_type()
        try:
            instance.ParseFromString(message.data)
        except DecodeError:
            self._logger.warning(
                "failed to unmarshal pubsub message",
                extra={
                    "topic_proto_name": self._topic_proto_name,
                    "subscription_proto_name": self._subscription_proto_name,
                    "message_id": message.message_id,
                },
            )
            message.nack()
            return None

        metadata = MessageMetadata(
            id=message.message_id,
            attributes=dict(message.attributes),
            delivery_attempt=delivery_attempt,
        )
        return instance, metadata

    async def _dispatch(self, message, callback: MessageCallback[M]) -> None:
        delivery_attempt = getattr(message, "delivery_attempt", None)

        parsed = self._parse(message)
        if parsed is None:
            return
        instance, metadata = parsed

        try:
            await callback(instance, metadata)
        except BaseException as exc:  # noqa: BLE001 - isolate one bad message
            # Cooperative cancellation must propagate: a cancelled handler is
            # neither acked nor nacked here. The library was started with
            # await_callbacks_on_shutdown=True, so on a *graceful* stop in-flight
            # handlers run to completion before the stream closes; on cancel the
            # message is simply left in flight and redelivery recovers it.
            if isinstance(exc, anyio.get_cancelled_exc_class()):
                raise
            # The callback raised — either a deliberate nack signal or an
            # unexpected error. Catch BaseException (not just Exception) so that
            # even a SystemExit-style error from a single message can't tear down
            # the receive loop. Log with full diagnostic context and nack so the
            # message is redelivered, and eventually dead-lettered if it keeps
            # failing.
            self._logger.error(
                "error processing pubsub message",
                exc_info=True,
                extra={
                    "topic_proto_name": self._topic_proto_name,
                    "subscription_proto_name": self._subscription_proto_name,
                    "message_id": message.message_id,
                    "delivery_attempt": delivery_attempt
                    if delivery_attempt is not None
                    else 0,
                },
            )
            message.nack()
            return
        message.ack()


class _MessageIterator(Generic[M]):
    """Async iterator yielded by ``Subscriber.stream``.

    Holds no scope of its own — the portal and streaming pull live in the
    ``stream`` context manager. This just drains the memory stream the scheduler
    feeds, decoding each message and skipping (after nacking) any that fail.
    """

    def __init__(
        self,
        subscriber: Subscriber[M],
        receive_stream: MemoryObjectReceiveStream,
    ) -> None:
        self._subscriber = subscriber
        self._recv = receive_stream

    def __aiter__(self) -> _MessageIterator[M]:
        return self

    async def __anext__(self) -> ReceivedMessage[M]:
        while True:
            try:
                raw = await self._recv.receive()
            except anyio.EndOfStream:
                raise StopAsyncIteration
            parsed = self._subscriber._parse(raw)
            if parsed is None:
                # Malformed payload: _parse already nacked it. Skip and pull next.
                continue
            instance, metadata = parsed
            return ReceivedMessage(instance, metadata, raw)


def pubsub_subscriber_for_message(
    broker: SubscriberBroker,
    message_type: type[M],
    subscription_type: type[Message],
    *,
    logger: logging.Logger | None = None,
) -> Subscriber[M]:
    """Return a subscriber for ``subscription_type`` delivering ``message_type`` messages.

    Raises ValueError if the message declares no topic or the subscription marker
    declares no subscription.
    """
    if message_type is None:
        raise ValueError("message type must not be None")
    if subscription_type is None:
        raise ValueError("subscription marker message type must not be None")

    handle = broker.subscriber_for_message(message_type, subscription_type)
    return Subscriber(
        handle,
        message_type,
        logger=logger or logging.getLogger(__name__),
        topic_proto_name=message_type.DESCRIPTOR.full_name,
        subscription_proto_name=subscription_type.DESCRIPTOR.full_name,
    )
