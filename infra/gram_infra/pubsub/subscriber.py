"""Type-safe Pub/Sub subscriber over a broker-resolved subscription.

Python counterpart of ``infra/pkg/gcp/subscriber.py`` (``subscriber.go``). The
subscriber unmarshals each message back into a fresh instance of the topic's
proto type and hands it to a callback along with delivery metadata.

Ack/nack semantics mirror the Go layer's callback contract: returning normally
acks the message; **raising** nacks it (triggering redelivery and eventual
dead-lettering when the subscription declares a ``dead_letter`` policy). A
message that fails to unmarshal is nacked without invoking the callback. Raised
exceptions are logged with diagnostic context — the analog of the Go layer's
panic recovery — so a bad message surfaces instead of silently looping.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Callable, Generic, Optional, TypeVar

from google.protobuf.message import DecodeError, Message

from .broker import SubscriberBroker, SubscriberHandle

if TYPE_CHECKING:
    from google.cloud.pubsub_v1.subscriber.futures import StreamingPullFuture

__all__ = [
    "MessageMetadata",
    "Subscriber",
    "MessageCallback",
    "pubsub_subscriber_for_message",
]

M = TypeVar("M", bound=Message)


@dataclass
class MessageMetadata:
    """Delivery metadata carried alongside a received message.

    Mirrors MessageMetadata in subscriber.go.
    """

    # Broker-assigned unique identifier for the message.
    id: str
    # Attributes carried with the payload (includes content-type and schema).
    attributes: dict[str, str] = field(default_factory=dict)
    # Number of delivery attempts. Set (starting at 1) only when dead-lettering
    # is enabled for the subscription; otherwise None.
    delivery_attempt: Optional[int] = None


# A callback returns None to ack; raising any exception nacks the message.
MessageCallback = Callable[[M, MessageMetadata], None]


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
    ) -> None:
        self._handle = handle
        self._message_type = message_type
        self._logger = logger
        self._topic_proto_name = topic_proto_name
        self._subscription_proto_name = subscription_proto_name

    def subscribe(self, callback: MessageCallback[M]) -> "StreamingPullFuture":
        """Start receiving in the background; returns the streaming-pull future.

        Non-blocking: the client dispatches messages to ``callback`` on its own
        threads. Cancel the returned future to stop. Use ``receive`` to block.
        """
        return self._handle.client.subscribe(
            self._handle.subscription_path,
            callback=lambda message: self._handle_message(message, callback),
        )

    def receive(
        self, callback: MessageCallback[M], *, timeout: float | None = None
    ) -> None:
        """Receive messages, blocking until cancelled or ``timeout`` elapses.

        Mirrors Go's blocking ``Receive``. A ``timeout`` (seconds) stops the
        receiver and returns; without one this blocks until interrupted.
        """
        future = self.subscribe(callback)
        try:
            future.result(timeout=timeout)
        except TimeoutError:
            future.cancel()
            future.result()
        except KeyboardInterrupt:
            future.cancel()
            future.result()

    def _handle_message(self, message, callback: MessageCallback[M]) -> None:
        """Process one incoming message: unmarshal, dispatch, ack/nack.

        ``message`` duck-types the google-cloud-pubsub Message (``.data``,
        ``.attributes``, ``.message_id``, ``.delivery_attempt``, ``.ack()``,
        ``.nack()``), which keeps this logic unit-testable without a live broker.
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
            return

        metadata = MessageMetadata(
            id=message.message_id,
            attributes=dict(message.attributes),
            delivery_attempt=delivery_attempt,
        )

        try:
            callback(instance, metadata)
        except Exception:
            # The callback raised — either a deliberate nack signal or an
            # unexpected error. Log with full diagnostic context (the analog of
            # the Go layer's panic recovery) and nack so the message is
            # redelivered, and eventually dead-lettered if it keeps failing.
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


def pubsub_subscriber_for_message(
    broker: SubscriberBroker,
    message_type: type[M],
    subscription_type: type[Message],
    *,
    logger: logging.Logger | None = None,
) -> Subscriber[M]:
    """Return a subscriber for ``subscription_type`` delivering ``message_type`` messages.

    Mirrors PubSubSubscriberForMessage in subscriber.go. Raises ValueError if the
    message declares no topic or the subscription marker declares no subscription.
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
