"""End-to-end Pub/Sub demo, the Python counterpart of `infra demo` (demo.go).

Publishes a `gram.ping.v1.Message` every second and consumes it through the
`gram.ping.v1.Processor` subscription, printing each message received. Run it
from the infra package against the local Pub/Sub emulator:

    uv run pubsub-demo

Runs on the asyncio backend by default; set ``PUBSUB_DEMO_BACKEND=trio`` to run
the same demo under trio, demonstrating the library is backend-agnostic:

    PUBSUB_DEMO_BACKEND=trio uv run pubsub-demo

The `EmulatedPubSubBroker` reconciles the topic, subscription, and dead-letter
topic on demand, so no Config Connector / GCP resources are needed locally.
"""

from __future__ import annotations

import logging
import os
import signal
import uuid

import anyio
import sniffio
from google.protobuf.timestamp_pb2 import Timestamp
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.ping.v1 import ping_pb2, processor_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)
from gram_infra.pubsub.publisher import Publisher

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


async def _publish_forever(publisher: Publisher[ping_pb2.Message]) -> None:
    while True:
        created_at = Timestamp()
        created_at.GetCurrentTime()
        message = ping_pb2.Message(
            id=str(uuid.uuid4()),
            type="simulated",
            created_at=created_at,
            payload=b'{"msg":"Hello, World!"}',
        )

        await publisher.publish(message)
        await anyio.sleep(0.2)


async def _shutdown_on_signal(cancel_scope: anyio.CancelScope) -> None:
    """Cancel the surrounding task group when SIGINT/SIGTERM arrives."""
    with anyio.open_signal_receiver(signal.SIGINT, signal.SIGTERM) as signals:
        async for _ in signals:
            logger.info("shutting down")
            cancel_scope.cancel()
            return


async def _main() -> None:
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s"
    )
    logger.info("running on %s backend", sniffio.current_async_library())

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

        logger.info("subscribers started; publishing messages (Ctrl-C to stop)")
        async with anyio.create_task_group() as tg:
            tg.start_soon(_shutdown_on_signal, tg.cancel_scope)
            tg.start_soon(_publish_forever, publisher)
            tg.start_soon(sub1.receive, make_handler("receiver-1"))
            tg.start_soon(sub2.receive, make_handler("receiver-2"))


def main() -> None:
    # The library is backend-agnostic; flip PUBSUB_DEMO_BACKEND=trio to run the
    # same demo under trio instead of asyncio.
    backend = os.environ.get("PUBSUB_DEMO_BACKEND", "asyncio")
    if backend not in ("asyncio", "trio"):
        raise SystemExit(
            f"PUBSUB_DEMO_BACKEND must be 'asyncio' or 'trio', got {backend!r}"
        )
    anyio.run(_main, backend=backend)


if __name__ == "__main__":
    main()
