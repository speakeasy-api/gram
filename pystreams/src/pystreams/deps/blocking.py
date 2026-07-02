"""Fail-fast detection of blocking code on the event loop.

An asyncio event loop runs cooperatively on a single thread, so any coroutine
that fails to yield — CPU-bound work, a synchronous client call, an un-awaited
blocking call — stalls every other ready task until it returns. ``loop_lag.py``
records that pressure passively into a histogram; this module is the active
counterpart: it installs `aiocop <https://github.com/Feverup/aiocop>`_, a runtime
monitor that hooks ``sys.audit`` and the running loop to spot blocking calls as
they happen, logs the offending task, and — when ``raise_on_violations`` is set —
turns a high-severity violation into a ``HighSeverityBlockingIoException`` so the
problem fails loudly in local development and tests instead of silently degrading
throughput in production.

It only sees blocking code while async code actually executes, so activation
lives at the app entrypoint (gated by ``GRAM_PYSTREAMS_DETECT_BLOCKING``) and in
the pytest suite, never in a static lint step.
"""

from __future__ import annotations

import aiocop
import structlog
from opentelemetry import metrics

from pystreams import attr

# Tasks slower than this without yielding are treated as blocking the loop. The
# trivial ping handler stays well under it, and the Presidio scan is offloaded to
# a worker thread (so it never counts), leaving real blocking calls to stand out.
DEFAULT_THRESHOLD_MS = 50

_meter = metrics.get_meter("github.com/speakeasy-api/gram/pystreams")

# Count of detected blocking-IO violations, broken down by severity. Until a
# MeterProvider is configured the instrument is the API's implicit no-op, so
# incrementing is cheap; once a provider is installed the count flows out wherever
# metrics are collected.
_violations_counter = _meter.create_counter(
    "python.event_loop.blocking_violations",
    unit="{violation}",
    description="Blocking-IO violations detected on the event loop, by severity.",
)


def activate_blocking_detection(
    *,
    logger: structlog.stdlib.BoundLogger,
    threshold_ms: int = DEFAULT_THRESHOLD_MS,
    raise_on_violations: bool = True,
) -> None:
    """Install and activate aiocop's blocking-IO monitor.

    Wires the aiocop setup sequence and registers a structlog callback for every
    detected slow task. When ``raise_on_violations`` is true a high-severity
    violation also raises ``aiocop.HighSeverityBlockingIoException`` from within
    the offending task. Call once, early, while the event loop is running.
    """
    logger = logger.bind(**{attr.COMPONENT: "aiocop"})

    def _on_slow_task(event: aiocop.SlowTaskEvent) -> None:
        # aiocop's own stack capture (traceback.extract_stack -> linecache) trips
        # dozens of os.stat/open audit events per scheduled step, inflating the
        # severity score to medium/high even when the task never stalled the loop.
        # Elapsed-vs-threshold is the honest signal: a task that ran under the
        # threshold didn't block meaningfully regardless of severity label, so
        # gate logging on that and let the severity descriptor only colour the
        # entries that genuinely overran. Raising on real violations is handled
        # separately inside aiocop (enable_raise_on_violations), so this only
        # quiets the logs and never suppresses a genuine stall.
        if not event.exceeded_threshold:
            return
        _violations_counter.add(1, {"severity": event.severity_level})
        logger.warning(
            "blocking code detected on the event loop",
            severity=event.severity_level,
            elapsed_ms=round(event.elapsed_ms, 1),
            threshold_ms=round(event.threshold_ms, 1),
            reason=event.reason,
            blocking_events=[str(e) for e in event.blocking_events],
        )

    aiocop.patch_audit_functions()
    aiocop.start_blocking_io_detection()
    aiocop.detect_slow_tasks(threshold_ms=threshold_ms, on_slow_task=_on_slow_task)
    aiocop.activate()
    if raise_on_violations:
        aiocop.enable_raise_on_violations()

    logger.info(
        "event-loop blocking detection enabled",
        threshold_ms=threshold_ms,
        raise_on_violations=raise_on_violations,
    )
