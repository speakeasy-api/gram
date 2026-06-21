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
                "Cap on concurrent Presidio scans (GIL-bound CPU work). Unset "
                "uses the handler default; <=0 disables the cap."
            ),
        ),
    ]
