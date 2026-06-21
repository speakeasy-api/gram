import logging as stdlogging
import os
import signal
from functools import partial

import anyio
import click
import structlog
from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.ping.v1 import ping_pb2, processor_pb2
from gram.risk.v1 import finding_pb2, presidio_analysis_pb2, presidio_analyzer_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    PubSubBroker,
    pubsub_publisher_for_message_async,
)

from pystreams import attr
from pystreams.deps import logging
from pystreams.deps.blocking import activate_blocking_detection
from pystreams.deps.loop_lag import monitor_event_loop_lag
from pystreams.health import HealthState, serve_control
from pystreams.ping.handler import PingHandler
from pystreams.risk.handler import PresidioHandler
from pystreams.risk.scanner import (
    ProcessPoolScanner,
    Scanner,
    ThreadScanner,
    build_default_analyzer,
)

from . import flags_control, flags_gcp, flags_presidio, flags_service
from .receiver import ReceiverGroup


@click.command(
    "multi",
    params=[
        *flags_service.service_options(),
        *flags_control.server_options(),
        *flags_gcp.pubsub_options(),
        *flags_presidio.presidio_options(),
    ],
)
def cli(**kwargs):
    anyio.run(partial(multi, **kwargs))


async def multi(
    *,
    # Service options
    service_version: str | None,
    environment: str | None,
    log_level: str,
    pretty_log: bool,
    # GCP options
    gcp_project_id: str | None,
    pubsub_emulator_host: str | None,
    # Control server options
    control_host: str,
    control_port: int,
    # Presidio options
    max_scan_concurrency: int | None,
    scan_workers: int,
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

    # Opt-in (defaulted on for local dev via mise.toml): actively watch the loop
    # for blocking calls and raise on a high-severity violation. The production
    # container leaves the env var unset, so this is a no-op there.
    if os.environ.get("GRAM_PYSTREAMS_DETECT_BLOCKING"):
        activate_blocking_detection(logger=logger)

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

    # The broker owns the publisher/subscriber clients: entering it flushes and
    # closes them on exit (including a clean teardown on Ctrl-C).
    with broker:
        health_state = HealthState()
        findings_publisher = await pubsub_publisher_for_message_async(
            broker, finding_pb2.Finding
        )

        # Choose the scan strategy. With --scan-workers > 0 the scan runs in a pool
        # of worker processes (each with its own GIL), breaking the single-process
        # throughput ceiling inside one pod; otherwise it runs in-process on threads
        # capped by --max-scan-concurrency. The pool owns its own per-worker spaCy
        # models, so the in-process analyzer is only built for the threaded path.
        presidio_scanner: Scanner
        if scan_workers > 0:
            logger.info("starting presidio scan pool", workers=scan_workers)
            presidio_scanner = await ProcessPoolScanner.create(max_workers=scan_workers)
        else:
            analyzer = await build_default_analyzer()
            # Concurrent Presidio scans are GIL-bound, so the scanner caps them at a
            # low default. --max-scan-concurrency overrides it (<=0 disables the
            # cap); unset (None) leaves the scanner default in place.
            concurrency_kwargs = (
                {"max_concurrency": max_scan_concurrency}
                if max_scan_concurrency is not None
                else {}
            )
            presidio_scanner = ThreadScanner(analyzer, **concurrency_kwargs)

        presidio_handler = PresidioHandler(logger, findings_publisher, presidio_scanner)

        try:
            async with anyio.create_task_group() as tg:
                tg.start_soon(
                    _shutdown_on_signal, tg.cancel_scope, health_state, logger
                )
                tg.start_soon(monitor_event_loop_lag)
                # Start the health server first (and wait until it is bound) so the
                # liveness probe answers as early as possible, then begin consuming
                # and only then report ready.
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
                await receivers.receive(
                    presidio_analysis_pb2.PresidioAnalysis,
                    presidio_analyzer_pb2.PresidioAnalyzer,
                    presidio_handler.handle,
                )

                health_state.set_ready()
        finally:
            # Release the scanner: drains in-flight scans and reaps the worker
            # processes for the pool scanner; a no-op for the in-process one.
            await presidio_scanner.aclose()


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
