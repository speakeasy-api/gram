"""End-to-end Pub/Sub demo, the Python counterpart of `infra demo` (demo.go).

Publishes a `gram.ping.v1.Message` every second and consumes it through the
`gram.ping.v1.Processor` subscription, printing each message received. Run it
from the infra package against the local Pub/Sub emulator:

    uv run pubsub-demo

The `EmulatedPubSubBroker` reconciles the topic, subscription, and dead-letter
topic on demand, so no Config Connector / GCP resources are needed locally.
"""

from __future__ import annotations

import logging
import os
import time
import uuid

from google.protobuf.timestamp_pb2 import Timestamp
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.ping.v1 import ping_pb2, processor_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    pubsub_publisher_for_message,
    pubsub_subscriber_for_message,
)

logger = logging.getLogger("gram-infra-pubsub-demo")


def main() -> None:
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s"
    )

    if not os.environ.get("PUBSUB_EMULATOR_HOST"):
        raise SystemExit(
            "PUBSUB_EMULATOR_HOST is not set; this demo only runs against the "
            "local emulator. Start it and re-run with e.g. "
            "PUBSUB_EMULATOR_HOST=localhost:8088 uv run pubsub-demo"
        )

    # For the emulator the project ID is arbitrary; mirror demo.go's default.
    project_id = os.environ.get("GOOGLE_CLOUD_PROJECT", "my-project-id")
    broker = EmulatedPubSubBroker(
        project_id, PublisherClient(), SubscriberClient(), logger=logger
    )

    # Publisher for the topic declared by gram.ping.v1.Message.
    publisher = pubsub_publisher_for_message(broker, ping_pb2.Message)

    # Subscriber for the Processor subscription, receiving Message payloads.
    # Read as: "a handle on the Processor subscription delivering Message messages."
    subscriber = pubsub_subscriber_for_message(
        broker, ping_pb2.Message, processor_pb2.Processor, logger=logger
    )

    def handle(message: ping_pb2.Message, meta) -> None:
        logger.info(
            "received message id=%s type=%s payload=%s (delivery_attempt=%s)",
            message.id,
            message.type,
            message.payload.decode("utf-8", "replace"),
            meta.delivery_attempt,
        )
        # Returning normally acks; raise to nack and trigger redelivery.

    # subscribe() is non-blocking; the client delivers messages on its own
    # threads while we publish on the main thread.
    streaming_future = subscriber.subscribe(handle)
    logger.info("subscriber started; publishing every second (Ctrl-C to stop)")

    try:
        while True:
            created_at = Timestamp()
            created_at.GetCurrentTime()
            message = ping_pb2.Message(
                id=str(uuid.uuid4()),
                type="simulated",
                created_at=created_at,
                payload=b'{"msg":"Hello, World!"}',
            )

            message_id = publisher.publish(message).result(timeout=30)
            logger.info(
                "published message id=%s (server id=%s)", message.id, message_id
            )
            time.sleep(1)
    except KeyboardInterrupt:
        logger.info("shutting down")
    finally:
        streaming_future.cancel()
        streaming_future.result()


if __name__ == "__main__":
    main()
