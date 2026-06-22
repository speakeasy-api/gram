"""Option-extraction tests: reading topic/subscription options off proto descriptors."""

from __future__ import annotations

from gram.ping.v1 import ping_pb2, processor_pb2

from gram_infra.pubsub import (
    subscription_options_from_message,
    topic_options_from_message,
)


def test_topic_options_present_on_message() -> None:
    options = topic_options_from_message(ping_pb2.Message.DESCRIPTOR)
    assert options is not None
    assert options.retention_hint.seconds == 86400


def test_topic_options_absent_on_subscription_marker() -> None:
    assert topic_options_from_message(processor_pb2.Processor.DESCRIPTOR) is None


def test_subscription_options_present_on_marker() -> None:
    options = subscription_options_from_message(processor_pb2.Processor.DESCRIPTOR)
    assert options is not None
    assert options.topic == "gram.ping.v1.Message"
    assert options.ack_deadline.seconds == 30
    assert options.retention.seconds == 3600
    assert options.retain_acked_messages is True
    assert options.retry_policy.minimum_backoff.seconds == 10
    assert options.retry_policy.maximum_backoff.seconds == 600
    assert options.dead_letter.max_delivery_attempts == 5


def test_subscription_options_absent_on_topic_message() -> None:
    assert subscription_options_from_message(ping_pb2.Message.DESCRIPTOR) is None
