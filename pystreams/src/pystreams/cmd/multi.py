import logging as stdlogging
import os
import signal
from contextlib import AsyncExitStack
from functools import partial

import anyio
import click
import structlog
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.ping.v2 import ping_pb2, processor_pb2
from gram.risk.v1 import (
    finding_pb2,
    presidio_analysis_pb2,
    presidio_analyzer_pb2,
    presidio_enforcement_pb2,
    presidio_enforcer_pb2,
)
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    PubSubBroker,
    pubsub_publisher_for_message_async,
)
from redis import asyncio as redis_asyncio

from pystreams import attr
from pystreams.deps import logging, otel
from pystreams.deps.blocking import activate_blocking_detection
from pystreams.deps.loop_lag import monitor_event_loop_lag
from pystreams.deps.scanner import build_presidio_scanner
from pystreams.health import HealthState, serve_control
from pystreams.ping.handler import PingHandler
from pystreams.risk.enforce_handler import PresidioEnforceHandler
from pystreams.risk.fingerprint import parse_pepper_keyring
from pystreams.risk.handler import PresidioHandler
from pystreams.risk.replywriter import ReplyWriter

from . import flags_control, flags_enforce, flags_gcp, flags_presidio, flags_service
from .receiver import ReceiverGroup


@click.command(
    "multi",
    params=[
        *flags_service.service_options(),
        *flags_control.server_options(),
        *flags_gcp.pubsub_options(),
        *flags_presidio.presidio_options(),
        *flags_enforce.enforce_options(),
    ],
)
def cli(**kwargs):
    anyio.run(partial(multi, **kwargs))


async def multi(
    *,
    # Service options
    service_version: str | None,
    environment: str | None,
    git_sha: str | None,
    log_level: str,
    pretty_log: bool,
    enable_tracing: bool,
    enable_metrics: bool,
    # GCP options
    gcp_project_id: str | None,
    pubsub_emulator_host: str | None,
    # Control server options
    control_host: str,
    control_port: int,
    # Presidio options
    max_scan_concurrency: int | None,
    scan_workers: int,
    scan_max_tasks_per_child: int,
    scan_timeout: float,
    scan_slot_timeout: float,
    max_inflight: int | None,
    # Enforcement lane options
    redis_addr: str | None,
    redis_password: str | None,
    risk_fingerprint_pepper_keyring: str | None,
):
    logging.configure_logging(
        pretty_log=pretty_log,
        log_level=log_level,
        base_attrs={
            attr.SERVICE_NAME: "gram-pystreams",
            attr.SERVICE_VERSION: service_version,
            attr.SERVICE_ENVIRONMENT: environment,
        },
    )
    logger: structlog.stdlib.BoundLogger = structlog.get_logger()

    otel_options = otel.OTelOptions(
        service_name="gram-pystreams",
        service_version=service_version,
        git_sha=git_sha,
        environment=environment,
        enable_tracing=enable_tracing,
        enable_metrics=enable_metrics,
    )

    # The emulator's project ID is arbitrary; against real GCP a project is
    # required to resolve the subscription path.
    project_id = gcp_project_id or ("my-project-id" if pubsub_emulator_host else None)
    if project_id is None:
        raise click.UsageError(
            "--gcp-project-id is required unless --pubsub-emulator-host is set"
        )

    broker = _build_broker(
        project_id=project_id,
        emulator_host=pubsub_emulator_host,
        logger=logger,
    )

    ping_log_level = stdlogging.DEBUG if environment != "local" else stdlogging.INFO

    # Install the OTel SDK outermost so its providers flush on exit only after
    # the receivers have drained — the per-message spans and loop-lag/blocking
    # metrics, dormant proxies until now, start exporting here.
    async with otel.otel_sdk(otel_options, logger=logger):
        # The broker owns the publisher/subscriber clients: entering it flushes
        # and closes them on exit (including a clean teardown on Ctrl-C).
        # The exit stack owns the enforcement Redis client from the moment it
        # exists, so a startup failure anywhere after creation (ping, keyring
        # parse, scan-pool build) still closes its connection pool.
        async with AsyncExitStack() as stack:
            stack.enter_context(broker)
            health_state = HealthState()
            findings_publisher = await pubsub_publisher_for_message_async(
                broker, finding_pb2.Finding
            )

            # The enforcement lane needs a Redis client for reply inboxes and
            # the pepper keyring for tenant-scoped fingerprints; without both,
            # the receiver is simply not registered and the process serves only
            # the batch subscriptions. Validated before the scan pool exists so
            # a startup failure here cannot leak pool workers.
            enforce_redis = None
            enforce_fingerprinter = None
            if redis_addr and risk_fingerprint_pepper_keyring:
                host, _, port = redis_addr.rpartition(":")
                # Accept bracketed IPv6 endpoints such as [::1]:6379.
                host = host.strip("[]")
                # Bounded socket timeouts so a Redis stall surfaces as a
                # write failure (which the handler ACKs) instead of hanging
                # the handler past its ack deadline.
                enforce_redis = redis_asyncio.Redis(
                    host=host or redis_addr,
                    port=int(port) if port else 6379,
                    password=redis_password or None,
                    socket_connect_timeout=1.0,
                    socket_timeout=2.0,
                )
                stack.push_async_callback(enforce_redis.aclose)
                # Fail startup, not per-message: consuming enforcement
                # requests without a reachable reply store would ACK every
                # request while losing its reply.
                await enforce_redis.ping()
                enforce_fingerprinter = parse_pepper_keyring(
                    risk_fingerprint_pepper_keyring
                )
            else:
                logger.info(
                    "presidio enforcement lane disabled",
                    reason="redis address or fingerprint pepper keyring not configured",
                )

            # Build the scan strategy: a pool of worker processes (the default,
            # with --scan-workers > 0) or the in-process thread scanner. The
            # selection and its analyzer/concurrency wiring live in
            # build_presidio_scanner.
            presidio_scanner = await build_presidio_scanner(
                scan_workers=scan_workers,
                scan_max_tasks_per_child=scan_max_tasks_per_child,
                scan_timeout=scan_timeout,
                scan_slot_timeout=scan_slot_timeout,
                max_scan_concurrency=max_scan_concurrency,
                logger=logger,
            )

            # Opt-in (defaulted on for local dev via mise.toml): actively watch
            # the steady-state loop for blocking calls and raise on a
            # high-severity violation. Activate only after startup has loaded
            # Presidio: aiocop documents lazy imports as expected startup I/O,
            # and its own stack collection can turn the analyzer's legitimate
            # worker-thread handoff into a noisy slow-task warning. Receivers
            # have not started yet, so all message handling remains covered.
            if os.environ.get("GRAM_PYSTREAMS_DETECT_BLOCKING"):
                activate_blocking_detection(logger=logger)

            presidio_handler = PresidioHandler(
                logger, findings_publisher, presidio_scanner
            )

            enforce_handler = None
            if enforce_redis is not None and enforce_fingerprinter is not None:
                enforce_handler = PresidioEnforceHandler(
                    logger,
                    ReplyWriter(enforce_redis),
                    presidio_scanner,
                    enforce_fingerprinter,
                )

            # The scanner is an async context manager: leaving the block
            # releases it, draining in-flight scans and reaping the worker
            # processes for the pool scanner (a no-op for the in-process one).
            # It exits after the task group drains and before the exit stack
            # closes the Redis client.
            async with presidio_scanner, anyio.create_task_group() as tg:
                tg.start_soon(
                    _shutdown_on_signal, tg.cancel_scope, health_state, logger
                )
                tg.start_soon(monitor_event_loop_lag)
                # Start the health server first (and wait until it is bound) so
                # the liveness probe answers as early as possible, then begin
                # consuming and only then report ready.
                await tg.start(
                    partial(
                        serve_control,
                        health_state,
                        host=control_host,
                        port=control_port,
                        logger=logger,
                    )
                )

                receivers = ReceiverGroup(task_group=tg, broker=broker, logger=logger)

                # Register subscription receivers here. Each call resolves a
                # subscriber and starts consuming with per-message tracing.
                await receivers.receive(
                    ping_pb2.Message,
                    processor_pb2.PyProcessor,
                    PingHandler(logger, ping_log_level).handle,
                )
                # Admit only as many messages as the scan pool can plausibly
                # serve: 2 handlers per scan slot keeps the pool fed while one
                # message's findings publish, and everything past the cap waits
                # at the broker — visible as subscription backlog and
                # redeliverable — instead of in-process, where 50 handlers
                # racing 2 workers spent whole slot budgets queued (the
                # process_duration >> scan_duration gap).
                if max_inflight is None:
                    scan_slots = scan_workers if scan_workers > 0 else 2
                    max_inflight = max(4, 2 * scan_slots)
                await receivers.receive(
                    presidio_analysis_pb2.PresidioAnalysis,
                    presidio_analyzer_pb2.PresidioAnalyzer,
                    presidio_handler.handle,
                    max_concurrency=max_inflight,
                )
                if enforce_handler is not None:
                    # A dedicated, smaller cap: enforcement shares the scan
                    # pool with the batch receiver above, so admitting a
                    # second full max_inflight would double total admission
                    # against the same slots. Roughly one handler per slot
                    # (floored at two so a reply write can overlap a scan)
                    # keeps inline latency low; excess waits at the broker.
                    if scan_workers > 0:
                        enforce_slots = scan_workers
                    else:
                        enforce_slots = max_scan_concurrency or 2
                    await receivers.receive(
                        presidio_enforcement_pb2.PresidioEnforcement,
                        presidio_enforcer_pb2.PresidioEnforcer,
                        enforce_handler.handle,
                        max_concurrency=max(2, enforce_slots),
                    )

                health_state.set_ready()


def _build_broker(
    *,
    project_id: str,
    emulator_host: str | None,
    logger: structlog.stdlib.BoundLogger,
) -> PubSubBroker:
    """Build a broker for the configured environment.

    Against the local emulator there is no Config Connector, so
    ``EmulatedPubSubBroker`` reconciles the topic and subscription on demand. In
    production ``PubSubBroker`` assumes the resources already exist.
    """
    if emulator_host:
        # The google clients auto-detect the emulator from this env var. The CLI
        # flag has already taken precedence over any pre-existing value (Click
        # resolves it that way), so write it back unconditionally — using
        # setdefault here would let a stale env var win over the explicit flag.
        os.environ["PUBSUB_EMULATOR_HOST"] = emulator_host
        return EmulatedPubSubBroker(
            project_id,
            PublisherClient(),
            SubscriberClient(),
            logger=logger,
        )
    return PubSubBroker(project_id, logger=logger)


async def _shutdown_on_signal(
    cancel_scope: anyio.CancelScope,
    health_state: HealthState,
    logger: structlog.stdlib.BoundLogger,
) -> None:
    """Cancel the surrounding task group when SIGINT/SIGTERM arrives.

    Flip readiness off before cancelling so the pod starts failing ``/readyz``
    the moment a shutdown begins — Kubernetes pulls it out of rotation while the
    in-flight handlers drain, rather than racing the cancellation.
    """
    with anyio.open_signal_receiver(signal.SIGINT, signal.SIGTERM) as signals:
        async for _ in signals:
            logger.info("shutting down")
            health_state.set_not_ready()
            cancel_scope.cancel()
            return
