from collections.abc import Sequence

import click


def enforce_options() -> Sequence[click.Option]:
    """Options for the Presidio enforcement lane (reply inbox + fingerprints).

    In the default ``all`` role, the lane activates only when both the Redis
    address and pepper keyring are set. The dedicated ``enforcement`` role
    requires both. The Redis password is independently optional.
    """
    return [
        click.Option(
            ["--redis-addr"],
            type=str,
            envvar="GRAM_REDIS_CACHE_ADDR",
            help="Redis address (host:port) for enforcement reply inboxes.",
        ),
        click.Option(
            ["--redis-password"],
            type=str,
            envvar="GRAM_REDIS_CACHE_PASSWORD",
            help="Redis password for enforcement reply inboxes.",
        ),
        click.Option(
            ["--redis-next-addr"],
            type=str,
            envvar="GRAM_REDIS_NEXT_ADDR",
            help="Redis address (host:port) the process moves to when the "
            "switch key on the current instance says so. Unset disables the switch.",
        ),
        click.Option(
            ["--redis-next-password"],
            type=str,
            envvar="GRAM_REDIS_NEXT_PASSWORD",
            help="Redis password for the next instance.",
        ),
        click.Option(
            ["--risk-fingerprint-pepper-keyring"],
            type=str,
            envvar="GRAM_RISK_FINGERPRINT_PEPPER_KEYRING",
            help="JSON pepper keyring for tenant-scoped finding fingerprints.",
        ),
    ]
