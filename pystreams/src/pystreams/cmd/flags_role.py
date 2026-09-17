from collections.abc import Sequence

import click


def role_options() -> Sequence[click.Option]:
    return [
        click.Option(
            ["--role"],
            type=click.Choice(["all", "analysis", "enforcement"]),
            default="all",
            envvar="GRAM_PYSTREAMS_ROLE",
            help="Select which subscription receivers this process runs.",
        )
    ]
