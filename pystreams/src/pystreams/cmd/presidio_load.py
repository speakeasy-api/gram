"""Load generator for the PresidioAnalyzer subscription, for local profiling.

Publishes a burst of ``gram.risk.v1.PresidioAnalysis`` messages onto the local
Pub/Sub emulator so a locally-running ``pystreams multi`` consumes them through
the PresidioAnalyzer subscription. Use it to reproduce and profile that
subscription's ACK latency — the wall time pystreams spends scanning content and
publishing findings per message — without real GCP or production traffic.

Three-step local loop:

    # 1. emulator up (compose.yml: pubsub-emulator on :8088)
    docker compose up -d pubsub-emulator

    # 2. pystreams consuming against it (auto-reconciles topic + subscription)
    PUBSUB_EMULATOR_HOST=localhost:8088 mise run start:pystreams-multi

    # 3. fire load (the mise task sets PUBSUB_EMULATOR_HOST from mise.toml)
    mise run risk:presidio-load -- --count 500 --concurrency 50

The project id defaults to ``my-project-id`` — the same fallback ``multi`` uses
when ``--pubsub-emulator-host`` is set (see ``multi.py``) — so both sides resolve
the same topic/subscription paths. Override with ``--gcp-project-id`` only if you
started pystreams with an explicit one.

This measures and reports the *publish* (offered-load) side only. The consumer's
per-message ACK cost is observed on the pystreams side: the handler's
``presidio scan detected entities`` log carries ``scan_ms`` / ``publish_ms``, and
a flame graph over the running process shows where the scan time goes.
"""

from __future__ import annotations

import math
import os
import time
from functools import partial

import anyio
import click
import structlog
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.risk.v1 import presidio_analysis_pb2
from gram_infra.pubsub import EmulatedPubSubBroker, pubsub_publisher_for_message_async
from gram_infra.pubsub.publisher import Publisher

from pystreams import attr
from pystreams.deps import logging

from . import flags_gcp

# The fallback ``multi`` uses for the emulator project; match it so the publisher
# and consumer resolve the same topic path.
DEFAULT_PROJECT_ID = "my-project-id"

# Prose with several reliably-detected PII entities (a name, an email, a phone
# number, and a US SSN). Kept off placeholder domains / reserved ranges so the
# handler's false-positive filters don't drop the findings before they publish.
# ``{i}`` makes each rendering distinct so nothing can be deduped en route.
_PII_BLOCK = (
    "Hi, this is Sarah Connor. You can reach me at "
    "sarah.connor.{i}@cyberdyne-systems.net or call me on (415) 553-0{n:03d}. "
    "For verification my social security number is 457-55-{s:04d}. Thanks for "
    "getting back to me so quickly about the order."
)


def _build_content(index: int, pii: int) -> str:
    """Build message content carrying roughly ``pii`` detectable entities.

    Each block contributes ~4 findings (name, email, phone, SSN); blocks are
    repeated to reach the requested count. Repetition also lengthens the text,
    which is what drives the spaCy NER scan cost the profile is interested in.
    """
    blocks = max(1, round(pii / 4))
    return " ".join(
        _PII_BLOCK.format(i=f"{index}-{b}", n=(index + b) % 1000, s=(index + b) % 10000)
        for b in range(blocks)
    )


def _build_message(
    index: int, pii: int, entities: list[str]
) -> presidio_analysis_pb2.PresidioAnalysis:
    """Construct one synthetic analysis request with unique routing context."""
    return presidio_analysis_pb2.PresidioAnalysis(
        request_id=f"load-req-{index}",
        chat_message_id=f"load-msg-{index}",
        project_id="load-proj",
        organization_id="load-org",
        risk_policy_id="load-policy",
        risk_policy_version=1,
        created_at="2026-01-01T00:00:00Z",
        reply_urn=f"urn:gram:load:{index}",
        content=_build_content(index, pii),
        # Empty => the handler asks Presidio to scan every type (the heaviest
        # path). A scoped list only runs the recognizers for those entities, which
        # is the lever measured by --entities.
        entities=entities,
    )


def _percentile(sorted_values: list[float], pct: float) -> float:
    """Nearest-rank percentile of an already-sorted, non-empty list.

    Nearest-rank: the ``ceil(pct/100 * N)``-th value (1-indexed), clamped into
    range. ``ceil`` (not ``round``) is what makes it nearest-rank — ``round`` can
    pick a lower rank when ``pct/100 * N`` has a fractional part below .5, slightly
    under-reporting high percentiles.
    """
    if not sorted_values:
        return 0.0
    rank = math.ceil(pct / 100 * len(sorted_values))
    idx = min(max(rank, 1), len(sorted_values)) - 1
    return sorted_values[idx]


async def _publish_all(
    publisher: Publisher[presidio_analysis_pb2.PresidioAnalysis],
    *,
    count: int,
    concurrency: int,
    pii: int,
    entities: list[str],
) -> list[float]:
    """Publish ``count`` messages with at most ``concurrency`` in flight.

    A fixed pool of worker tasks pulls indices off a shared counter rather than
    spawning ``count`` tasks up front, so a large run doesn't allocate a task per
    message. ``next()`` has no checkpoint, so the counter is race-free on the
    single-threaded event loop. Returns per-publish durations in milliseconds.
    """
    durations: list[float] = []
    indices = iter(range(count))

    # concurrency is validated at the CLI boundary (IntRange min=1), so it is >= 1.
    async def worker() -> None:
        for index in indices:
            message = _build_message(index, pii, entities)
            started = time.perf_counter()
            # Await the commit so the recorded duration is the full publish
            # latency, not just the time to hand the message to the client.
            await publisher.publish(message).get()
            durations.append((time.perf_counter() - started) * 1000)

    async with anyio.create_task_group() as tg:
        for _ in range(concurrency):
            tg.start_soon(worker)

    return durations


async def run(
    *,
    gcp_project_id: str | None,
    pubsub_emulator_host: str | None,
    count: int,
    concurrency: int,
    pii: int,
    entities: str,
) -> None:
    logging.configure_logging(
        pretty_log=True,
        log_level="info",
        base_attrs={attr.SERVICE_NAME: "gram-presidio-load"},
    )
    logger: structlog.stdlib.BoundLogger = structlog.get_logger()

    if not pubsub_emulator_host:
        # Guard against accidentally pointing a PII load generator at real GCP.
        raise click.UsageError(
            "--pubsub-emulator-host (or PUBSUB_EMULATOR_HOST) is required: this "
            "load generator only runs against the local emulator. Start it with "
            "'docker compose up -d pubsub-emulator'."
        )
    # The google clients auto-detect the emulator from this env var; write it back
    # unconditionally so the explicit flag wins over any stale pre-existing value.
    os.environ["PUBSUB_EMULATOR_HOST"] = pubsub_emulator_host

    project_id = gcp_project_id or DEFAULT_PROJECT_ID
    entity_list = [e.strip() for e in entities.split(",") if e.strip()]
    logger.info(
        "starting presidio load",
        project=project_id,
        emulator=pubsub_emulator_host,
        count=count,
        concurrency=concurrency,
        pii_per_message=pii,
        entities=entity_list or "ALL",
    )

    # EmulatedPubSubBroker reconciles the PresidioAnalysis topic on demand, so the
    # topic exists even if pystreams (which owns the subscription) has not started
    # yet — though messages only get consumed once it has.
    broker = EmulatedPubSubBroker(
        project_id, PublisherClient(), SubscriberClient(), logger=logger
    )
    with broker:
        publisher = await pubsub_publisher_for_message_async(
            broker, presidio_analysis_pb2.PresidioAnalysis
        )

        wall_started = time.perf_counter()
        durations = await _publish_all(
            publisher,
            count=count,
            concurrency=concurrency,
            pii=pii,
            entities=entity_list,
        )
        wall = time.perf_counter() - wall_started

    durations.sort()
    throughput = len(durations) / wall if wall > 0 else 0.0
    logger.info(
        "presidio load complete",
        published=len(durations),
        wall_seconds=round(wall, 2),
        throughput_per_sec=round(throughput, 1),
        publish_ms_p50=round(_percentile(durations, 50), 1),
        publish_ms_p90=round(_percentile(durations, 90), 1),
        publish_ms_p99=round(_percentile(durations, 99), 1),
        publish_ms_max=round(durations[-1], 1) if durations else 0.0,
    )


@click.command(
    "presidio-load",
    params=[
        *flags_gcp.pubsub_options(),
        click.Option(
            ["--count"],
            # Validate at the boundary: a non-positive count is a misconfiguration,
            # not a run with zero messages silently coerced away.
            type=click.IntRange(min=1),
            default=200,
            show_default=True,
            help="Total messages to publish.",
        ),
        click.Option(
            ["--concurrency"],
            # min=1 rejects bad input here rather than silently coercing to a single
            # worker downstream, which would mask the misconfiguration and skew the
            # load-test results.
            type=click.IntRange(min=1),
            default=50,
            show_default=True,
            help="Maximum publishes in flight at once.",
        ),
        click.Option(
            ["--pii"],
            type=click.IntRange(min=1),
            default=4,
            show_default=True,
            help="Approximate detectable PII entities per message (drives both "
            "the scan cost and the per-message finding/publish fan-out).",
        ),
        click.Option(
            ["--entities"],
            type=str,
            default="",
            help="Comma-separated Presidio entity types to scope the scan to "
            "(e.g. EMAIL_ADDRESS,PHONE_NUMBER,US_SSN). Empty scans all types.",
        ),
    ],
)
def cli(**kwargs) -> None:
    anyio.run(partial(run, **kwargs))


if __name__ == "__main__":
    cli()
