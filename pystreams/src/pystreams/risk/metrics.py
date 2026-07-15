"""Per-message Presidio processing metrics.

Two duration distributions, answering different questions:

- ``process_duration`` — how long the
  :class:`~pystreams.risk.handler.PresidioHandler` spends processing a single
  ``PresidioAnalysis`` message end to end: the scan (incl. any wait for a free
  scan slot / pool worker) plus building and publishing a finding per detection.
  Recorded as wall clock, so the distribution reflects real per-message latency
  on the path — the latency/SLO signal, where queue wait under load is itself
  informative (pool saturation).
- ``scan_duration`` — how long the scan itself *executed*, measured where it ran
  (a pool worker process or an anyio worker thread), excluding slot/queue wait
  and publish. This is the compute-cost signal: split by ``size_bucket`` it
  shows the cost curve against input size, unpolluted by saturation.

Both carry a ``size_bucket`` attribute (log-scale bands of the scanned content's
character length) so either distribution can be split by input size at bounded
cardinality. Exact per-message sizes belong on the delivery span
(``gram.risk.content_chars``), not on a metric tag.

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
# The scan never reached a pool worker within the slot budget; the message was
# nacked for redelivery. This delivery's wall clock still records (it is real
# latency), but split out so backlog requeues don't read as scan failures.
OUTCOME_REQUEUED: Final = "requeued"

_OUTCOME_ATTR: Final = "outcome"

# Input-size band of the scanned content, attached to both distributions so they
# can be split by size at bounded cardinality (5 values). Measured in characters
# — the cost driver for the NLP pass — not UTF-8 bytes. Values avoid characters
# Datadog tag normalization would mangle (no ``<``/``>=``).
_SIZE_BUCKET_ATTR: Final = "size_bucket"

# Upper bounds (exclusive, in characters) and the label of each band, smallest
# first; anything past the last bound falls into the terminal band.
_SIZE_BUCKETS: Final = (
    (1_000, "0-1k"),
    (10_000, "1k-10k"),
    (100_000, "10k-100k"),
    (1_000_000, "100k-1m"),
)
_SIZE_BUCKET_MAX: Final = "1m-inf"

_meter = metrics.get_meter("github.com/speakeasy-api/gram/pystreams")

# Bucket boundaries (seconds) span sub-second processing up to multi-minute:
# Presidio on a large payload (or a long wait for a free scan slot / pool worker
# under load) can run for minutes, so the distribution reaches a 10-minute upper
# bound rather than saturating the top bucket on the slow tail that matters most.
_process_duration = _meter.create_histogram(
    "gram.pystreams_presidio.process_duration",
    unit="s",
    description=(
        "Wall-clock time to process one message through the Presidio handler "
        "(scan, incl. scan-slot wait, plus building and publishing findings)."
    ),
    explicit_bucket_boundaries_advisory=[
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


# Buckets (seconds) are finer at the low end than process_duration's: a scan on
# a typical payload completes in milliseconds-to-seconds, and the shape of that
# region is exactly what the cost-vs-size question needs. The tail still reaches
# the 5-minute scan timeout so pathological inputs stay visible.
_scan_duration = _meter.create_histogram(
    "gram.pystreams_presidio.scan_duration",
    unit="s",
    description=(
        "Execution time of a single Presidio scan, measured where it ran (pool "
        "worker process or worker thread) — excludes scan-slot/pool queue wait "
        "and finding publish."
    ),
    explicit_bucket_boundaries_advisory=[
        0.05,
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
    ],
)


def size_bucket_for(content_chars: int) -> str:
    """Map a content length (characters) onto its low-cardinality size band."""
    for bound, label in _SIZE_BUCKETS:
        if content_chars < bound:
            return label
    return _SIZE_BUCKET_MAX


def record_process_duration(seconds: float, outcome: str, size_bucket: str) -> None:
    """Record one message's end-to-end processing duration.

    Tagged by terminal outcome and the input-size band (``size_bucket_for``), so
    the latency distribution can be split by path and by size.
    """
    _process_duration.record(
        seconds, {_OUTCOME_ATTR: outcome, _SIZE_BUCKET_ATTR: size_bucket}
    )


def record_scan_duration(seconds: float, size_bucket: str) -> None:
    """Record one scan's execution time, tagged by the input-size band.

    Recorded by the scanners on scan success only: a timed-out or failed scan
    never reports a duration (its cost shows up in ``process_duration`` under
    ``outcome=error`` instead), so this distribution stays a clean read of what
    completed scans cost.
    """
    _scan_duration.record(seconds, {_SIZE_BUCKET_ATTR: size_bucket})
