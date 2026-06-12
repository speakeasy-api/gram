"""Type-safe Pub/Sub publisher over a broker-resolved topic.

Python counterpart of ``infra/pkg/gcp/publisher.py`` (``publisher.go``). A
publisher marshals a proto message to binary and tags it with the same two
attributes the Go layer uses — ``content-type: application/x-protobuf`` and
``schema: <proto full name>`` — so messages are interoperable across languages.
"""

from __future__ import annotations

import threading
from typing import Any, Generic, TypeVar

import anyio
import anyio.from_thread

from google.protobuf.message import Message

from .broker import PublisherBroker, PublisherHandle

__all__ = ["Publisher", "pubsub_publisher_for_message", "CONTENT_TYPE"]

M = TypeVar("M", bound=Message)

# Attribute value mirrored from publisher.go; identifies the body encoding.
CONTENT_TYPE = "application/x-protobuf"


class Publisher(Generic[M]):
    """Publishes messages of a single proto type to a fixed topic."""

    def __init__(self, handle: PublisherHandle, schema: str) -> None:
        self._handle = handle
        self._schema = schema

    async def publish(self, message: M) -> str:
        """Marshal and publish a message; awaits delivery and returns the message ID."""
        # Guard against a runtime message whose type disagrees with the topic
        # this publisher was built for: otherwise we'd emit payload bytes tagged
        # with a mismatched ``schema`` attribute (and onto the wrong topic).
        actual = message.DESCRIPTOR.full_name
        if actual != self._schema:
            raise TypeError(
                f"message type {actual!r} does not match publisher schema "
                f"{self._schema!r}"
            )

        data = message.SerializeToString()
        # The client returns a ``concurrent.futures.Future`` resolved on a
        # background commit thread. Blocking a ``to_thread`` worker on
        # ``future.result()`` would pin one of the limited anyio thread-pool
        # slots per in-flight publish, throttling concurrent publishers behind
        # the pool limiter. Instead — mirroring the subscriber's scheduler
        # bridge — hop the completion signal back to the event loop through a
        # BlockingPortal via the future's done callback, so waiting costs no
        # worker thread at all.
        future = self._handle.client.publish(
            self._handle.topic_path,
            data,
            **{"content-type": CONTENT_TYPE, "schema": self._schema},
        )

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
                    portal.call(done.set)
                except BaseException:  # noqa: BLE001 - never raise into the commit thread
                    # The portal is gone: the publish was cancelled or the loop
                    # is tearing down, so no waiter remains. The send itself
                    # still proceeds on the commit thread.
                    pass

            future.add_done_callback(_on_done)
            await done.wait()

        # Resolved by now; raises the publish error if the send failed.
        return future.result()


def pubsub_publisher_for_message(
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
