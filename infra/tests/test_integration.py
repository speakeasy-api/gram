"""End-to-end publish/subscribe test against the Pub/Sub emulator.

Skipped unless PUBSUB_EMULATOR_HOST is set (and the emulator is reachable). Run
locally with the emulator from compose.yml / the local stack, e.g.::

    PUBSUB_EMULATOR_HOST=localhost:8088 uv run pytest tests/test_integration.py
"""

from __future__ import annotations

import asyncio
import os
import uuid

from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
import pytest
from gram.ping.v1 import ping_pb2, processor_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)

pytestmark = pytest.mark.skipif(
    not os.environ.get("PUBSUB_EMULATOR_HOST"),
    reason="PUBSUB_EMULATOR_HOST not set; emulator integration test skipped",
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
        await publisher.publish(
            ping_pb2.Message(id=unique_id, type="it", payload=payload)
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
