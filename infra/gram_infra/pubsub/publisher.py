"""Type-safe Pub/Sub publisher over a broker-resolved topic.

Python counterpart of ``infra/pkg/gcp/publisher.go``. A publisher marshals a
proto message to binary and tags it with the same two attributes the Go layer
uses — ``content-type: application/x-protobuf`` and ``schema: <proto full
name>`` — so messages are interoperable across languages.
"""

from __future__ import annotations

import concurrent.futures
import threading
from typing import Any, Protocol, runtime_checkable

import anyio
import anyio.from_thread
from google.protobuf.message import Message
from opentelemetry import propagate

from .broker import PublisherBroker, PublisherHandle

__all__ = [
    "CONTENT_TYPE",
    "PublishResult",
    "Publisher",
    "pubsub_publisher_for_message",
]

# Attribute value mirrored from publisher.go; identifies the body encoding.
CONTENT_TYPE = "application/x-protobuf"


def _message_attributes(schema: str) -> dict[str, str]:
    """Build the attribute set carried with an outgoing message.

    Mirrors ``messageAttributes`` in ``publisher.go``: the ``content-type`` and
    ``schema`` markers the subscriber uses to decode the payload, plus any trace
    context injected from the active span so the subscriber can continue the
    producer's trace. Injection uses the globally configured propagator (W3C
    tracecontext + baggage by default, the same one the receiver extracts with);
    with no active span it is a no-op and no propagation attributes are added.
    """
    attributes = {"content-type": CONTENT_TYPE, "schema": schema}
    propagate.inject(attributes)
    return attributes


@runtime_checkable
class PublishResult(Protocol):
    """Future-like handle to an in-flight publish.

    Python counterpart of the ``PublishResult`` interface in ``publisher.go``:
    :meth:`Publisher.publish` returns one of these immediately, without waiting
    for the broker to acknowledge the send, so a caller can fan out many
    publishes and only then collect them. Await :meth:`get` to wait for the
    commit and obtain the server-assigned message id (or surface the send error).
    """

    async def get(self) -> str:
        """Await the commit and return the message id, raising on failure."""
        ...


class _FuturePublishResult:
    """A :class:`PublishResult` backed by the client's ``concurrent.futures.Future``."""

    def __init__(self, future: concurrent.futures.Future) -> None:
        self._future = future

    async def get(self) -> str:
        # The client resolves the future on a background commit thread. Blocking a
        # ``to_thread`` worker on ``future.result()`` would pin one of the limited
        # anyio thread-pool slots per in-flight publish, throttling concurrent
        # collectors behind the pool limiter. Instead — mirroring the subscriber's
        # scheduler bridge — hop the completion signal back to the event loop
        # through a BlockingPortal via the future's done callback, so waiting costs
        # no worker thread at all.
        future = self._future
        done = anyio.Event()
        loop_thread = threading.get_ident()

        async with anyio.from_thread.BlockingPortal() as portal:

            def _on_done(_future: Any) -> None:
                if threading.get_ident() == loop_thread:
                    # ``add_done_callback`` ran the callback inline because the
                    # future had already resolved — we are synchronously on the
                    # event-loop thread, so set the event directly (the portal
                    # rejects calls from its own loop thread).
                    done.set()
                    return
                try:
                    # Fire-and-forget: a blocking ``portal.call`` would park the
                    # library's single commit thread for a full event-loop round
                    # trip per completion, serializing batch-resolved publishes
                    # at loop latency apiece.
                    portal.start_task_soon(done.set)
                except BaseException:
                    # The portal is gone: the get was cancelled or the loop is
                    # tearing down, so no waiter remains. The send itself still
                    # proceeds on the commit thread.
                    pass

            future.add_done_callback(_on_done)
            await done.wait()

        # Resolved by now; raises the publish error if the send failed.
        return future.result()


class _ErrorPublishResult:
    """A :class:`PublishResult` that fails on :meth:`get`.

    Mirrors Go's ``errPublishResult``: when ``publish`` cannot even hand the
    message to the client (e.g. it fails to serialize), the error is deferred to
    ``get`` so the call site is uniform — every publish returns a result and
    surfaces failure the same way.
    """

    def __init__(self, exc: Exception) -> None:
        self._exc = exc

    async def get(self) -> str:
        raise self._exc


class Publisher[M: Message]:
    """Publishes messages of a single proto type to a fixed topic."""

    def __init__(self, handle: PublisherHandle, schema: str) -> None:
        self._handle = handle
        self._schema = schema

    def publish(self, message: M) -> PublishResult:
        """Hand a message to the client and return a future for its commit.

        Returns immediately with a :class:`PublishResult`; the send commits on
        the client's background batcher. Await the result's :meth:`~PublishResult.get`
        to wait for delivery and read the message id. Mirrors ``Publish`` in
        ``publisher.go``, which likewise returns a future rather than blocking.
        """
        # Guard against a runtime message whose type disagrees with the topic
        # this publisher was built for: otherwise we'd emit payload bytes tagged
        # with a mismatched ``schema`` attribute (and onto the wrong topic). This
        # is a programming error, so raise rather than defer it to ``get``.
        actual = message.DESCRIPTOR.full_name
        if actual != self._schema:
            raise TypeError(
                f"message type {actual!r} does not match publisher schema "
                f"{self._schema!r}"
            )

        try:
            data = message.SerializeToString()
        except Exception as exc:
            # Defer the failure to ``get`` (mirrors Go's errPublishResult) so the
            # call site treats every publish uniformly.
            return _ErrorPublishResult(exc)

        future = self._handle.client.publish(
            self._handle.topic_path,
            data,
            **_message_attributes(self._schema),
        )
        # Let the broker's teardown wait for this commit before it closes the
        # publisher's transport — even if the caller never awaits the result.
        self._handle.inflight.add(future)

        return _FuturePublishResult(future)


def pubsub_publisher_for_message[M: Message](
    broker: PublisherBroker, message_type: type[M]
) -> Publisher[M]:
    """Return a publisher for the topic declared by ``message_type``'s topic option.

    Mirrors PubSubPublisherForMessage in publisher.go. Raises ValueError if the
    message declares no topic.
    """
    if message_type is None:
        raise ValueError("message type must not be None")

    handle = broker.publisher_for_message(message_type)
    return Publisher(handle, message_type.DESCRIPTOR.full_name)


async def pubsub_publisher_for_message_async[M: Message](
    broker: PublisherBroker, message_type: type[M]
) -> Publisher[M]:
    """Async :func:`pubsub_publisher_for_message`.

    Resolves the handle via the broker's async path so an emulator topic
    reconcile runs off the event loop. Prefer this from async wiring.
    """
    if message_type is None:
        raise ValueError("message type must not be None")

    handle = await broker.publisher_for_message_async(message_type)
    return Publisher(handle, message_type.DESCRIPTOR.full_name)
