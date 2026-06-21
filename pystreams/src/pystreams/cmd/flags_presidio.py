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
            default=0,
            envvar="GRAM_PYSTREAMS_SCAN_WORKERS",
            help=(
                "Run Presidio scans in a pool of this many worker processes, each "
                "with its own GIL, to break the single-process throughput ceiling. "
                "0 (default) scans in-process on threads. Each worker loads its own "
                "spaCy model, so keep this small (2-4)."
            ),
        ),
    ]
