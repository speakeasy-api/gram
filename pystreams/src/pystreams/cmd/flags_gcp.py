from collections.abc import Sequence

import click


def pubsub_options() -> Sequence[click.Option]:
    return [
        click.Option(
            ["--gcp-project-id"],
            type=str,
            envvar="GRAM_GCP_PROJECT_ID",
            help="Google Cloud project ID",
        ),
        click.Option(
            ["--pubsub-emulator-host"],
            type=str,
            envvar="PUBSUB_EMULATOR_HOST",
            help="Host to use for the PubSub emulator",
        ),
    ]
