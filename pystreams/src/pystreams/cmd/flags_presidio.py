import math
from collections.abc import Sequence

import click


def _finite_float(
    ctx: click.Context, param: click.Parameter, value: float | None
) -> float | None:
    """Reject non-finite timeout values at parse time.

    ``float`` parsing accepts ``nan`` and ``inf``, and NaN fails every
    comparison — so it would slip past the scanners' ``<= 0`` disable checks
    and *silently* turn a timeout off, reverting queued scans to unbounded
    waits. A misconfigured bound should fail loudly here, where the error
    names the flag; ``<= 0`` remains the one documented way to disable one.
    """
    if value is not None and not math.isfinite(value):
        raise click.BadParameter("must be a finite number (use <=0 to disable)")
    return value


def presidio_options() -> Sequence[click.Option]:
    return [
        click.Option(
            ["--max-scan-concurrency"],
            type=int,
            default=None,
            envvar="GRAM_PYSTREAMS_SCAN_CONCURRENCY",
            help=(
                "Cap on concurrent Presidio scans (GIL-bound CPU work) for the "
                "in-process scanner. Unset uses the handler default; <=0 disables "
                "the cap. Ignored when --scan-workers > 0."
            ),
        ),
        click.Option(
            ["--scan-workers"],
            type=int,
            default=2,
            envvar="GRAM_PYSTREAMS_SCAN_WORKERS",
            help=(
                "Run Presidio scans in a pool of this many worker processes, each "
                "with its own GIL, to break the single-process throughput ceiling. "
                "Defaults to 2; 0 scans in-process on threads instead. Each worker "
                "loads its own spaCy model, so keep this small (2-4)."
            ),
        ),
        click.Option(
            ["--scan-max-tasks-per-child"],
            type=int,
            default=10_000,
            envvar="GRAM_PYSTREAMS_SCAN_MAX_TASKS_PER_CHILD",
            help=(
                "Recycle a scan-pool worker after this many scans to bound memory "
                "drift (like gunicorn --max-requests). Each recycle costs a full "
                "spaCy model reload on the replacement worker, so size it in "
                "hours of traffic. <=0 disables recycling. Only applies when "
                "--scan-workers > 0."
            ),
        ),
        click.Option(
            ["--scan-timeout"],
            type=float,
            default=300.0,
            callback=_finite_float,
            envvar="GRAM_PYSTREAMS_SCAN_TIMEOUT",
            help=(
                "Seconds a single scan may execute before it is treated as a "
                "failure (like gunicorn --timeout). <=0 disables the bound. Only "
                "applies when --scan-workers > 0."
            ),
        ),
        click.Option(
            ["--scan-slot-timeout"],
            type=float,
            default=60.0,
            callback=_finite_float,
            envvar="GRAM_PYSTREAMS_SCAN_SLOT_TIMEOUT",
            help=(
                "Seconds a scan may wait for a free pool worker before the "
                "message is requeued (nacked for redelivery) instead of burning "
                "the execution budget on queue wait. <=0 disables the bound. "
                "Only applies when --scan-workers > 0."
            ),
        ),
        click.Option(
            ["--max-inflight"],
            type=int,
            default=None,
            envvar="GRAM_PYSTREAMS_MAX_INFLIGHT",
            help=(
                "Cap on PresidioAnalysis messages processed concurrently by this "
                "process. Excess backlog then waits at the broker (visible and "
                "redeliverable) rather than in-process where it spends the scan "
                "slot budget. Unset derives from scan capacity (2 handlers per "
                "scan slot, minimum 4); <=0 disables the cap."
            ),
        ),
    ]
