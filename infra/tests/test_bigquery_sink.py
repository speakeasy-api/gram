"""Tests for BigQuery export sinks.

A subscription marker carrying the `bigquery` option is an export-only sink: it
writes its topic's messages into a BigQuery table and is not consumable in code.
require_subscription_for_message is the single chokepoint every broker (and both
public subscriber helpers) route through, so rejecting a sink there covers them
all.
"""

from __future__ import annotations

import pytest
from gram.risk.v1 import finding_pb2, finding_sink_pb2

from gram_infra.pubsub.discover import (
    require_subscription_for_message,
    subscription_options_from_message,
)


def test_finding_sink_declares_bigquery() -> None:
    opts = subscription_options_from_message(finding_sink_pb2.FindingSink.DESCRIPTOR)
    assert opts is not None
    assert opts.HasField("bigquery")
    assert opts.topic == "gram.risk.v1.Finding"


def test_bigquery_sink_is_not_consumable() -> None:
    with pytest.raises(ValueError, match="BigQuery export sink"):
        require_subscription_for_message(
            finding_pb2.Finding, finding_sink_pb2.FindingSink
        )
