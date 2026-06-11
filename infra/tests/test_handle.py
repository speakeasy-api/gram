"""Subscriber message-handling tests (no live broker).

Ported from infra/pkg/gcp/subscriber_test.go. Exercises the ack/nack and logging
behavior of Subscriber._handle_message against a fake message, so the core
delivery logic is covered without an emulator.
"""

from __future__ import annotations

import logging
from typing import cast

from gram.ping.v1 import ping_pb2
from gram_infra.pubsub import MessageMetadata, Subscriber, SubscriberHandle

TOPIC_PROTO = "gram.ping.v1.Message"
SUB_PROTO = "gram.ping.v1.Processor"


class FakeMessage:
    """Duck-typed stand-in for a google-cloud-pubsub Message."""

    def __init__(
        self, data, *, message_id="msg-id", attributes=None, delivery_attempt=None
    ):
        self.data = data
        self.message_id = message_id
        self.attributes = attributes or {}
        self.delivery_attempt = delivery_attempt
        self.acked = False
        self.nacked = False

    def ack(self):
        self.acked = True

    def nack(self):
        self.nacked = True


def make_subscriber(logger=None) -> Subscriber:
    # _handle_message never touches the handle (only subscribe() does), so a
    # typed-None placeholder is sufficient for these unit tests.
    return Subscriber(
        cast(SubscriberHandle, None),
        ping_pb2.Message,
        logger=logger or logging.getLogger("gram_infra.test"),
        topic_proto_name=TOPIC_PROTO,
        subscription_proto_name=SUB_PROTO,
    )


def test_success_is_acked_only() -> None:
    data = ping_pb2.Message(id="abc", type="t").SerializeToString()
    message = FakeMessage(
        data, message_id="msg-ok", attributes={"content-type": "application/x-protobuf"}
    )

    seen: dict = {}

    def callback(msg, meta: MessageMetadata) -> None:
        seen["msg"] = msg
        seen["meta"] = meta

    make_subscriber()._handle_message(message, callback)

    assert message.acked is True
    assert message.nacked is False
    assert seen["msg"].id == "abc"
    assert seen["meta"].id == "msg-ok"
    assert seen["meta"].attributes == {"content-type": "application/x-protobuf"}


def test_unmarshal_error_is_nacked_and_skips_callback() -> None:
    # Field 1 (string) declares a 5-byte length but supplies 2 bytes -> truncated.
    message = FakeMessage(b"\x0a\x05ab", message_id="msg-bad")

    class Receiver:
        called = False

        def callback(self, msg, meta) -> None:
            self.called = True

    receiver = Receiver()
    make_subscriber()._handle_message(message, receiver.callback)

    assert message.nacked is True
    assert message.acked is False
    assert receiver.called is False


def test_callback_exception_is_logged_and_nacked(caplog) -> None:
    data = ping_pb2.Message(id="x").SerializeToString()
    message = FakeMessage(data, message_id="msg-123", delivery_attempt=3)

    def callback(msg, meta) -> None:
        raise RuntimeError("boom")

    with caplog.at_level(logging.ERROR, logger="gram_infra.test"):
        make_subscriber()._handle_message(message, callback)

    assert message.nacked is True
    assert message.acked is False

    record = next(
        r for r in caplog.records if r.msg == "error processing pubsub message"
    )
    assert "boom" in record.exc_text
    assert record.topic_proto_name == TOPIC_PROTO
    assert record.subscription_proto_name == SUB_PROTO
    assert record.message_id == "msg-123"
    assert record.delivery_attempt == 3


def test_nil_delivery_attempt_logs_zero(caplog) -> None:
    data = ping_pb2.Message(id="x").SerializeToString()
    message = FakeMessage(data, message_id="msg-nil", delivery_attempt=None)

    def callback(msg, meta) -> None:
        raise RuntimeError("kaboom")

    with caplog.at_level(logging.ERROR, logger="gram_infra.test"):
        make_subscriber()._handle_message(message, callback)

    assert message.nacked is True
    record = next(
        r for r in caplog.records if r.msg == "error processing pubsub message"
    )
    assert record.delivery_attempt == 0
