"""End-to-end publish/subscribe test against the Pub/Sub emulator.

Skipped unless PUBSUB_EMULATOR_HOST is set (and the emulator is reachable). Run
locally with the emulator from compose.yml / the local stack, e.g.::

    PUBSUB_EMULATOR_HOST=localhost:8088 uv run pytest tests/test_integration.py
"""

from __future__ import annotations

import asyncio
import os
import socket
import uuid

from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
import pytest
from gram.ping.v1 import ping_pb2, processor_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)


def _emulator_reachable() -> bool:
    """True when PUBSUB_EMULATOR_HOST is set AND actually accepting connections.

    The env var alone is not enough: mise.toml exports it to every shell in this
    repo, so it is always set in the standard dev environment even when the
    emulator container is down — and against a dead endpoint the client's
    create_topic retries gRPC UNAVAILABLE for up to 10 minutes instead of
    failing fast. A one-second TCP probe keeps the skip guard honest.
    """
    host = os.environ.get("PUBSUB_EMULATOR_HOST", "")
    hostname, sep, port = host.rpartition(":")
    if not sep:
        return False
    try:
        with socket.create_connection((hostname or "localhost", int(port)), timeout=1):
            return True
    except OSError, ValueError:
        return False


pytestmark = pytest.mark.skipif(
    not _emulator_reachable(),
    reason="Pub/Sub emulator not reachable; emulator integration test skipped",
)


async def test_publish_subscribe_roundtrip() -> None:
    broker = EmulatedPubSubBroker(
        "gram-infra-it",
        publisher_client=PublisherClient(),
        subscriber_client=SubscriberClient(),
    )

    publisher = pubsub_publisher_for_message(broker, ping_pb2.Message)
    subscriber = pubsub_subscriber_for_message(
        broker, ping_pb2.Message, processor_pb2.Processor
    )

    unique_id = uuid.uuid4().hex
    payload = b'{"msg":"hello"}'
    received: list = []
    done = asyncio.Event()

    async def callback(message: ping_pb2.Message, meta) -> None:
        # Ignore stragglers from previous runs on the shared subscription.
        if message.id != unique_id:
            return
        received.append((message, meta))
        done.set()

    receive_task = asyncio.create_task(subscriber.receive(callback))
    try:
        # Bounded: a publish whose future never resolves (e.g. a wedged
        # emulator) must fail the test rather than hang it forever.
        await asyncio.wait_for(
            publisher.publish(
                ping_pb2.Message(id=unique_id, type="it", payload=payload)
            ),
            timeout=30,
        )
        await asyncio.wait_for(done.wait(), timeout=30)
    finally:
        receive_task.cancel()
        try:
            await receive_task
        except asyncio.CancelledError:
            pass

    message, meta = received[0]
    assert message.payload == payload
    assert meta.attributes.get("content-type") == "application/x-protobuf"
    assert meta.attributes.get("schema") == "gram.ping.v1.Message"
