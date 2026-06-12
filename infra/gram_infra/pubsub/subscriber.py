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

import asyncio
import logging
import queue as queue_module
from dataclasses import dataclass, field
from collections.abc import Coroutine
from typing import Any, Awaitable, Callable, Generic, Optional, TypeVar

from google.cloud.pubsub_v1.subscriber.scheduler import Scheduler
from google.protobuf.message import DecodeError, Message

from .broker import SubscriberBroker, SubscriberHandle


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
MessageCallback = Callable[[M, MessageMetadata], Awaitable[None]]


class _AsyncIOScheduler(Scheduler):  # type: ignore[misc]
    """Pub/Sub callback scheduler backed by a running asyncio loop."""

    def __init__(self, loop: asyncio.AbstractEventLoop) -> None:
        self._loop = loop
        self._queue: queue_module.Queue[Any] = queue_module.Queue()
        self._tasks: set[asyncio.Task[None]] = set()
        self._closed = False

    @property
    def queue(self) -> queue_module.Queue[Any]:
        return self._queue

    def schedule(self, callback, *args, **kwargs) -> None:
        message = args[0] if args else None
        if self._closed:
            if message is not None:
                message.nack()
            return

        def run() -> None:
            if self._closed:
                if message is not None:
                    message.nack()
                return
            callback(*args, **kwargs)

        try:
            self._loop.call_soon_threadsafe(run)
        except RuntimeError:
            if message is not None:
                message.nack()
            return

    def create_task(
        self, coroutine: Coroutine[Any, Any, None]
    ) -> asyncio.Task[None]:
        task = self._loop.create_task(coroutine)
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)
        return task

    async def wait_for_completion(self) -> None:
        self._closed = True
        await asyncio.sleep(0)

        while self._tasks:
            await asyncio.gather(*self._tasks, return_exceptions=True)

    def shutdown(self, await_msg_callbacks: bool = True):
        self._closed = True
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
    ) -> None:
        self._handle = handle
        self._message_type = message_type
        self._logger = logger
        self._topic_proto_name = topic_proto_name
        self._subscription_proto_name = subscription_proto_name

    async def receive(
        self, callback: MessageCallback[M], *, timeout: float | None = None
    ) -> None:
        """Receive messages, blocking until cancelled or ``timeout`` elapses.

        Pub/Sub uses an asyncio-backed scheduler so message callbacks run on the
        current event loop and can create normal async tasks.
        """
        loop = asyncio.get_running_loop()
        scheduler = _AsyncIOScheduler(loop)

        def run(message) -> None:
            scheduler.create_task(self._handle_message(message, callback))

        future = self._handle.client.subscribe(
            self._handle.subscription_path,
            callback=run,
            scheduler=scheduler,
            await_callbacks_on_shutdown=True,
        )
        result_task = asyncio.create_task(asyncio.to_thread(future.result))

        try:
            await asyncio.wait_for(asyncio.shield(result_task), timeout=timeout)
        except asyncio.TimeoutError:
            future.cancel()
        except asyncio.CancelledError:
            future.cancel()
            raise
        finally:
            if not future.done():
                future.cancel()

            await asyncio.gather(result_task, return_exceptions=True)
            await scheduler.wait_for_completion()

    async def _handle_message(self, message, callback: MessageCallback[M]) -> None:
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
            await callback(instance, metadata)
        except Exception:
            # The callback raised — either a deliberate nack signal or an
            # unexpected error. Log with full diagnostic context and nack so the
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
