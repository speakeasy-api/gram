from collections.abc import Sequence

import click

from pystreams import partition


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
            # No default: a deployment that forgets the partition fails at
            # startup instead of running as the wrong one. Click treats a set
            # but empty GRAM_PARTITION as unset, so it fails the same way,
            # matching the Go server.
            required=True,
            envvar="GRAM_PARTITION",
            show_envvar=True,
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
