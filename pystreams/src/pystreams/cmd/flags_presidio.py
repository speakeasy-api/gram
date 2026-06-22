from collections.abc import Sequence

import click


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
            default=1000,
            envvar="GRAM_PYSTREAMS_SCAN_MAX_TASKS_PER_CHILD",
            help=(
                "Recycle a scan-pool worker after this many scans to bound memory "
                "drift (like gunicorn --max-requests). <=0 disables recycling. "
                "Only applies when --scan-workers > 0."
            ),
        ),
        click.Option(
            ["--scan-timeout"],
            type=float,
            default=60.0,
            envvar="GRAM_PYSTREAMS_SCAN_TIMEOUT",
            help=(
                "Seconds a single scan may run before it is treated as a failure "
                "(like gunicorn --timeout). <=0 disables the bound. Only applies "
                "when --scan-workers > 0."
            ),
        ),
    ]
