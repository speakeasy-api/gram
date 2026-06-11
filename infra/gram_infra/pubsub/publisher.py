"""Type-safe Pub/Sub publisher over a broker-resolved topic.

Python counterpart of ``infra/pkg/gcp/publisher.py`` (``publisher.go``). A
publisher marshals a proto message to binary and tags it with the same two
attributes the Go layer uses — ``content-type: application/x-protobuf`` and
``schema: <proto full name>`` — so messages are interoperable across languages.
"""

from __future__ import annotations

from concurrent.futures import Future
from typing import Generic, TypeVar

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

    def publish(self, message: M) -> Future:
        """Marshal and publish a message; returns the publish future (resolves to the message ID)."""
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
        return self._handle.client.publish(
            self._handle.topic_path,
            data,
            **{"content-type": CONTENT_TYPE, "schema": self._schema},
        )


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
