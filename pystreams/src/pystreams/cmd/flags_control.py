from typing import Sequence
import click


def server_options() -> Sequence[click.Option]:
    return [
        click.Option(
            ["--control-host"],
            type=str,
            default="0.0.0.0",
            envvar="GRAM_PYSTREAMS_CONTROL_HOST",
            help="Host/interface for the Kubernetes control server to bind.",
        ),
        click.Option(
            ["--control-port"],
            type=int,
            default=8089,
            envvar="GRAM_PYSTREAMS_CONTROL_PORT",
            help="Port for the Kubernetes control server (/healthz, /readyz).",
        ),
    ]
