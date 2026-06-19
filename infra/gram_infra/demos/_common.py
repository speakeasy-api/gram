"""Shared scaffolding for the Pub/Sub demos.

Both demos publish ``gram.ping.v1.Message`` payloads against the local Pub/Sub
emulator and differ only in how they consume them (callback vs async-iterator),
so the publish loop, signal handling, broker construction, and backend selection
live here.
"""

from __future__ import annotations

import logging
import os
import signal
import uuid
from typing import Awaitable, Callable

import anyio
import structlog
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from google.protobuf.timestamp_pb2 import Timestamp

from gram.ping.v1 import ping_pb2
from gram_infra.pubsub import EmulatedPubSubBroker
from gram_infra.pubsub.publisher import Publisher

# Cadence of the demo publish loop.
PUBLISH_INTERVAL_SECONDS = 0.2


def configure_logging() -> None:
    """Configure structlog for the demos: leveled, timestamped console output."""
    structlog.configure(
        processors=[
            structlog.processors.add_log_level,
            structlog.processors.TimeStamper(fmt="iso", utc=True),
            structlog.dev.ConsoleRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(logging.INFO),
    )


def make_emulator_broker(
    logger: structlog.stdlib.BoundLogger, *, script_name: str
) -> EmulatedPubSubBroker:
    """Build a broker against the local emulator, or exit with guidance."""
    if not os.environ.get("PUBSUB_EMULATOR_HOST"):
        raise SystemExit(
            "PUBSUB_EMULATOR_HOST is not set; this demo only runs against the "
            "local emulator. Start it and re-run with e.g. "
            f"PUBSUB_EMULATOR_HOST=localhost:8088 uv run {script_name}"
        )

    # For the emulator the project ID is arbitrary. Use an isolated default so
    # repeated demo runs don't inherit stale leased/backlogged subscription state.
    project_id = os.environ.get("GOOGLE_CLOUD_PROJECT") or (
        f"gram-infra-demo-{uuid.uuid4().hex[:8]}"
    )
    logger.info("using emulator project", project=project_id)
    return EmulatedPubSubBroker(
        project_id, PublisherClient(), SubscriberClient(), logger=logger
    )


async def publish_forever(publisher: Publisher[ping_pb2.Message]) -> None:
    """Publish a fresh ping message every :data:`PUBLISH_INTERVAL_SECONDS`."""
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
        await anyio.sleep(PUBLISH_INTERVAL_SECONDS)


async def shutdown_on_signal(
    cancel_scope: anyio.CancelScope, logger: structlog.stdlib.BoundLogger
) -> None:
    """Cancel the surrounding task group when SIGINT/SIGTERM arrives."""
    with anyio.open_signal_receiver(signal.SIGINT, signal.SIGTERM) as signals:
        async for _ in signals:
            logger.info("shutting down")
            cancel_scope.cancel()
            return


def run_demo(demo: Callable[[str], Awaitable[None]]) -> None:
    """Run a demo coroutine on the backend selected by ``PUBSUB_DEMO_BACKEND``.

    The library is backend-agnostic; flip ``PUBSUB_DEMO_BACKEND=trio`` to run
    the same demo under trio instead of asyncio. The chosen backend name is
    passed to the demo so it can report it (anyio no longer depends on sniffio,
    so the runner's choice is the source of truth).
    """
    backend = os.environ.get("PUBSUB_DEMO_BACKEND", "asyncio")
    if backend not in ("asyncio", "trio"):
        raise SystemExit(
            f"PUBSUB_DEMO_BACKEND must be 'asyncio' or 'trio', got {backend!r}"
        )
    configure_logging()
    anyio.run(demo, backend, backend=backend)
