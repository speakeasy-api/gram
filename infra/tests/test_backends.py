"""Backend-agnostic tests: the publisher and subscriber must work under both
anyio backends, not just asyncio.

These are written as *sync* tests that drive an async scenario via
``anyio.run(..., backend=...)``, parametrized over asyncio and trio. That keeps
them independent of the pytest-asyncio auto-mode plugin the rest of the suite
uses, and proves the library does not secretly depend on asyncio internals
(e.g. ``asyncio.wrap_future`` would fail under trio).
"""

from __future__ import annotations

import concurrent.futures as cf
import threading
from datetime import timedelta
from typing import Any, cast

import anyio
import anyio.lowlevel
import pytest
import structlog
from conftest import FakeMessage, FakeSubscriberClient
from gcp.pubsub.v1 import options_pb2
from google.protobuf.duration_pb2 import Duration
from gram.ping.v2 import ping_pb2, processor_pb2

from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    Publisher,
    PublisherHandle,
    PubSubBroker,
    Subscriber,
    SubscriberHandle,
)
from gram_infra.pubsub.broker import (
    _duration_to_seconds,
    _validate_backoff,
    _validate_expiration_ttl,
    _validate_max_delivery_attempts,
    _validate_retention,
    _validate_retry_policy,
)


def test_negative_duration_rounds_toward_zero() -> None:
    """``Duration.ToTimedelta`` rounds nanos toward zero, unlike a naive
    ``nanos // 1000`` floor which biases negative values toward -inf."""
    d = Duration()
    d.FromTimedelta(timedelta(microseconds=-500))
    # microseconds component is -500 (toward zero), not -1000 (floored).
    assert d.ToTimedelta() == timedelta(microseconds=-500)


def test_duration_to_seconds_rounds_half_up_and_clamps() -> None:
    assert _duration_to_seconds(Duration(seconds=30)) == 30
    assert _duration_to_seconds(Duration(seconds=0, nanos=500_000_000)) == 1
    assert _duration_to_seconds(Duration(seconds=-5)) == 0
    assert _duration_to_seconds(Duration(seconds=2**40)) == 2**31 - 1


def test_validate_retention_bounds() -> None:
    _validate_retention(Duration(seconds=600), "topic-x")  # exactly 10 minutes: ok
    with pytest.raises(ValueError, match="message retention"):
        _validate_retention(Duration(seconds=60), "topic-x")


def test_validate_expiration_ttl_bounds() -> None:
    _validate_expiration_ttl(Duration(seconds=86400), "sub-x")  # 1 day: ok
    with pytest.raises(ValueError, match="expiration TTL"):
        _validate_expiration_ttl(Duration(seconds=3600), "sub-x")


def test_validate_backoff_bounds() -> None:
    _validate_backoff(Duration(seconds=600), "maximum backoff", "sub-x")  # cap: ok
    with pytest.raises(ValueError, match="maximum backoff"):
        _validate_backoff(Duration(seconds=601), "maximum backoff", "sub-x")


def test_validate_retry_policy_pair() -> None:
    # Each bound in range and min <= max: ok.
    _validate_retry_policy(
        options_pb2.RetryPolicy(
            minimum_backoff=Duration(seconds=5), maximum_backoff=Duration(seconds=30)
        ),
        "sub-x",
    )
    # Explicit minimum above explicit maximum.
    with pytest.raises(ValueError, match="must not exceed maximum backoff"):
        _validate_retry_policy(
            options_pb2.RetryPolicy(
                minimum_backoff=Duration(seconds=60),
                maximum_backoff=Duration(seconds=30),
            ),
            "sub-x",
        )
    # Unset maximum resolves to the 600s server default, so any in-range
    # explicit minimum is fine on its own.
    _validate_retry_policy(
        options_pb2.RetryPolicy(minimum_backoff=Duration(seconds=600)), "sub-x"
    )
    # Unset minimum resolves to the 10s server default; an explicit maximum
    # below that must fail like it would at generation time.
    with pytest.raises(ValueError, match="must not exceed maximum backoff"):
        _validate_retry_policy(
            options_pb2.RetryPolicy(maximum_backoff=Duration(seconds=5)), "sub-x"
        )


class _RecordingPublisherClient:
    def __init__(self) -> None:
        self.requests: list[dict] = []

    def topic_path(self, project: str, topic: str) -> str:
        return f"projects/{project}/topics/{topic}"

    def create_topic(self, request: dict) -> None:
        self.requests.append(request)


class _RecordingSubscriberClient:
    def __init__(self) -> None:
        self.requests: list[dict] = []

    def subscription_path(self, project: str, sub: str) -> str:
        return f"projects/{project}/subscriptions/{sub}"

    def create_subscription(self, request: dict) -> None:
        self.requests.append(request)


def _recording_broker() -> tuple[
    EmulatedPubSubBroker, _RecordingPublisherClient, _RecordingSubscriberClient
]:
    publisher = _RecordingPublisherClient()
    subscriber = _RecordingSubscriberClient()
    broker = EmulatedPubSubBroker("proj", cast(Any, publisher), cast(Any, subscriber))
    return broker, publisher, subscriber


def test_reconcile_treats_zero_durations_as_unset() -> None:
    # The Go generation path treats an explicit 0 retention/TTL as unset, so
    # the emulator broker must accept it too (and leave the field to the
    # server's default) instead of failing bounds validation.
    broker, publisher, subscriber = _recording_broker()

    broker._reconcile_topic(
        "t", options_pb2.TopicOptions(retention_hint=Duration(seconds=0))
    )
    assert "message_retention_duration" not in publisher.requests[0]

    broker._reconcile_subscription(
        "s",
        "t",
        options_pb2.SubscriptionOptions(
            retention=Duration(seconds=0), expiration_ttl=Duration(seconds=0)
        ),
    )
    request = subscriber.requests[0]
    assert "message_retention_duration" not in request
    assert "expiration_policy" not in request


def test_reconcile_rejects_negative_durations() -> None:
    # Negative durations are malformed, not unset: they must hit the bounds
    # validators rather than silently skipping the field (which would let a
    # bad declaration work locally and fail at generation time).
    broker, _, _ = _recording_broker()

    with pytest.raises(ValueError, match="message retention"):
        broker._reconcile_topic(
            "t", options_pb2.TopicOptions(retention_hint=Duration(seconds=-60))
        )

    with pytest.raises(ValueError, match="message retention"):
        broker._reconcile_subscription(
            "s", "t", options_pb2.SubscriptionOptions(retention=Duration(seconds=-60))
        )

    with pytest.raises(ValueError, match="expiration TTL"):
        broker._reconcile_subscription(
            "s",
            "t",
            options_pb2.SubscriptionOptions(expiration_ttl=Duration(seconds=-60)),
        )


def test_validate_max_delivery_attempts_bounds() -> None:
    _validate_max_delivery_attempts(5, "sub-x")
    _validate_max_delivery_attempts(100, "sub-x")
    with pytest.raises(ValueError, match="max delivery attempts"):
        _validate_max_delivery_attempts(4, "sub-x")


def test_emulated_reconcile_is_memoized() -> None:
    # Handle resolution is on the hot path (every publisher/subscriber build);
    # the emulator broker must not re-issue create RPCs for resources it already
    # reconciled in this session.
    broker, publisher, subscriber = _recording_broker()

    broker.publisher_for_message(ping_pb2.Message)
    broker.publisher_for_message(ping_pb2.Message)
    broker.subscriber_for_message(ping_pb2.Message, processor_pb2.Processor)
    broker.subscriber_for_message(ping_pb2.Message, processor_pb2.Processor)

    topic_names = [request["name"] for request in publisher.requests]
    assert len(topic_names) == len(set(topic_names))
    assert len(subscriber.requests) == 1


# Both anyio backends. trio is a dev dependency (anyio[trio]).
BACKENDS = ["asyncio", "trio"]

TOPIC_PROTO = "gram.ping.v2.Message"
SUB_PROTO = "gram.ping.v2.Processor"


class FakePublisherClient:
    """Mimics the google-cloud-pubsub publish path: returns a
    ``concurrent.futures.Future`` resolved on a background thread."""

    def __init__(self) -> None:
        self.published: list[tuple[str, bytes, dict[str, str]]] = []

    def publish(self, topic_path, data, **attributes):
        self.published.append((topic_path, data, attributes))
        future: cf.Future = cf.Future()
        threading.Thread(
            target=lambda: future.set_result("server-msg-id"), daemon=True
        ).start()
        return future


@pytest.mark.parametrize("backend", BACKENDS)
def test_publish_awaits_future_on_backend(backend) -> None:
    client = FakePublisherClient()
    publisher = Publisher(
        PublisherHandle(cast(Any, client), "topics/test"), TOPIC_PROTO
    )

    async def scenario() -> Any:
        return await publisher.publish(ping_pb2.Message(id="x", type="t")).get()

    result = anyio.run(scenario, backend=backend)

    assert result == "server-msg-id"
    assert len(client.published) == 1
    _, _, attributes = client.published[0]
    assert attributes["schema"] == TOPIC_PROTO
    assert attributes["content-type"] == "application/x-protobuf"


class ImmediatePublisherClient:
    """Returns an already-resolved future, so ``add_done_callback`` fires the
    completion callback inline on the event-loop thread rather than from a
    commit thread — exercising the publisher's loop-thread branch."""

    def publish(self, topic_path, data, **attributes):
        future: cf.Future = cf.Future()
        future.set_result("immediate-msg-id")
        return future


@pytest.mark.parametrize("backend", BACKENDS)
def test_publish_handles_already_resolved_future(backend) -> None:
    publisher = Publisher(
        PublisherHandle(cast(Any, ImmediatePublisherClient()), "topics/test"),
        TOPIC_PROTO,
    )

    async def scenario() -> Any:
        return await publisher.publish(ping_pb2.Message(id="x", type="t")).get()

    assert anyio.run(scenario, backend=backend) == "immediate-msg-id"


@pytest.mark.parametrize("backend", BACKENDS)
def test_publish_raises_publish_error(backend) -> None:
    # A failed send must surface to the awaiting caller, not vanish.
    class FailingPublisherClient:
        def publish(self, topic_path, data, **attributes):
            future: cf.Future = cf.Future()
            threading.Thread(
                target=lambda: future.set_exception(RuntimeError("send failed")),
                daemon=True,
            ).start()
            return future

    publisher = Publisher(
        PublisherHandle(cast(Any, FailingPublisherClient()), "topics/test"),
        TOPIC_PROTO,
    )

    async def scenario() -> Any:
        return await publisher.publish(ping_pb2.Message(id="x", type="t")).get()

    with pytest.raises(RuntimeError, match="send failed"):
        anyio.run(scenario, backend=backend)


@pytest.mark.parametrize("backend", BACKENDS)
def test_receive_acks_messages_on_backend(backend) -> None:
    messages = [
        FakeMessage(ping_pb2.Message(id="one").SerializeToString(), message_id="one"),
        FakeMessage(ping_pb2.Message(id="two").SerializeToString(), message_id="two"),
    ]
    client = FakeSubscriberClient(messages)
    subscriber = Subscriber(
        SubscriberHandle(cast(Any, client), "subscriptions/test"),
        ping_pb2.Message,
        logger=structlog.get_logger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )

    async def scenario() -> list[str]:
        seen: list[str] = []
        done = anyio.Event()

        async def callback(message, meta) -> None:
            seen.append(message.id)
            if len(seen) == len(messages):
                done.set()

        async with anyio.create_task_group() as tg:
            tg.start_soon(subscriber.receive, callback)
            with anyio.fail_after(5):
                await done.wait()
                # Let the in-flight handlers ack before we tear down.
                while not all(message.acked for message in messages):
                    await anyio.lowlevel.checkpoint()
            tg.cancel_scope.cancel()

        return seen

    seen = anyio.run(scenario, backend=backend)

    assert sorted(seen) == ["one", "two"]
    assert all(message.acked for message in messages)
    assert not any(message.nacked for message in messages)


class _LifecyclePublisher:
    """Context-manager publisher that records stop()/close, for broker teardown tests.

    ``stop`` is exposed only when ``with_stop`` so the "publisher has no stop()"
    branch of the broker's drain can be exercised too.
    """

    def __init__(self, events, *, with_stop=True, stop_error=False) -> None:
        self._events = events
        self._stop_error = stop_error
        if with_stop:
            self.stop = self._stop

    def _stop(self) -> None:
        if self._stop_error:
            raise RuntimeError("publisher already stopped")
        self._events.append("publisher.stop")

    def __enter__(self):
        return self

    def __exit__(self, *exc) -> None:
        self._events.append("publisher.close")
        return None


class _LifecycleSubscriber:
    def __init__(self, events) -> None:
        self._events = events

    def __enter__(self):
        return self

    def __exit__(self, *exc) -> None:
        self._events.append("subscriber.close")
        return None


def test_emulated_broker_drains_publisher_before_closing_transport() -> None:
    # The publisher's batching must be stopped/flushed before its transport is
    # closed, else an in-flight commit hits a closed channel.
    events: list[str] = []
    publisher = _LifecyclePublisher(events)
    subscriber = _LifecycleSubscriber(events)

    with EmulatedPubSubBroker("proj", cast(Any, publisher), cast(Any, subscriber)):
        pass

    assert "publisher.stop" in events
    assert events.index("publisher.stop") < events.index("publisher.close")


def test_pubsub_broker_is_a_context_manager_and_drains() -> None:
    # The production broker shares the same lifecycle: usable as a context
    # manager, draining the publisher before closing the transport.
    events: list[str] = []
    publisher = _LifecyclePublisher(events)
    subscriber = _LifecycleSubscriber(events)

    with PubSubBroker(
        "proj",
        publisher_client=cast(Any, publisher),
        subscriber_client=cast(Any, subscriber),
    ):
        pass

    assert events.index("publisher.stop") < events.index("publisher.close")
    assert "subscriber.close" in events


def test_emulated_broker_teardown_tolerates_stop_raising() -> None:
    # stop() raising (e.g. already stopped) must not prevent the transport close.
    events: list[str] = []
    publisher = _LifecyclePublisher(events, stop_error=True)

    with EmulatedPubSubBroker(
        "proj", cast(Any, publisher), cast(Any, _LifecycleSubscriber(events))
    ):
        pass

    assert "publisher.close" in events


def test_emulated_broker_teardown_tolerates_publisher_without_stop() -> None:
    # A publisher exposing no stop() (e.g. a test double) must not break teardown.
    events: list[str] = []
    publisher = _LifecyclePublisher(events, with_stop=False)

    with EmulatedPubSubBroker(
        "proj", cast(Any, publisher), cast(Any, _LifecycleSubscriber(events))
    ):
        pass

    # No stop() to call, but teardown still closes the transport cleanly.
    assert "publisher.stop" not in events
    assert "publisher.close" in events


def test_broker_cannot_be_reentered_after_exit() -> None:
    # Exiting closes the clients for good; silently re-entering would hand out
    # handles backed by dead clients whose failures only surface at use time.
    events: list[str] = []
    broker = PubSubBroker(
        "proj",
        publisher_client=cast(Any, _LifecyclePublisher(events)),
        subscriber_client=cast(Any, _LifecycleSubscriber(events)),
    )
    with broker:
        pass

    with pytest.raises(RuntimeError, match="re-entered"):
        broker.__enter__()


def test_broker_exit_waits_for_inflight_publishes() -> None:
    # Teardown must wait for publish futures still being committed so no RPC is
    # issued on a just-closed channel and no awaiting caller is stranded on a
    # never-resolved future.
    events: list[str] = []
    broker = PubSubBroker(
        "proj",
        publisher_client=cast(Any, _LifecyclePublisher(events)),
        subscriber_client=cast(Any, _LifecycleSubscriber(events)),
    )
    with broker:
        future: cf.Future = cf.Future()
        broker._inflight.add(future)
        threading.Timer(0.05, lambda: future.set_result("server-msg-id")).start()

    # The exit drained: the future resolved before the transport was closed.
    assert future.done()
    assert events.index("publisher.stop") < events.index("publisher.close")


def test_publish_registers_future_with_broker_drain() -> None:
    # Publisher.publish must register its future so the broker teardown above
    # actually has something to wait on.
    client = FakePublisherClient()
    handle = PublisherHandle(cast(Any, client), "topics/test")
    publisher = Publisher(handle, TOPIC_PROTO)

    async def scenario() -> None:
        await publisher.publish(ping_pb2.Message(id="x", type="t")).get()

    anyio.run(scenario)

    # Resolved futures discard themselves; draining returns immediately.
    assert handle.inflight._futures == set()
    handle.inflight.drain(timeout=0.1)
