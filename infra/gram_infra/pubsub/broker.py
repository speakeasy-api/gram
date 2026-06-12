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

The Pub/Sub emulator is far more lenient than GCP and does NOT enforce GCP's
accepted ranges for retention, expiration TTL, retry backoff, or dead-letter
delivery attempts. Authoritative bounds validation lives in the Go
generation path (``validate*`` in ``infra/internal/gcp/pubsub_discover.go``),
which runs when ``infra/gen/kcc.yaml`` is generated. To avoid a bad proto
declaration silently working locally only to fail far from its source during
generation or a Config Connector apply, the emulator broker mirrors those same
bounds and raises ``ValueError`` at reconcile time.
"""

from __future__ import annotations

import logging
from contextlib import ExitStack
from dataclasses import dataclass
from datetime import timedelta
from types import TracebackType
from typing import Protocol, Self, runtime_checkable

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

    def _topic_path(self, topic_id: str) -> str:
        return self._publisher.topic_path(self._project_id, topic_id)

    def _sub_path(self, sub_id: str) -> str:
        return self._subscriber.subscription_path(self._project_id, sub_id)

    def _reconcile_publisher(self, topic_id, options) -> None:
        """Hook for subclasses that must provision the topic on demand.

        No-op on the production broker, where Config Connector has already
        created the resource.
        """

    def _reconcile_subscriber(self, sub_id, topic_id, binding) -> None:
        """Hook for subclasses that must provision the subscription on demand.

        No-op on the production broker, where Config Connector has already
        created the resource.
        """

    def publisher_for_message(self, message_type: type[Message]) -> PublisherHandle:
        descriptor, options = require_topic_options(message_type)
        topic_id = resolve_topic_name(descriptor, options)
        self._reconcile_publisher(topic_id, options)
        return PublisherHandle(self._publisher, self._topic_path(topic_id))

    def subscriber_for_message(
        self, message_type: type[Message], subscription_type: type[Message]
    ) -> SubscriberHandle:
        binding = require_subscription_for_message(message_type, subscription_type)
        sub_id = resolve_subscription_name(
            binding.subscription_descriptor, binding.subscription_options
        )
        topic_id = resolve_topic_name(binding.message_descriptor, binding.topic_options)
        self._reconcile_subscriber(sub_id, topic_id, binding)
        return SubscriberHandle(self._subscriber, self._sub_path(sub_id))


class EmulatedPubSubBroker(PubSubBroker):
    """Local broker. Reconciles topics/subscriptions on demand before returning handles."""

    _exit_stack: ExitStack | None

    def __init__(
        self,
        project_id: str,
        publisher_client: PublisherClient,
        subscriber_client: SubscriberClient,
        *,
        logger: logging.Logger | None = None,
    ) -> None:
        super().__init__(
            project_id,
            publisher_client=publisher_client,
            subscriber_client=subscriber_client,
            logger=logger,
        )
        self._exit_stack = None

    def __enter__(self) -> Self:
        if self._exit_stack is not None:
            raise RuntimeError("EmulatedPubSubBroker is already entered")

        stack = ExitStack()

        try:
            stack.enter_context(self._publisher)
            stack.enter_context(self._subscriber)
        except BaseException as exc:
            stack.__exit__(type(exc), exc, exc.__traceback__)
            raise

        self._exit_stack = stack
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> bool | None:
        if self._exit_stack is None:
            return None

        try:
            # Discard the ExitStack's return value: a context manager it wraps
            # could in principle return truthy from __exit__ and suppress an
            # exception raised in the ``with`` body that the stack did not
            # originate. Returning None keeps such exceptions propagating while
            # still letting the stack run its cleanup above.
            self._exit_stack.__exit__(exc_type, exc_val, exc_tb)
            return None
        finally:
            self._exit_stack = None

    def _reconcile_publisher(self, topic_id, options) -> None:
        self._reconcile_topic(topic_id, options)

    def _reconcile_subscriber(self, sub_id, topic_id, binding) -> None:
        # The emulator has no Config Connector, so the topic must exist before its
        # subscription can be created; reconcile it first (mirrors pubsub_local.go).
        self._reconcile_topic(topic_id, binding.topic_options)
        self._reconcile_subscription(sub_id, topic_id, binding.subscription_options)

    def _reconcile_topic(self, topic_id: str, options=None) -> None:
        topic_path = self._topic_path(topic_id)
        request: dict = {"name": topic_path}

        if options is not None:
            labels = dict(options.labels)
            if labels:
                request["labels"] = labels
            if (
                options.HasField("retention_hint")
                and options.retention_hint.ToNanoseconds() > 0
            ):
                _validate_retention(options.retention_hint, topic_id)
                request["message_retention_duration"] = (
                    options.retention_hint.ToTimedelta()
                )

        try:
            self._publisher.create_topic(request=request)
            self._logger.info("topic created", extra={"topic": topic_path})
        except AlreadyExists:
            self._logger.info("topic already exists", extra={"topic": topic_path})

    def _reconcile_subscription(self, sub_id: str, topic_id: str, options) -> None:
        sub_path = self._sub_path(sub_id)
        topic_path = self._topic_path(topic_id)

        request: dict = {
            "name": sub_path,
            "topic": topic_path,
            "retain_acked_messages": options.retain_acked_messages,
        }

        if options.HasField("ack_deadline"):
            request["ack_deadline_seconds"] = _duration_to_seconds(options.ack_deadline)
        if options.HasField("retention"):
            _validate_retention(options.retention, sub_id)
            request["message_retention_duration"] = options.retention.ToTimedelta()
        labels = dict(options.labels)
        if labels:
            request["labels"] = labels
        if (
            options.HasField("expiration_ttl")
            and options.expiration_ttl.ToNanoseconds() > 0
        ):
            _validate_expiration_ttl(options.expiration_ttl, sub_id)
            request["expiration_policy"] = {
                "ttl": options.expiration_ttl.ToTimedelta()
            }
        if options.filter:
            request["filter"] = options.filter
        if options.HasField("retry_policy"):
            # Only forward bounds that were explicitly set; leaving an unset
            # field absent lets the server apply its default instead of pinning
            # it to 0s.
            retry_policy: dict = {}
            if options.retry_policy.HasField("minimum_backoff"):
                _validate_backoff(
                    options.retry_policy.minimum_backoff, "minimum backoff", sub_id
                )
                retry_policy["minimum_backoff"] = (
                    options.retry_policy.minimum_backoff.ToTimedelta()
                )
            if options.retry_policy.HasField("maximum_backoff"):
                _validate_backoff(
                    options.retry_policy.maximum_backoff, "maximum backoff", sub_id
                )
                retry_policy["maximum_backoff"] = (
                    options.retry_policy.maximum_backoff.ToTimedelta()
                )
            request["retry_policy"] = retry_policy
        if options.HasField("dead_letter"):
            dead_letter = options.dead_letter
            _validate_max_delivery_attempts(dead_letter.max_delivery_attempts, sub_id)
            dlq_id = resolve_dead_letter_topic_name(sub_id, dead_letter)
            # As with the source topic, the auto-derived DLQ topic must exist
            # before the subscription can reference it.
            self._reconcile_topic(dlq_id, None)
            request["dead_letter_policy"] = {
                "dead_letter_topic": self._topic_path(dlq_id),
                "max_delivery_attempts": dead_letter.max_delivery_attempts,
            }

        try:
            self._subscriber.create_subscription(request=request)
            self._logger.info("subscription created", extra={"subscription": sub_path})
        except AlreadyExists:
            self._logger.info(
                "subscription already exists", extra={"subscription": sub_path}
            )


_MAX_INT32 = 2**31 - 1

# GCP's accepted ranges, mirrored from the Go generation-time validators in
# infra/internal/gcp/pubsub_discover.go. The emulator itself does not enforce
# these, so we check them here to surface bad proto declarations at reconcile
# time rather than letting them slip through to kcc.yaml generation or a
# Config Connector apply against real GCP.
_MIN_RETENTION = timedelta(minutes=10)
_MAX_RETENTION = timedelta(days=31)
_MIN_EXPIRATION_TTL = timedelta(days=1)
_MAX_EXPIRATION_TTL = timedelta(days=31)
_MAX_BACKOFF = timedelta(seconds=600)
_MIN_DELIVERY_ATTEMPTS = 5
_MAX_DELIVERY_ATTEMPTS = 100


def _validate_retention(duration: Duration, resource_id: str) -> None:
    value = duration.ToTimedelta()
    if value < _MIN_RETENTION or value > _MAX_RETENTION:
        raise ValueError(
            f"message retention for {resource_id!r} must be between "
            f"{_MIN_RETENTION} and {_MAX_RETENTION}, got {value}"
        )


def _validate_expiration_ttl(duration: Duration, sub_id: str) -> None:
    value = duration.ToTimedelta()
    if value < _MIN_EXPIRATION_TTL or value > _MAX_EXPIRATION_TTL:
        raise ValueError(
            f"expiration TTL for subscription {sub_id!r} must be between "
            f"{_MIN_EXPIRATION_TTL} and {_MAX_EXPIRATION_TTL}, got {value}"
        )


def _validate_backoff(duration: Duration, label: str, sub_id: str) -> None:
    value = duration.ToTimedelta()
    if value < timedelta(0) or value > _MAX_BACKOFF:
        raise ValueError(
            f"retry {label} for subscription {sub_id!r} must be between "
            f"0s and {_MAX_BACKOFF}, got {value}"
        )


def _validate_max_delivery_attempts(attempts: int, sub_id: str) -> None:
    if attempts < _MIN_DELIVERY_ATTEMPTS or attempts > _MAX_DELIVERY_ATTEMPTS:
        raise ValueError(
            f"max delivery attempts for subscription {sub_id!r} must be between "
            f"{_MIN_DELIVERY_ATTEMPTS} and {_MAX_DELIVERY_ATTEMPTS}, got {attempts}"
        )


def _duration_to_seconds(duration: Duration) -> int:
    """Round a Duration to whole seconds (half up), clamping to the int32 range.

    Mirrors durationToSeconds in pubsub_local.go for ack-deadline conversion:
    non-positive durations clamp to 0, and large ones clamp to int32 max since
    ``ack_deadline_seconds`` is an int32 field.
    """
    total = duration.ToNanoseconds() / 1_000_000_000
    if total <= 0:
        return 0
    # Clamp before rounding so the half-up addition can't push us past int32.
    if total >= _MAX_INT32 - 0.5:
        return _MAX_INT32
    return int(total + 0.5)
