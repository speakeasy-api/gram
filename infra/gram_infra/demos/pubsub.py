"""End-to-end Pub/Sub demo, the Python counterpart of `infra demo` (demo.go).

Publishes a `gram.ping.v2.Message` every ~200ms and consumes it through the
`gram.ping.v2.Processor` subscription, printing each message received. Run it
from the infra package against the local Pub/Sub emulator:

    uv run pubsub-demo

Runs on the asyncio backend by default; set ``PUBSUB_DEMO_BACKEND=trio`` to run
the same demo under trio, demonstrating the library is backend-agnostic:

    PUBSUB_DEMO_BACKEND=trio uv run pubsub-demo

The `EmulatedPubSubBroker` reconciles the topic, subscription, and dead-letter
topic on demand, so no Config Connector / GCP resources are needed locally.
"""

from __future__ import annotations

import anyio
import structlog
from gram.ping.v2 import ping_pb2, processor_pb2

from gram_infra.pubsub import (
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)

from ._common import make_emulator_broker, publish_forever, run_demo, shutdown_on_signal

logger = structlog.get_logger("gram-infra-pubsub-demo")


def make_handler(receiver_id: str):
    async def handle(message: ping_pb2.Message, meta) -> None:
        logger.info(
            "received message",
            receiver_id=receiver_id,
            id=message.id,
            type=message.type,
            payload=message.payload.decode("utf-8", "replace"),
            delivery_attempt=meta.delivery_attempt,
        )

    return handle


async def _main(backend: str) -> None:
    logger.info("running", backend=backend)

    with make_emulator_broker(logger, script_name="pubsub-demo") as broker:
        # Publisher for the topic declared by gram.ping.v2.Message.
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
            tg.start_soon(shutdown_on_signal, tg.cancel_scope, logger)
            tg.start_soon(publish_forever, publisher)
            tg.start_soon(sub1.receive, make_handler("receiver-1"))
            tg.start_soon(sub2.receive, make_handler("receiver-2"))


def main() -> None:
    run_demo(_main)


if __name__ == "__main__":
    main()
