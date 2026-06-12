"""Type-safe Pub/Sub subscriber over a broker-resolved subscription.

Unmarshals each delivered message back into a fresh instance of the topic's
proto type and surfaces it together with its delivery metadata. Two consumption
styles are available:

- :meth:`Subscriber.receive` — the callback form. Returning from the callback
  acks the message; **raising** nacks it (triggering redelivery and eventual
  dead-lettering when the subscription declares a ``dead_letter`` policy).
  Handlers run concurrently up to a configurable bound.
- :meth:`Subscriber.stream` — the async-iterator form. Each item is a
  :class:`ReceivedMessage` that the caller acks or nacks explicitly. A terminal
  subscription failure (subscription deleted, permission revoked, ...) is
  raised out of the iteration rather than silently ending it.

A message that fails to unmarshal is nacked without reaching the consumer. In
the callback form, an exception raised by a handler is logged with diagnostic
context and nacked, so a single bad message surfaces instead of silently looping
and never tears down the receiver.

Teardown disposition: a message that was delivered by the library but not yet
dispatched to a handler (or yielded to the stream consumer) when the session
ends is nacked, so the broker redelivers it immediately instead of waiting for
its ack deadline to lapse — mirroring how the Go library and the stock
``ThreadScheduler`` dispose of undispatched messages.
"""

from __future__ import annotations

import logging
import queue as queue_module
import threading
from concurrent.futures import CancelledError as FutureCancelledError
from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from typing import (
    Any,
    AsyncIterator,
    Awaitable,
    Callable,
    Generic,
    NoReturn,
    Optional,
    TypeVar,
)

import anyio
import anyio.from_thread
import anyio.to_thread
from anyio.streams.memory import MemoryObjectReceiveStream, MemoryObjectSendStream
from google.cloud.pubsub_v1.subscriber.scheduler import Scheduler
from google.protobuf.message import Message

from .broker import SubscriberBroker, SubscriberHandle


__all__ = [
    "MessageMetadata",
    "ReceivedMessage",
    "Subscriber",
    "MessageCallback",
    "pubsub_subscriber_for_message",
]

M = TypeVar("M", bound=Message)

def _unwrap_single(eg: BaseExceptionGroup) -> BaseException:
    """Peel single-member exception groups down to the lone leaf exception.

    Both ``receive``/``stream`` host task groups (their own and the
    BlockingPortal's), and anyio wraps any exception crossing a task group in a
    ``BaseExceptionGroup`` — even a lone one. The only expected failure is a
    terminal subscription error, so surface it directly (the way Go's
    ``Receive`` returns the error) instead of making every caller match on
    nested groups. Groups with several members are returned as-is.
    """
    exc: BaseException = eg
    while isinstance(exc, BaseExceptionGroup) and len(exc.exceptions) == 1:
        exc = exc.exceptions[0]
    return exc


# Default ceiling on concurrently-executing handler tasks. The library's own
# flow control admits up to ~1000 outstanding messages by default; without an
# app-level cap a backlog of slow handlers would spawn that many concurrent
# tasks. A modest default keeps fan-out bounded while leaving plenty of
# parallelism for typical I/O-bound handlers. A value <= 0 disables the bound
# entirely (handlers limited only by the library's flow control).
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
    fire messages through the portal without blocking; the event loop drains the
    stream — no extra worker thread is involved.

    All mutable scheduling state (``_closed``, the stream, the in-flight handler
    count) is touched only on the event-loop thread, which runs tasks to
    completion between checkpoints — so no locks are needed: every loop-side
    method here is checkpoint-free (no ``await``), making each one atomic with
    respect to the others.

    ``queue`` exposes a plain thread-safe ``Queue`` that the library wires into
    its Dispatcher and stamps onto every ``Message`` as the request queue used by
    ``ack()``/``nack()``. That queue is load-bearing for ack/nack delivery and is
    owned end-to-end by the library; we merely supply it.
    """

    def __init__(
        self,
        portal: anyio.from_thread.BlockingPortal,
        send_stream: MemoryObjectSendStream,
        receive_stream: MemoryObjectReceiveStream,
    ) -> None:
        # Back-channel queue the library's Dispatcher consumes to deliver
        # ack/nack RPCs. We only construct it; the library owns its contents.
        self._queue: queue_module.Queue = queue_module.Queue()
        self._portal = portal
        self._send = send_stream
        self._receive = receive_stream
        # --- Event-loop-thread-only state below. ---
        self._closed = False
        # Handler tasks the receive loop currently has in flight. ``shutdown``
        # blocks on ``_idle`` so the library's Dispatcher is not stopped while
        # handlers that will still ack/nack are running (their requests would be
        # enqueued after the Dispatcher's STOP sentinel and silently dropped).
        self._inflight = 0
        self._idle = threading.Event()
        self._idle.set()

    @property
    def queue(self) -> queue_module.Queue:
        return self._queue

    def schedule(self, callback, *args, **kwargs) -> None:
        """Hand one message from a library thread to the event loop.

        Fire-and-forget: ``start_task_soon`` does not park the library's single
        dispatch thread on an event-loop round trip per message (``portal.call``
        would serialize intake at one loop-latency apiece while the library holds
        its pause/resume lock). If the enqueue cannot run — the portal is gone or
        the task is cancelled during teardown — the message is nacked so the
        broker redelivers it rather than dropping it.
        """
        message = args[0] if args else None
        if message is None:
            return
        try:
            future = self._portal.start_task_soon(self._enqueue, message)
        except BaseException:  # noqa: BLE001 - never raise back into the library thread
            # The portal is already stopped (RuntimeError) or going away. Nack so
            # the broker redelivers; raising here would only crash the library's
            # background dispatch thread.
            message.nack()
            return

        def _nack_on_failure(f) -> None:
            try:
                failed = f.cancelled() or f.exception() is not None
            except BaseException:  # noqa: BLE001 - runs on arbitrary threads
                failed = True
            if failed:
                message.nack()

        future.add_done_callback(_nack_on_failure)

    def _enqueue(self, message) -> None:
        # Runs on the event loop via the portal. Checkpoint-free, so the
        # closed-check and the send are atomic with respect to ``close()`` — no
        # TOCTOU window in which a message lands on a stream that teardown has
        # already drained.
        if self._closed:
            message.nack()
            return
        try:
            self._send.send_nowait(message)  # unbounded buffer: never blocks
        except (anyio.ClosedResourceError, anyio.BrokenResourceError):
            message.nack()

    def track_handler(self) -> None:
        """Record one dispatched handler task. Event-loop thread only."""
        self._inflight += 1
        self._idle.clear()

    def handler_done(self) -> None:
        """Record one finished (or cancelled) handler task. Event-loop thread only."""
        self._inflight -= 1
        if self._inflight == 0:
            self._idle.set()

    def close(self) -> None:
        """Stop intake and nack every buffered-but-undispatched message.

        Event-loop thread only; idempotent. Checkpoint-free (no ``await``), so it
        runs to completion inside a ``finally`` even while the surrounding task
        is being cancelled — no shield required. Draining through the receive
        stream's public ``receive_nowait`` (rather than anyio's private buffer
        state) hands each stranded message a nack so the broker redelivers it
        immediately instead of waiting out the ack deadline.
        """
        if self._closed:
            return
        self._closed = True
        self._send.close()
        while True:
            try:
                message = self._receive.receive_nowait()
            except (anyio.EndOfStream, anyio.WouldBlock, anyio.ClosedResourceError):
                break
            message.nack()

    def shutdown(self, await_msg_callbacks: bool = True) -> list:
        """Library-initiated shutdown: close intake, then wait for handlers.

        Called by the streaming-pull manager's background shutdown thread. The
        stock ``ThreadScheduler.shutdown`` returns its queued-but-undispatched
        messages so the manager can nack them; here ``close()`` already nacks
        them on the event loop, so this always reports no dropped messages.

        With ``await_msg_callbacks`` the stock scheduler blocks until its
        executor's callbacks finish — and the manager stops its Dispatcher right
        after this returns, silently dropping any ack/nack enqueued later. Honor
        the same contract by blocking until the receive loop's in-flight handler
        tasks have drained, so their acks are dispatched before the Dispatcher
        stops.
        """
        try:
            self._portal.call(self.close)
        except BaseException:  # noqa: BLE001 - never raise back into the library thread
            # The portal (and with it the event loop side) is already gone: the
            # loop-side teardown in Subscriber._session closes and nacks on its
            # own task, so nothing is stranded. Report no dropped messages rather
            # than crashing the library's shutdown thread.
            return []
        if await_msg_callbacks:
            self._idle.wait()
        return []


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
        # Default handler-concurrency bound for ``receive``; a value <= 0
        # disables the bound. Overridable per call via ``receive(max_concurrency=...)``.
        self._max_concurrency = max_concurrency

    @asynccontextmanager
    async def _session(
        self,
    ) -> AsyncIterator[tuple[_PortalScheduler, Any, MemoryObjectReceiveStream]]:
        """Portal + scheduler + streaming-pull plumbing shared by receive/stream.

        Teardown is fully synchronous (checkpoint-free), so it runs to completion
        even when the surrounding task is being cancelled — the Ctrl-C path needs
        no shield: the streaming pull is cancelled, the scheduler closes intake
        and nacks everything buffered, and the receive stream is closed (so no
        unclosed-stream ResourceWarning at GC time).
        """
        send_stream, receive_stream = anyio.create_memory_object_stream[object](
            max_buffer_size=float("inf")
        )

        async with anyio.from_thread.BlockingPortal() as portal:
            scheduler = _PortalScheduler(portal, send_stream, receive_stream)
            future = self._handle.client.subscribe(
                self._handle.subscription_path,
                callback=lambda message: None,  # dispatch flows through the scheduler
                # The stub types this as ThreadScheduler, but subscribe accepts
                # any Scheduler subclass (per its own docstring).
                scheduler=scheduler,  # pyrefly: ignore[bad-argument-type]
                await_callbacks_on_shutdown=True,
            )
            try:
                yield scheduler, future, receive_stream
            finally:
                future.cancel()
                scheduler.close()
                receive_stream.close()

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
        handlers run at once; ``max_concurrency`` overrides the subscriber's
        default for this call, and a value <= 0 disables the bound.

        A terminal subscription failure (subscription deleted, permission
        revoked, ...) is raised out of this call, mirroring how the Go library's
        ``Receive`` returns the error.

        The streaming-pull future is the one place we still park a worker thread:
        the synchronous subscriber client exposes only a blocking
        ``future.result()``, so a single ``to_thread`` slot stays parked per
        subscriber waiting for the stream to end.
        """
        limit = max_concurrency if max_concurrency is not None else self._max_concurrency
        limiter = anyio.CapacityLimiter(limit) if limit > 0 else None

        try:
            async with self._session() as (scheduler, future, receive_stream):
                with anyio.move_on_after(timeout):  # None => no deadline
                    async with anyio.create_task_group() as tg:

                        async def watch_stream() -> None:
                            # Blocks until the streaming pull ends: re-raises a
                            # terminal subscription error, or returns when the
                            # pull was cancelled; either way close intake so the
                            # receive loop below terminates.
                            try:
                                await anyio.to_thread.run_sync(
                                    future.result, abandon_on_cancel=True
                                )
                            finally:
                                scheduler.close()

                        async def run_handler(message) -> None:
                            try:
                                await self._handle_message(message, callback, limiter)
                            finally:
                                scheduler.handler_done()

                        tg.start_soon(watch_stream)

                        async for message in receive_stream:
                            # Track before spawning: there is no checkpoint
                            # between the stream pop and this call, so the
                            # scheduler's in-flight count can never miss a
                            # popped message.
                            scheduler.track_handler()
                            tg.start_soon(run_handler, message)
        except BaseExceptionGroup as eg:
            unwrapped = _unwrap_single(eg)
            if unwrapped is eg:
                raise
            raise unwrapped from None

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

        If the streaming pull dies with a terminal error (subscription deleted,
        ``PermissionDenied``, ...), that error is raised out of the ``async
        for`` rather than ending the iteration silently, so a consumer cannot
        keep running with a dead subscription.

        Implemented with ``@asynccontextmanager`` rather than a hand-written
        ``__aenter__``/``__aexit__`` so the BlockingPortal's scope nests cleanly:
        holding it open across iteration via an ExitStack corrupts trio's nursery
        stack.
        """
        try:
            async with self._session() as (_, future, receive_stream):
                yield _MessageIterator(self, receive_stream, future)
        except BaseExceptionGroup as eg:
            # The portal's task group wraps exceptions crossing it (including
            # the consumer's own); unwrap lone leaves for transparency.
            unwrapped = _unwrap_single(eg)
            if unwrapped is eg:
                raise
            raise unwrapped from None

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
        if limiter is None:
            await self._dispatch(message, callback)
            return
        try:
            await limiter.acquire()
        except anyio.get_cancelled_exc_class():
            # Cancelled while queued behind the concurrency bound: the message
            # never reached a handler, so dispose of it like any other
            # undispatched message — nack for immediate redelivery instead of
            # leaving it leased until the ack deadline lapses.
            message.nack()
            raise
        try:
            await self._dispatch(message, callback)
        finally:
            limiter.release()

    def _parse(self, message) -> tuple[M, MessageMetadata] | None:
        """Unmarshal a raw message into its proto type + metadata.

        Returns ``None`` (after nacking) when the payload fails to decode, so a
        malformed message never reaches a callback or the stream consumer.
        Catches ``Exception`` rather than just ``DecodeError``: under the
        pure-Python protobuf backend a malformed payload can surface as e.g.
        ``UnicodeDecodeError``, and letting it escape would tear down the whole
        receive loop over one poison message (the Go layer likewise nacks on any
        unmarshal error). Only synchronous code runs in the ``try``, so no
        cancellation exception can be swallowed here.
        """
        delivery_attempt = getattr(message, "delivery_attempt", None)

        instance = self._message_type()
        try:
            instance.ParseFromString(message.data)
        except Exception:
            self._logger.warning(
                "failed to unmarshal pubsub message",
                exc_info=True,
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
                    "message_id": metadata.id,
                    "delivery_attempt": metadata.delivery_attempt
                    if metadata.delivery_attempt is not None
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
        future: Any,
    ) -> None:
        self._subscriber = subscriber
        self._recv = receive_stream
        self._future = future

    def __aiter__(self) -> _MessageIterator[M]:
        return self

    async def __anext__(self) -> ReceivedMessage[M]:
        while True:
            try:
                raw = await self._recv.receive()
            except anyio.EndOfStream:
                await self._end_of_stream()
            parsed = self._subscriber._parse(raw)
            if parsed is None:
                # Malformed payload: _parse already nacked it. Skip and pull next.
                continue
            instance, metadata = parsed
            return ReceivedMessage(instance, metadata, raw)

    async def _end_of_stream(self) -> NoReturn:
        """The scheduler closed the stream: distinguish a clean stop from a dead
        subscription.

        The stream closes either because our own teardown closed it (the
        consumer already left the loop, so it never observes that) or because
        the library shut the pull down. Wait for the streaming-pull future to
        settle and re-raise its terminal error, so a dead subscription
        (deleted, ``PermissionDenied``, ...) surfaces to the consumer instead of
        ending the iteration as if the backlog were merely drained.
        """
        future = self._future
        if future.cancelled():
            raise StopAsyncIteration
        try:
            await anyio.to_thread.run_sync(future.result, abandon_on_cancel=True)
        except FutureCancelledError:
            # On asyncio this exception type doubles as task cancellation;
            # disambiguate via the future's own state.
            if future.cancelled():
                raise StopAsyncIteration from None
            raise
        raise StopAsyncIteration


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
