"""End-to-end Pub/Sub demo using the async-iterator (`stream`) API.

Sibling of `pubsub.py`: that demo consumes with the callback form
(`subscriber.receive`); this one consumes a single `subscriber.stream()` with an
`async for` loop and acks each message explicitly. Run it from the infra package
against the local Pub/Sub emulator:

    uv run pubsub-stream-demo

Runs on the asyncio backend by default; set ``PUBSUB_DEMO_BACKEND=trio`` to run
the same demo under trio, demonstrating the library is backend-agnostic:

    PUBSUB_DEMO_BACKEND=trio uv run pubsub-stream-demo

The `EmulatedPubSubBroker` reconciles the topic, subscription, and dead-letter
topic on demand, so no Config Connector / GCP resources are needed locally.
"""

from __future__ import annotations

import anyio
import structlog
from gram.ping.v2 import ping_pb2, processor_pb2

from gram_infra.pubsub import (
    Subscriber,
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)

from ._common import make_emulator_broker, publish_forever, run_demo, shutdown_on_signal

logger = structlog.get_logger("gram-infra-pubsub-stream-demo")


async def _consume_stream(
    subscriber: Subscriber[ping_pb2.Message], receiver_id: str
) -> None:
    """Drain the subscription with a single `async for`, acking each message.

    Unlike the callback demo (where returning acks and raising nacks), the stream
    form hands disposition to us: we call ``received.ack()`` after logging. We
    could call ``received.nack()`` instead to force redelivery / dead-lettering.
    """
    async with subscriber.stream() as messages:
        async for received in messages:
            logger.info(
                "received message",
                receiver_id=receiver_id,
                id=received.message.id,
                type=received.message.type,
                payload=received.message.payload.decode("utf-8", "replace"),
                delivery_attempt=received.metadata.delivery_attempt,
            )
            received.ack()


async def _main(backend: str) -> None:
    logger.info("running", backend=backend)

    with make_emulator_broker(logger, script_name="pubsub-stream-demo") as broker:
        # Publisher for the topic declared by gram.ping.v2.Message.
        publisher = pubsub_publisher_for_message(broker, ping_pb2.Message)

        # Two subscribers on the Processor subscription, each draining its own
        # stream concurrently. They are competing consumers on one subscription,
        # so each published message is delivered to exactly one of the two
        # receivers.
        sub1 = pubsub_subscriber_for_message(
            broker, ping_pb2.Message, processor_pb2.Processor, logger=logger
        )
        sub2 = pubsub_subscriber_for_message(
            broker, ping_pb2.Message, processor_pb2.Processor, logger=logger
        )

        logger.info(
            "streaming subscribers started; publishing messages (Ctrl-C to stop)"
        )
        async with anyio.create_task_group() as tg:
            tg.start_soon(shutdown_on_signal, tg.cancel_scope, logger)
            tg.start_soon(publish_forever, publisher)
            tg.start_soon(_consume_stream, sub1, "receiver-1")
            tg.start_soon(_consume_stream, sub2, "receiver-2")


def main() -> None:
    run_demo(_main)


if __name__ == "__main__":
    main()
