"""Tests for the (message, subscription) correlation guard.

require_subscription_for_message is the single place every broker routes
subscriber resolution through, so it's where the message/subscription pairing is
validated.
"""

from __future__ import annotations

import pytest
from gram.ping.v1 import ping_pb2, processor_pb2

import gram_infra.pubsub.discover as discover
from gram_infra.pubsub.discover import require_subscription_for_message


def test_binds_matching_pair() -> None:
    binding = require_subscription_for_message(
        ping_pb2.Message, processor_pb2.Processor
    )
    assert binding.message_descriptor.full_name == "gram.ping.v1.Message"
    assert binding.subscription_descriptor.full_name == "gram.ping.v1.Processor"
    assert binding.subscription_options.topic == "gram.ping.v1.Message"
    assert binding.topic_options.retention_hint.seconds == 86400


def test_mismatched_pair_raises(monkeypatch) -> None:
    # The fixture protos only define one topic message, so fake a message whose
    # full name differs from the topic the Processor subscription declares.
    class FakeDescriptor:
        full_name = "gram.ping.v1.Other"

    def fake_require_topic_options(message_type):
        assert message_type is ping_pb2.Message
        return FakeDescriptor(), object()

    monkeypatch.setattr(discover, "require_topic_options", fake_require_topic_options)

    with pytest.raises(ValueError, match=r"declares topic 'gram\.ping\.v1\.Message'"):
        require_subscription_for_message(ping_pb2.Message, processor_pb2.Processor)
