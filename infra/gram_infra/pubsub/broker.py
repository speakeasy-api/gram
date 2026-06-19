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

import concurrent.futures
import threading
from contextlib import ExitStack
from dataclasses import dataclass, field
from datetime import timedelta
from types import TracebackType
from typing import Protocol, Self, runtime_checkable

import structlog
from gcp.pubsub.v1 import options_pb2
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
    "EmulatedPubSubBroker",
    "PubSubBroker",
    "PublisherBroker",
    "PublisherHandle",
    "SubscriberBroker",
    "SubscriberHandle",
]


class _InflightPublishes:
    """Tracks unresolved publish futures so broker teardown can wait for them.

    ``Publisher.publish`` registers each future the client returns; a done
    callback drops it again once the commit thread resolves it. At teardown the
    broker waits on whatever is still pending, so no commit RPC is issued on a
    just-closed channel. Tracking the futures we created is precise where the
    previous approach — joining every thread in the process named like the
    library's commit threads — silently degrades to a no-op if a dependency
    bump renames them.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._futures: set[concurrent.futures.Future] = set()

    def add(self, future: concurrent.futures.Future) -> None:
        with self._lock:
            self._futures.add(future)
        # Registered after adding so an already-resolved future discards itself.
        future.add_done_callback(self._discard)

    def _discard(self, future: concurrent.futures.Future) -> None:
        with self._lock:
            self._futures.discard(future)

    def drain(self, timeout: float) -> None:
        """Block until all tracked publishes resolve, bounded by ``timeout``."""
        with self._lock:
            pending = list(self._futures)
        if pending:
            concurrent.futures.wait(pending, timeout=timeout)


@dataclass(frozen=True)
class PublisherHandle:
    """A publisher client paired with the fully-qualified topic path to publish to."""

    client: PublisherClient
    topic_path: str
    # Registry the owning broker drains at teardown; ``Publisher.publish``
    # registers every publish future here.
    inflight: _InflightPublishes = field(default_factory=_InflightPublishes)


@dataclass(frozen=True)
class SubscriberHandle:
    """A subscriber client paired with the fully-qualified subscription path to
    receive from."""

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


# Upper bound on how long teardown waits for our in-flight commits. A
# healthy commit lands in milliseconds; the bound just keeps a wedged commit
# (e.g. an unreachable server) from blocking teardown forever.
_DRAIN_JOIN_TIMEOUT_SECONDS = 10.0


class PubSubBroker:
    """Production broker. Resolves names and returns handles; assumes resources exist.

    May be used as a context manager. Entering it takes ownership of the publisher
    and subscriber clients for the duration of the ``with`` block: on exit it
    flushes and stops the publisher's batching, then closes both clients'
    transports. Use the ``with`` form when the broker owns the clients (the common
    case, including the auto-created defaults); skip it and manage lifecycle
    yourself when a client is shared with other components.
    """

    _exit_stack: ExitStack | None

    def __init__(
        self,
        project_id: str,
        *,
        publisher_client: PublisherClient | None = None,
        subscriber_client: SubscriberClient | None = None,
        logger: structlog.stdlib.BoundLogger | None = None,
    ) -> None:
        self._project_id = project_id
        self._publisher = publisher_client or PublisherClient()
        self._subscriber = subscriber_client or SubscriberClient()
        self._logger = logger or structlog.get_logger(__name__)
        self._exit_stack = None
        self._closed = False
        self._inflight = _InflightPublishes()

    def __enter__(self) -> Self:
        if self._exit_stack is not None:
            raise RuntimeError(f"{type(self).__name__} is already entered")
        if self._closed:
            # Exiting closed the clients' transports and stopped the publisher's
            # batching for good; re-entering would hand out handles backed by
            # dead clients whose failures only surface at publish/subscribe
            # time, far from the misuse. Fail loudly here instead.
            raise RuntimeError(
                f"{type(self).__name__} cannot be re-entered after exit: "
                "its clients are closed. Construct a new broker instead."
            )

        stack = ExitStack()

        try:
            stack.enter_context(self._publisher)
            stack.enter_context(self._subscriber)
            # Registered last, so it runs first on exit (LIFO) — before the
            # publisher's transport.close() — to flush and drain its batching
            # layer while the channel is still open.
            stack.callback(self._drain_publisher)
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
            self._closed = True

    def _drain_publisher(self) -> None:
        """Flush batched publishes, awaiting their commits before transport close.

        ``PublisherClient`` inherits the generated client's ``__exit__``, which
        only calls ``transport.close()`` — it never stops the batching layer. So a
        batch whose commit is still in flight (commits run on daemon background
        threads) would otherwise issue its publish RPC on the just-closed
        channel: that commit thread crashes, and because the error path does not
        resolve the publish futures, any caller still awaiting one (e.g. a
        publish cancelled with ``abandon_on_cancel``) blocks forever.

        ``stop()`` flushes outstanding batches and blocks new publishes; waiting
        on the publish futures this broker's handles registered makes those
        flushed commits land while the channel is still open, so the futures
        resolve and no RPC hits a closed channel. Publishes made directly on the
        client (bypassing :class:`~gram_infra.pubsub.Publisher`) are not
        tracked; manage the client's lifecycle yourself in that case.
        """
        stop = getattr(self._publisher, "stop", None)
        if callable(stop):
            try:
                stop()
            except RuntimeError:
                # Already stopped, or the publisher was never used — nothing to flush.
                pass

        # Bounded so a wedged commit (e.g. an unreachable server) can't block
        # teardown indefinitely; a healthy commit returns in millis.
        self._inflight.drain(timeout=_DRAIN_JOIN_TIMEOUT_SECONDS)

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
        return PublisherHandle(
            self._publisher, self._topic_path(topic_id), self._inflight
        )

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
    """Local broker. Reconciles topics/subscriptions on demand before returning handles.

    Inherits the context-manager lifecycle from :class:`PubSubBroker` (publisher
    drain + client close on exit), but requires the clients to be passed in since
    the emulator client construction is the caller's responsibility.
    """

    def __init__(
        self,
        project_id: str,
        publisher_client: PublisherClient,
        subscriber_client: SubscriberClient,
        *,
        logger: structlog.stdlib.BoundLogger | None = None,
    ) -> None:
        super().__init__(
            project_id,
            publisher_client=publisher_client,
            subscriber_client=subscriber_client,
            logger=logger,
        )
        # Resource IDs already reconciled this session, so repeated handle
        # resolution doesn't re-issue a create RPC per call. The emulator is
        # this process's private state, so nothing else deletes them behind us.
        self._reconciled_topics: set[str] = set()
        self._reconciled_subscriptions: set[str] = set()

    def _reconcile_publisher(self, topic_id, options) -> None:
        self._reconcile_topic(topic_id, options)

    def _reconcile_subscriber(self, sub_id, topic_id, binding) -> None:
        # The emulator has no Config Connector, so the topic must exist before its
        # subscription can be created; reconcile it first (mirrors pubsub_local.go).
        self._reconcile_topic(topic_id, binding.topic_options)
        self._reconcile_subscription(sub_id, topic_id, binding.subscription_options)

    def _reconcile_topic(self, topic_id: str, options=None) -> None:
        if topic_id in self._reconciled_topics:
            return
        topic_path = self._topic_path(topic_id)
        request: dict = {"name": topic_path}

        if options is not None:
            labels = dict(options.labels)
            if labels:
                request["labels"] = labels
            # Exactly-zero means unset (the server applies its default), but a
            # negative duration is malformed and must hit the validator —
            # mirroring validateRetention in pubsub_discover.go.
            if (
                options.HasField("retention_hint")
                and options.retention_hint.ToNanoseconds() != 0
            ):
                _validate_retention(options.retention_hint, topic_id)
                request["message_retention_duration"] = (
                    options.retention_hint.ToTimedelta()
                )

        try:
            self._publisher.create_topic(request=request)
            self._logger.info("topic created", topic=topic_path)
        except AlreadyExists:
            self._logger.info("topic already exists", topic=topic_path)
        self._reconciled_topics.add(topic_id)

    def _reconcile_subscription(self, sub_id: str, topic_id: str, options) -> None:
        if sub_id in self._reconciled_subscriptions:
            return
        sub_path = self._sub_path(sub_id)
        topic_path = self._topic_path(topic_id)

        request: dict = {
            "name": sub_path,
            "topic": topic_path,
            "retain_acked_messages": options.retain_acked_messages,
        }

        if options.HasField("ack_deadline"):
            request["ack_deadline_seconds"] = _duration_to_seconds(options.ack_deadline)
        # As with topics: exactly-zero retention means unset (server default),
        # which the Go generation path accepts; negative values must validate.
        if options.HasField("retention") and options.retention.ToNanoseconds() != 0:
            _validate_retention(options.retention, sub_id)
            request["message_retention_duration"] = options.retention.ToTimedelta()
        labels = dict(options.labels)
        if labels:
            request["labels"] = labels
        # Exactly-zero TTL means "never expires" (accepted by GCP and the Go
        # generation path); negative values must hit the validator.
        if (
            options.HasField("expiration_ttl")
            and options.expiration_ttl.ToNanoseconds() != 0
        ):
            _validate_expiration_ttl(options.expiration_ttl, sub_id)
            request["expiration_policy"] = {"ttl": options.expiration_ttl.ToTimedelta()}
        if options.filter:
            request["filter"] = options.filter
        if options.HasField("retry_policy"):
            _validate_retry_policy(options.retry_policy, sub_id)
            # Only forward bounds that were explicitly set; leaving an unset
            # field absent lets the server apply its default instead of pinning
            # it to 0s.
            retry_policy: dict = {}
            if options.retry_policy.HasField("minimum_backoff"):
                retry_policy["minimum_backoff"] = (
                    options.retry_policy.minimum_backoff.ToTimedelta()
                )
            if options.retry_policy.HasField("maximum_backoff"):
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
            self._logger.info("subscription created", subscription=sub_path)
        except AlreadyExists:
            self._logger.info("subscription already exists", subscription=sub_path)
        self._reconciled_subscriptions.add(sub_id)


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
# GCP's server-side defaults, applied when a bound is left unset. The pair
# check below resolves unset bounds to these before comparing, so an explicit
# minimum above 600s-default-maximum (or maximum below 10s-default-minimum)
# fails the same way it would at generation time.
_DEFAULT_MIN_BACKOFF = timedelta(seconds=10)
_DEFAULT_MAX_BACKOFF = timedelta(seconds=600)
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


def _validate_retry_policy(retry_policy: options_pb2.RetryPolicy, sub_id: str) -> None:
    """Mirror validateRetryPolicy in pubsub_discover.go: each set bound must be
    within [0s, 600s], and the resolved minimum (defaulting to 10s) must not
    exceed the resolved maximum (defaulting to 600s)."""
    minimum = _DEFAULT_MIN_BACKOFF
    if retry_policy.HasField("minimum_backoff"):
        _validate_backoff(retry_policy.minimum_backoff, "minimum backoff", sub_id)
        minimum = retry_policy.minimum_backoff.ToTimedelta()
    maximum = _DEFAULT_MAX_BACKOFF
    if retry_policy.HasField("maximum_backoff"):
        _validate_backoff(retry_policy.maximum_backoff, "maximum backoff", sub_id)
        maximum = retry_policy.maximum_backoff.ToTimedelta()
    if minimum > maximum:
        raise ValueError(
            f"retry minimum backoff {minimum} for subscription {sub_id!r} "
            f"must not exceed maximum backoff {maximum}"
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
