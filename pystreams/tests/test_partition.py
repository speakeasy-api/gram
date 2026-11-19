"""The partition option and its closed set of values.

The set mirrors ``server/internal/partition`` in the Go server. The golden
values below must change together with ``partition.All`` there, so the two
services never disagree about which partitions exist.
"""

import click
import pytest
from click.testing import CliRunner

from pystreams import partition
from pystreams.cmd.flags_service import service_options


@pytest.fixture
def parse():
    """Parse argv/env through a command carrying only the service options."""

    def _parse(args: list[str], env: dict[str, str] | None = None) -> dict:
        captured: dict = {}

        def _record(**kwargs) -> None:
            captured.update(kwargs)

        command = click.Command(
            "probe", params=list(service_options()), callback=_record
        )
        CliRunner().invoke(
            command, args, env=env, standalone_mode=False, catch_exceptions=False
        )
        return captured

    return _parse


def test_partitions_match_go_server():
    # Golden values from server/internal/partition (partition.All, Default).
    assert partition.PARTITIONS == ("us", "eu")
    assert partition.DEFAULT == "us"


def test_partition_defaults_to_us(parse):
    assert parse([])["partition"] == "us"


def test_partition_reads_environment_variable(parse):
    assert parse([], env={"GRAM_PARTITION": "eu"})["partition"] == "eu"


def test_empty_partition_environment_variable_is_rejected(parse):
    # Matches the Go server, which rejects a set-but-empty GRAM_PARTITION.
    with pytest.raises(click.BadParameter, match="set but empty"):
        parse([], env={"GRAM_PARTITION": ""})


@pytest.mark.parametrize("bad", ["apac", "US", ""])
def test_unknown_partition_is_rejected(parse, bad):
    with pytest.raises(click.UsageError):
        parse(["--partition", bad])
