"""Gram Pub/Sub convenience layer.

A type-safe publisher/subscriber over ``google-cloud-pubsub`` that resolves
topic and subscription names from protobuf message options, mirroring the Go
layer in ``infra/pkg/gcp/``. Two brokers cover production (``PubSubBroker``) and
local development/testing against the emulator (``EmulatedPubSubBroker``).

``Publisher.publish`` returns immediately with a ``PublishResult`` future
(mirroring the Go layer); await its ``get()`` for the published message id, or
fan out many publishes and collect them afterwards::

    from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
    from gram_infra.pubsub import EmulatedPubSubBroker, pubsub_publisher_for_message
    from gram.ping.v2 import ping_pb2

    async def example() -> None:
        with EmulatedPubSubBroker(
            "my-project-id", PublisherClient(), SubscriberClient()
        ) as broker:
            publisher = pubsub_publisher_for_message(broker, ping_pb2.Message)
            await publisher.publish(ping_pb2.Message(id="1")).get()
"""

from .broker import (
    EmulatedPubSubBroker,
    PublisherBroker,
    PublisherHandle,
    PubSubBroker,
    SubscriberBroker,
    SubscriberHandle,
)
from .discover import (
    resolve_dead_letter_topic_name,
    resolve_subscription_name,
    resolve_topic_name,
    subscription_options_from_message,
    to_kebab,
    topic_options_from_message,
)
from .publisher import (
    Publisher,
    PublishResult,
    pubsub_publisher_for_message,
    pubsub_publisher_for_message_async,
)
from .subscriber import (
    MessageCallback,
    MessageMetadata,
    ReceivedMessage,
    Subscriber,
    pubsub_subscriber_for_message,
    pubsub_subscriber_for_message_async,
)

__all__ = [
    "EmulatedPubSubBroker",
    "MessageCallback",
    "MessageMetadata",
    "PubSubBroker",
    "PublishResult",
    "Publisher",
    "PublisherBroker",
    "PublisherHandle",
    "ReceivedMessage",
    "Subscriber",
    "SubscriberBroker",
    "SubscriberHandle",
    "pubsub_publisher_for_message",
    "pubsub_publisher_for_message_async",
    "pubsub_subscriber_for_message",
    "pubsub_subscriber_for_message_async",
    "resolve_dead_letter_topic_name",
    "resolve_subscription_name",
    "resolve_topic_name",
    "subscription_options_from_message",
    "to_kebab",
    "topic_options_from_message",
]
