from typing import Sequence
import click


def service_options() -> Sequence[click.Option]:
    return [
        click.Option(
            ["--service-version"],
            type=str,
            envvar="GRAM_SERVICE_VERSION",
            help="Set the service version.",
        ),
        click.Option(
            ["--environment"],
            type=str,
            envvar="GRAM_ENVIRONMENT",
            help="Set the environment.",
        ),
        click.Option(
            ["--log-level"],
            type=click.Choice(
                ["debug", "info", "warning", "error", "critical"],
                case_sensitive=False,
            ),
            default="info",
            envvar="GRAM_LOG_LEVEL",
            help="Set the logging level.",
        ),
        click.Option(
            ["--pretty-log"],
            is_flag=True,
            default=False,
            envvar="GRAM_LOG_PRETTY",
            help="Enable pretty logging output.",
        ),
    ]
