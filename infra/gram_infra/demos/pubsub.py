"""End-to-end Pub/Sub demo, the Python counterpart of `infra demo` (demo.go).

Publishes a `gram.ping.v1.Message` every second and consumes it through the
`gram.ping.v1.Processor` subscription, printing each message received. Run it
from the infra package against the local Pub/Sub emulator:

    uv run pubsub-demo

The `EmulatedPubSubBroker` reconciles the topic, subscription, and dead-letter
topic on demand, so no Config Connector / GCP resources are needed locally.
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal
import uuid

from google.protobuf.timestamp_pb2 import Timestamp
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.ping.v1 import ping_pb2, processor_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)
from gram_infra.pubsub.publisher import Publisher
from gram_infra.pubsub.subscriber import Subscriber

logger = logging.getLogger("gram-infra-pubsub-demo")


def make_handler(receiver_id: str):
    async def handle(message: ping_pb2.Message, meta) -> None:
        logger.info(
            "%s received message id=%s type=%s payload=%s (delivery_attempt=%s)",
            receiver_id,
            message.id,
            message.type,
            message.payload.decode("utf-8", "replace"),
            meta.delivery_attempt,
        )

    return handle


async def _publish_forever(
    publisher: Publisher[ping_pb2.Message], stop_event: asyncio.Event
) -> None:
    while not stop_event.is_set():
        created_at = Timestamp()
        created_at.GetCurrentTime()
        message = ping_pb2.Message(
            id=str(uuid.uuid4()),
            type="simulated",
            created_at=created_at,
            payload=b'{"msg":"Hello, World!"}',
        )

        await asyncio.to_thread(lambda: publisher.publish(message).result(timeout=30))
        await asyncio.sleep(0)


async def _run_demo(
    publisher: Publisher[ping_pb2.Message],
    subscribers: list[tuple[str, Subscriber[ping_pb2.Message]]],
) -> None:
    loop = asyncio.get_running_loop()
    stop_event = asyncio.Event()

    def request_shutdown() -> None:
        logger.info("shutting down")
        stop_event.set()

    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, request_shutdown)

    receive_tasks = [
        asyncio.create_task(subscriber.receive(make_handler(receiver_id)))
        for receiver_id, subscriber in subscribers
    ]
    publish_task = asyncio.create_task(_publish_forever(publisher, stop_event))

    try:
        logger.info("subscribers started; publishing messages (Ctrl-C to stop)")
        await stop_event.wait()
    finally:
        stop_event.set()
        try:
            await asyncio.wait_for(publish_task, timeout=35)
        except asyncio.TimeoutError:
            publish_task.cancel()

        for task in receive_tasks:
            task.cancel()

        await asyncio.gather(publish_task, *receive_tasks, return_exceptions=True)

        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.remove_signal_handler(sig)


async def _main() -> None:
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s"
    )

    if not os.environ.get("PUBSUB_EMULATOR_HOST"):
        raise SystemExit(
            "PUBSUB_EMULATOR_HOST is not set; this demo only runs against the "
            "local emulator. Start it and re-run with e.g. "
            "PUBSUB_EMULATOR_HOST=localhost:8088 uv run pubsub-demo"
        )

    # For the emulator the project ID is arbitrary. Use an isolated default so
    # repeated demo runs don't inherit stale leased/backlogged subscription state.
    project_id = os.environ.get("GOOGLE_CLOUD_PROJECT") or (
        f"gram-infra-demo-{uuid.uuid4().hex[:8]}"
    )
    logger.info("using emulator project %s", project_id)
    with EmulatedPubSubBroker(
        project_id, PublisherClient(), SubscriberClient(), logger=logger
    ) as broker:
        # Publisher for the topic declared by gram.ping.v1.Message.
        publisher = pubsub_publisher_for_message(broker, ping_pb2.Message)

        # Subscriber for the Processor subscription, receiving Message payloads.
        # Read as: "a handle on the Processor subscription delivering Message messages."
        sub1 = pubsub_subscriber_for_message(
            broker, ping_pb2.Message, processor_pb2.Processor, logger=logger
        )

        sub2 = pubsub_subscriber_for_message(
            broker, ping_pb2.Message, processor_pb2.Processor, logger=logger
        )

        await _run_demo(publisher, [("receiver-1", sub1), ("receiver-2", sub2)])


def main() -> None:
    asyncio.run(_main())


if __name__ == "__main__":
    main()
