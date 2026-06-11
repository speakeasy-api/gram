"""Pub/Sub brokers that resolve topic/subscription handles from proto options.

Python counterpart of ``infra/pkg/gcp/pubsub_gcp.go`` (production) and
``infra/pkg/gcp/pubsub_local.go`` (local emulator). A broker takes a proto
message *type*, reads its ``(gcp.pubsub.v1.topic)`` / ``(gcp.pubsub.v1.subscription)``
option, resolves the resource name, and returns a lightweight handle bound to a
``google-cloud-pubsub`` client. The publisher/subscriber wrappers in
``publisher.py`` / ``subscriber.py`` consume those handles.

``PubSubBroker`` assumes resources already exist (provisioned by Config
Connector from ``infra/gen/kcc.yaml``). ``EmulatedPubSubBroker`` reconciles them
on demand because the emulator has no Config Connector — matching the Go split.
The clients auto-detect the emulator via the ``PUBSUB_EMULATOR_HOST`` env var.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from datetime import timedelta
from typing import Protocol, runtime_checkable

from google.api_core.exceptions import AlreadyExists
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from google.protobuf.duration_pb2 import Duration
from google.protobuf.message import Message

from .discover import (
    require_subscription_for_message,
    require_topic_options,
    resolve_dead_letter_topic_name,
    resolve_subscription_name,
    resolve_topic_name,
)

__all__ = [
    "PublisherHandle",
    "SubscriberHandle",
    "PublisherBroker",
    "SubscriberBroker",
    "PubSubBroker",
    "EmulatedPubSubBroker",
]


@dataclass(frozen=True)
class PublisherHandle:
    """A publisher client paired with the fully-qualified topic path to publish to."""

    client: PublisherClient
    topic_path: str


@dataclass(frozen=True)
class SubscriberHandle:
    """A subscriber client paired with the fully-qualified subscription path to receive from."""

    client: SubscriberClient
    subscription_path: str


@runtime_checkable
class PublisherBroker(Protocol):
    def publisher_for_message(self, message_type: type[Message]) -> PublisherHandle: ...


@runtime_checkable
class SubscriberBroker(Protocol):
    def subscriber_for_message(
        self, message_type: type[Message], subscription_type: type[Message]
    ) -> SubscriberHandle: ...


class PubSubBroker:
    """Production broker. Resolves names and returns handles; assumes resources exist."""

    def __init__(
        self,
        project_id: str,
        *,
        publisher_client: PublisherClient | None = None,
        subscriber_client: SubscriberClient | None = None,
        logger: logging.Logger | None = None,
    ) -> None:
        self._project_id = project_id
        self._publisher = publisher_client or PublisherClient()
        self._subscriber = subscriber_client or SubscriberClient()
        self._logger = logger or logging.getLogger(__name__)

    def publisher_for_message(self, message_type: type[Message]) -> PublisherHandle:
        descriptor, options = require_topic_options(message_type)
        topic_id = resolve_topic_name(descriptor, options)
        return PublisherHandle(
            self._publisher, self._publisher.topic_path(self._project_id, topic_id)
        )

    def subscriber_for_message(
        self, message_type: type[Message], subscription_type: type[Message]
    ) -> SubscriberHandle:
        binding = require_subscription_for_message(message_type, subscription_type)
        sub_id = resolve_subscription_name(
            binding.subscription_descriptor, binding.subscription_options
        )
        return SubscriberHandle(
            self._subscriber,
            self._subscriber.subscription_path(self._project_id, sub_id),
        )


class EmulatedPubSubBroker:
    """Local broker. Reconciles topics/subscriptions on demand before returning handles."""

    def __init__(
        self,
        project_id: str,
        publisher_client: PublisherClient,
        subscriber_client: SubscriberClient,
        *,
        logger: logging.Logger | None = None,
    ) -> None:
        self._project_id = project_id
        self._publisher = publisher_client
        self._subscriber = subscriber_client
        self._logger = logger or logging.getLogger(__name__)

    def publisher_for_message(self, message_type: type[Message]) -> PublisherHandle:
        descriptor, options = require_topic_options(message_type)
        topic_id = resolve_topic_name(descriptor, options)
        self._reconcile_topic(topic_id, options)
        return PublisherHandle(
            self._publisher, self._publisher.topic_path(self._project_id, topic_id)
        )

    def subscriber_for_message(
        self, message_type: type[Message], subscription_type: type[Message]
    ) -> SubscriberHandle:
        binding = require_subscription_for_message(message_type, subscription_type)

        sub_id = resolve_subscription_name(
            binding.subscription_descriptor, binding.subscription_options
        )
        topic_id = resolve_topic_name(binding.message_descriptor, binding.topic_options)

        # The emulator has no Config Connector, so the topic must exist before its
        # subscription can be created; reconcile it first (mirrors pubsub_local.go).
        self._reconcile_topic(topic_id, binding.topic_options)
        self._reconcile_subscription(sub_id, topic_id, binding.subscription_options)

        return SubscriberHandle(
            self._subscriber,
            self._subscriber.subscription_path(self._project_id, sub_id),
        )

    def _reconcile_topic(self, topic_id: str, options=None) -> None:
        topic_path = self._publisher.topic_path(self._project_id, topic_id)
        request: dict = {"name": topic_path}

        if options is not None:
            labels = dict(options.labels)
            if labels:
                request["labels"] = labels
            if options.HasField("retention_hint") and _is_positive(
                options.retention_hint
            ):
                request["message_retention_duration"] = _to_timedelta(
                    options.retention_hint
                )

        try:
            self._publisher.create_topic(request=request)
            self._logger.info("topic created", extra={"topic": topic_path})
        except AlreadyExists:
            self._logger.info("topic already exists", extra={"topic": topic_path})

    def _reconcile_subscription(self, sub_id: str, topic_id: str, options) -> None:
        sub_path = self._subscriber.subscription_path(self._project_id, sub_id)
        topic_path = self._publisher.topic_path(self._project_id, topic_id)

        request: dict = {
            "name": sub_path,
            "topic": topic_path,
            "retain_acked_messages": options.retain_acked_messages,
        }

        if options.HasField("ack_deadline"):
            request["ack_deadline_seconds"] = _duration_to_seconds(options.ack_deadline)
        if options.HasField("retention"):
            request["message_retention_duration"] = _to_timedelta(options.retention)
        labels = dict(options.labels)
        if labels:
            request["labels"] = labels
        if options.HasField("expiration_ttl") and _is_positive(options.expiration_ttl):
            request["expiration_policy"] = {
                "ttl": _to_timedelta(options.expiration_ttl)
            }
        if options.filter:
            request["filter"] = options.filter
        if options.HasField("retry_policy"):
            # Only forward bounds that were explicitly set; leaving an unset
            # field absent lets the server apply its default instead of pinning
            # it to 0s.
            retry_policy: dict = {}
            if options.retry_policy.HasField("minimum_backoff"):
                retry_policy["minimum_backoff"] = _to_timedelta(
                    options.retry_policy.minimum_backoff
                )
            if options.retry_policy.HasField("maximum_backoff"):
                retry_policy["maximum_backoff"] = _to_timedelta(
                    options.retry_policy.maximum_backoff
                )
            request["retry_policy"] = retry_policy
        if options.HasField("dead_letter"):
            dead_letter = options.dead_letter
            dlq_id = resolve_dead_letter_topic_name(sub_id, dead_letter)
            # As with the source topic, the auto-derived DLQ topic must exist
            # before the subscription can reference it.
            self._reconcile_topic(dlq_id, None)
            request["dead_letter_policy"] = {
                "dead_letter_topic": self._publisher.topic_path(
                    self._project_id, dlq_id
                ),
                "max_delivery_attempts": dead_letter.max_delivery_attempts,
            }

        try:
            self._subscriber.create_subscription(request=request)
            self._logger.info("subscription created", extra={"subscription": sub_path})
        except AlreadyExists:
            self._logger.info(
                "subscription already exists", extra={"subscription": sub_path}
            )


def _is_positive(duration: Duration) -> bool:
    return duration.seconds > 0 or duration.nanos > 0


def _to_timedelta(duration: Duration) -> timedelta:
    return timedelta(seconds=duration.seconds, microseconds=duration.nanos // 1000)


_MAX_INT32 = 2**31 - 1


def _duration_to_seconds(duration: Duration) -> int:
    """Round a Duration to whole seconds (half up), clamping to the int32 range.

    Mirrors durationToSeconds in pubsub_local.go for ack-deadline conversion:
    non-positive durations clamp to 0, and large ones clamp to int32 max since
    ``ack_deadline_seconds`` is an int32 field.
    """
    total = duration.seconds + duration.nanos / 1_000_000_000
    if total <= 0:
        return 0
    # Clamp before rounding so the half-up addition can't push us past int32.
    if total >= _MAX_INT32 - 0.5:
        return _MAX_INT32
    return int(total + 0.5)
