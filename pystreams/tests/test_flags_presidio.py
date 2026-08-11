"""Parse-time validation of the presidio timeout flags.

NaN is the dangerous input: ``float("nan")`` parses fine and fails every
comparison, so without the ``_finite_float`` callback it would slip past the
scanners' ``<= 0`` disable checks and silently turn a timeout off — reverting
queued scans to the unbounded waits the slot timeout exists to prevent.
"""

import click
import pytest
from click.testing import CliRunner

from pystreams.cmd.flags_presidio import presidio_options


@pytest.fixture
def parse():
    """Parse argv/env through a command carrying only the presidio options.

    Returns the parsed params dict on success; raises ``UsageError`` (via
    ``standalone_mode=False``) so tests assert on the failure directly instead
    of on exit codes.
    """

    def _parse(args: list[str], env: dict[str, str] | None = None) -> dict:
        captured: dict = {}

        def _record(**kwargs) -> None:
            captured.update(kwargs)

        command = click.Command(
            "probe", params=list(presidio_options()), callback=_record
        )
        CliRunner().invoke(
            command, args, env=env, standalone_mode=False, catch_exceptions=False
        )
        return captured

    return _parse


@pytest.mark.parametrize("flag", ["--scan-timeout", "--scan-slot-timeout"])
@pytest.mark.parametrize("bad", ["nan", "inf", "-inf"])
def test_non_finite_timeouts_are_rejected_at_parse(parse, flag, bad):
    with pytest.raises(click.UsageError, match="finite"):
        parse([flag, bad])


def test_non_finite_timeout_from_env_is_rejected(parse):
    # The env var path goes through the same conversion + callback as the flag.
    with pytest.raises(click.UsageError, match="finite"):
        parse([], env={"GRAM_PYSTREAMS_SCAN_SLOT_TIMEOUT": "nan"})


def test_zero_still_means_disabled(parse):
    # <=0 is the documented disable switch and must keep parsing cleanly.
    params = parse(["--scan-timeout", "0", "--scan-slot-timeout", "-1"])

    assert params["scan_timeout"] == 0.0
    assert params["scan_slot_timeout"] == -1.0


def test_finite_timeouts_pass_through(parse):
    params = parse(["--scan-timeout", "120.5", "--scan-slot-timeout", "30"])

    assert params["scan_timeout"] == 120.5
    assert params["scan_slot_timeout"] == 30.0
