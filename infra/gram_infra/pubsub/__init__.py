"""Gram Pub/Sub convenience layer.

A type-safe publisher/subscriber over ``google-cloud-pubsub`` that resolves
topic and subscription names from protobuf message options, mirroring the Go
layer in ``infra/pkg/gcp/``. Two brokers cover production (``PubSubBroker``) and
local development/testing against the emulator (``EmulatedPubSubBroker``).

``Publisher.publish`` is a coroutine that returns the published message id, so
it must be awaited from inside an async function::

    from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
    from gram_infra.pubsub import EmulatedPubSubBroker, pubsub_publisher_for_message
    from gram.ping.v1 import ping_pb2

    async def example() -> None:
        with EmulatedPubSubBroker(
            "my-project-id", PublisherClient(), SubscriberClient()
        ) as broker:
            publisher = pubsub_publisher_for_message(broker, ping_pb2.Message)
            await publisher.publish(ping_pb2.Message(id="1"))
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
from .publisher import Publisher, pubsub_publisher_for_message
from .subscriber import (
    MessageCallback,
    MessageMetadata,
    Subscriber,
    pubsub_subscriber_for_message,
)

__all__ = [
    # brokers
    "PubSubBroker",
    "EmulatedPubSubBroker",
    "PublisherBroker",
    "SubscriberBroker",
    "PublisherHandle",
    "SubscriberHandle",
    # publisher
    "Publisher",
    "pubsub_publisher_for_message",
    # subscriber
    "Subscriber",
    "MessageMetadata",
    "MessageCallback",
    "pubsub_subscriber_for_message",
    # discovery / naming
    "topic_options_from_message",
    "subscription_options_from_message",
    "resolve_topic_name",
    "resolve_subscription_name",
    "resolve_dead_letter_topic_name",
    "to_kebab",
]
