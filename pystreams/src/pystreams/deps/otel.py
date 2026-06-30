"""OpenTelemetry SDK bootstrap for pystreams.

Installs the global ``TracerProvider``, ``MeterProvider`` and text-map
propagator, wiring OTLP/gRPC exporters that ship to the per-node Datadog Agent.
The entry point is :func:`otel_sdk` — an async context manager that flushes and
tears the providers down on exit.

Behaviour worth knowing:

* **No-op when disabled.** When tracing or metrics are turned off the
  corresponding SDK provider is simply never installed; the OpenTelemetry *API*
  then resolves tracers/instruments to its built-in no-ops. The per-message
  spans (``deps/tracing.py``), the loop-lag histogram (``deps/loop_lag.py``) and
  the blocking-violations counter (``deps/blocking.py``) hold proxy instruments
  that light up unchanged the moment a provider is installed, even though they
  were created at import.
* **Head sampling is env-driven.** The provider keeps its default parent-based
  sampler; operators tune head sampling through the standard
  ``OTEL_TRACES_SAMPLER`` / ``OTEL_TRACES_SAMPLER_ARG`` env vars.
* **Exporter endpoint is env-driven.** The OTLP exporters read
  ``OTEL_EXPORTER_OTLP_ENDPOINT`` (set in ``mise.toml`` for local dev) rather
  than taking an explicit address.
* **Process/runtime metrics.** When metrics are enabled,
  ``SystemMetricsInstrumentor`` registers process/runtime gauges — CPU, RSS,
  CPython GC counts, thread count, context switches — against the meter
  provider.

Blocking discipline: provider/exporter construction and — more importantly —
``shutdown`` (which force-flushes over the network) run on a worker thread via
``asyncer.asyncify`` so they never stall the event loop or trip the aiocop
blocking-IO detector (``deps/blocking.py``). The shutdown is shielded from
cancellation so a Ctrl-C still drains buffered spans/metrics.
"""

from __future__ import annotations

from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass

import anyio
import structlog
from asyncer import asyncify
from opentelemetry import metrics, trace
from opentelemetry.baggage.propagation import W3CBaggagePropagator
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.system_metrics import SystemMetricsInstrumentor
from opentelemetry.propagate import set_global_textmap
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.sdk.metrics import (
    Counter,
    Histogram,
    MeterProvider,
    ObservableCounter,
    ObservableGauge,
    ObservableUpDownCounter,
    UpDownCounter,
)
from opentelemetry.sdk.metrics.export import (
    AggregationTemporality,
    PeriodicExportingMetricReader,
)
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from pystreams import attr

# The repository telemetry is attributed to, for Datadog's source-code links.
_REPOSITORY_URL = "github.com/speakeasy-api/gram"

# How often the periodic reader flushes metrics to the exporter.
_METRIC_EXPORT_INTERVAL_MS = 60_000


@dataclass(frozen=True, slots=True)
class OTelOptions:
    """Inputs for :func:`otel_sdk`."""

    service_name: str
    service_version: str | None
    git_sha: str | None
    environment: str | None
    enable_tracing: bool
    enable_metrics: bool


@asynccontextmanager
async def otel_sdk(
    options: OTelOptions, *, logger: structlog.stdlib.BoundLogger
) -> AsyncIterator[None]:
    """Install the OTel SDK for the duration of the ``async with`` block.

    Sets the global propagator and, depending on ``options``, the SDK tracer
    and/or meter providers, then flushes and shuts them down on exit. Both the
    install and the shutdown are offloaded to a worker thread so the network I/O
    they perform never blocks the event loop; the shutdown additionally runs
    shielded so an in-flight cancellation (Ctrl-C) cannot abandon buffered
    telemetry mid-flush.
    """
    log = logger.bind(**{attr.COMPONENT: "otel"})
    shutdowns = await asyncify(_install)(options, log)
    try:
        yield
    finally:
        with anyio.CancelScope(shield=True):
            await asyncify(_shutdown)(shutdowns, log)


def _install(
    options: OTelOptions, logger: structlog.stdlib.BoundLogger
) -> list[Callable[[], object]]:
    """Configure providers/propagator; return their shutdown callables.

    Runs on a worker thread (see :func:`otel_sdk`). Returns the providers'
    ``shutdown`` methods in install order so the caller can flush them later.
    """
    # Continue producer traces and carry baggage across Pub/Sub boundaries. The
    # SDK already defaults to this composite, but set it explicitly to stay
    # robust to any future default change.
    set_global_textmap(
        CompositePropagator([TraceContextTextMapPropagator(), W3CBaggagePropagator()])
    )

    resource = _build_resource(options)
    shutdowns: list[Callable[[], object]] = []

    if options.enable_metrics:
        logger.info("otel metrics enabled")
        reader = PeriodicExportingMetricReader(
            OTLPMetricExporter(preferred_temporality=_delta_temporality()),
            export_interval_millis=_METRIC_EXPORT_INTERVAL_MS,
        )
        provider = MeterProvider(resource=resource, metric_readers=[reader])
        metrics.set_meter_provider(provider)

        # Register process/runtime gauges (CPU, RSS, CPython GC counts, thread
        # count, context switches) against the meter provider. The instrumentor
        # is a no-op without a real provider, so it goes here, after one is
        # installed. Uninstrument before the provider shuts down so its
        # observable callbacks stop firing first.
        instrumentor = SystemMetricsInstrumentor()
        instrumentor.instrument(meter_provider=provider)
        shutdowns.append(instrumentor.uninstrument)
        shutdowns.append(provider.shutdown)
    else:
        logger.info("otel metrics disabled")

    if options.enable_tracing:
        logger.info("otel tracing enabled")
        provider = TracerProvider(resource=resource)
        provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
        trace.set_tracer_provider(provider)
        shutdowns.append(provider.shutdown)
    else:
        logger.info("otel tracing disabled")

    return shutdowns


def _shutdown(
    shutdowns: list[Callable[[], object]], logger: structlog.stdlib.BoundLogger
) -> None:
    """Flush and close every installed provider, surviving individual failures.

    Runs on a worker thread (see :func:`otel_sdk`). One provider's shutdown
    raising must not strand the others, so each is guarded and logged.
    """
    for fn in shutdowns:
        try:
            fn()
        except Exception:
            logger.exception("otel provider shutdown failed")


def _build_resource(options: OTelOptions) -> Resource:
    """Build the OTel resource describing this process.

    ``Resource.create`` merges these over the SDK defaults (telemetry.sdk.*, plus
    anything from ``OTEL_RESOURCE_ATTRIBUTES``). Unset optional attributes are
    dropped so we never emit empty-string service version/env/sha.
    """
    candidate = {
        attr.SERVICE_NAME: options.service_name,
        attr.SERVICE_VERSION: options.service_version,
        attr.SERVICE_ENVIRONMENT: options.environment,
        attr.DATADOG_GIT_COMMIT_SHA: options.git_sha,
        attr.DATADOG_GIT_REPOSITORY_URL: _REPOSITORY_URL,
    }
    return Resource.create({k: v for k, v in candidate.items() if v is not None})


def _delta_temporality() -> dict[type, AggregationTemporality]:
    """Map each instrument kind to its export temporality.

    Delta for monotonic sums and histograms; cumulative for UpDownCounters,
    where delta is meaningless for a non-monotonic running value. This follows
    Datadog's recommended selector: emitting delta at the SDK makes each pod
    self-contained and the per-node Agent a pass-through, instead of forcing it
    through a fragile stateful cumulative->delta conversion across pod churn.
    """
    delta = AggregationTemporality.DELTA
    cumulative = AggregationTemporality.CUMULATIVE
    return {
        Counter: delta,
        Histogram: delta,
        ObservableCounter: delta,
        ObservableGauge: delta,
        UpDownCounter: cumulative,
        ObservableUpDownCounter: cumulative,
    }
