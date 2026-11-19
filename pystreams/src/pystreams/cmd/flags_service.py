import os
from collections.abc import Sequence

import click

from pystreams import partition


def _reject_empty_partition_env(
    _ctx: click.Context, _param: click.Parameter, value: str
) -> str:
    """Fail on a GRAM_PARTITION that is set but empty.

    Click treats an empty environment variable as unset and falls back to the
    default, while the Go server rejects it. Rejecting it here too keeps a
    misrendered deployment from running pystreams as ``us`` while the Go
    processes refuse to start.
    """
    if os.environ.get("GRAM_PARTITION") == "":
        raise click.BadParameter(
            "GRAM_PARTITION is set but empty; set it to one of: "
            + ", ".join(partition.PARTITIONS)
        )
    return value


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
            ["--partition"],
            type=click.Choice(partition.PARTITIONS, case_sensitive=True),
            default=partition.DEFAULT,
            show_default=True,
            envvar="GRAM_PARTITION",
            callback=_reject_empty_partition_env,
            help="Partition this deployment runs in. A partition is an "
            "independent production deployment with its own data stores and "
            "hosts.",
        ),
        click.Option(
            ["--git-sha"],
            type=str,
            envvar="GRAM_GIT_SHA",
            help="Build commit SHA, stamped onto telemetry for Datadog "
            "source-code links.",
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
        click.Option(
            ["--enable-tracing"],
            is_flag=True,
            default=False,
            envvar="GRAM_ENABLE_OTEL_TRACES",
            help="Export OpenTelemetry traces via OTLP.",
        ),
        click.Option(
            ["--enable-metrics"],
            is_flag=True,
            default=False,
            envvar="GRAM_ENABLE_OTEL_METRICS",
            help="Export OpenTelemetry metrics via OTLP.",
        ),
    ]
