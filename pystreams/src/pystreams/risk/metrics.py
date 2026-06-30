"""Per-message Presidio processing metrics.

Records how long the :class:`~pystreams.risk.handler.PresidioHandler` spends
processing a single ``PresidioAnalysis`` message end to end — the scan (incl. any
wait for a free scan slot / pool worker) plus building and publishing a finding
per detection. Recorded as wall clock, so the distribution reflects real
per-message latency on the path, not just the CPU-bound scan.

Following the rest of pystreams (``deps/loop_lag.py``, ``deps/blocking.py``), the
instrument is created at import against the OpenTelemetry API. Until a
``MeterProvider`` is configured it resolves to the API's implicit no-op, so
recording is cheap; once a provider is installed (``deps/otel.py``) the
distribution flows out wherever metrics are collected, with no change here.
"""

from __future__ import annotations

from typing import Final

from opentelemetry import metrics

# Terminal outcome of a single message, attached to every recorded duration so
# the distribution can be split by path. Low cardinality by construction.
OUTCOME_DETECTED: Final = "detected"  # findings were published
OUTCOME_CLEAN: Final = "clean"  # scan succeeded, nothing detected
OUTCOME_ERROR: Final = "error"  # scan failed and was swallowed

_OUTCOME_ATTR: Final = "outcome"

_meter = metrics.get_meter("github.com/speakeasy-api/gram/pystreams")

# Bucket boundaries (seconds) span the few-millisecond scan of a tiny clean
# message all the way to multi-minute processing: Presidio on a large payload
# (or a long wait for a free scan slot / pool worker under load) can run for
# minutes, so the distribution reaches a 10-minute upper bound rather than
# saturating the top bucket on the slow tail that matters most.
_process_duration = _meter.create_histogram(
    "pystreams.presidio.process.duration",
    unit="s",
    description=(
        "Wall-clock time to process one message through the Presidio handler "
        "(scan, incl. scan-slot wait, plus building and publishing findings)."
    ),
    explicit_bucket_boundaries_advisory=[
        0.005,
        0.025,
        0.1,
        0.25,
        0.5,
        1.0,
        2.5,
        5.0,
        10.0,
        30.0,
        60.0,
        120.0,
        300.0,
        600.0,
    ],
)


def record_process_duration(seconds: float, outcome: str) -> None:
    """Record one message's end-to-end processing duration, tagged by outcome."""
    _process_duration.record(seconds, {_OUTCOME_ATTR: outcome})
