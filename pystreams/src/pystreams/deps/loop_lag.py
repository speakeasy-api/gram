"""Event-loop lag sampling.

An asyncio event loop runs cooperatively on a single thread, so any coroutine
that fails to yield — CPU-bound work, a synchronous client call, an un-awaited
blocking call — delays every other ready task until it returns. A liveness probe
only catches the terminal case (a fully wedged loop), giving no early warning
that the loop is starting to saturate.

This samples the loop's scheduling delay so that pressure is observable before
it becomes an outage. The sampler parks on ``anyio.sleep(interval)`` and compares
the wall-clock elapsed against the interval it asked for: the excess is time the
loop spent unable to run a ready timer — a direct proxy for loop saturation.

Each sample is recorded into an OpenTelemetry histogram. Until a ``MeterProvider``
is configured the instrument resolves to the API's implicit no-op, so recording
is a cheap no-op; once a provider is installed the distribution (p50/p99/max)
flows out wherever metrics are collected, with no change here.
"""

from __future__ import annotations

import time

import anyio
from opentelemetry import metrics

_meter = metrics.get_meter("github.com/speakeasy-api/gram/pystreams")

# Distribution of event-loop scheduling delay, in seconds.
_lag_histogram = _meter.create_histogram(
    "python.event_loop.lag",
    unit="s",
    description=(
        "Delay between when a loop timer was scheduled to fire and when the "
        "event loop actually ran it; a proxy for loop saturation."
    ),
)


async def monitor_event_loop_lag(interval: float = 0.5) -> None:
    """Sample event-loop lag until cancelled.

    Launch with ``task_group.start_soon``; it is cancelled cleanly with the
    surrounding task group on shutdown. ``interval`` is how often a sample is
    taken — a short, cheap sleep whose overshoot is recorded as the lag.
    """
    while True:
        start = time.perf_counter()
        await anyio.sleep(interval)
        # Overshoot beyond the requested interval is loop lag: the timer was due
        # at ``start + interval`` but the loop only got back to it ``lag`` later.
        lag = max(0.0, (time.perf_counter() - start) - interval)
        _lag_histogram.record(lag)
