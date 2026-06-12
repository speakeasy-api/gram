"""Name-resolution parity tests.

The kebab-cased names Python derives MUST equal the names the Go layer derives
(and bakes into infra/gen/kcc.yaml), or a Python publisher and a Go subscriber
will silently target different resources. These assertions pin the contract.

to_kebab mirrors github.com/ettle/strcase (used by the Go layer). The cases
below cover the proto names in use today plus a few casing edges. If a future
topic or subscription uses acronym-heavy or otherwise unusual casing, add a case
here so the Python and Go name derivations stay identical — divergence is silent
(messages just go to the wrong resource), so this test is the only guard.
"""

from __future__ import annotations

import pytest
from gcp.pubsub.v1 import options_pb2
from gram.ping.v1 import ping_pb2, processor_pb2
from gram_infra.pubsub import (
    resolve_dead_letter_topic_name,
    resolve_subscription_name,
    resolve_topic_name,
    subscription_options_from_message,
    to_kebab,
    topic_options_from_message,
)


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("gram.ping.v1.Message", "gram-ping-v1-message"),
        ("gram.ping.v1.Processor", "gram-ping-v1-processor"),
        ("gram.outbox.v1.Event", "gram-outbox-v1-event"),
        ("fooBar", "foo-bar"),
        ("HTTPServer", "http-server"),
        ("already-kebab", "already-kebab"),
        # Number-internal '.'/',' between two digits is preserved, not split:
        # ettle treats it as part of a version/number, so "events.v2.5" keeps the
        # ".5" but still splits the "events." delimiter.
        ("events.v2.5", "events-v2.5"),
        ("v4.3", "v4.3"),
        ("x.2.3.y", "x-2.3-y"),
        ("a.b.2,000.3.c", "a-b-2,000.3-c"),
        ("v1beta2", "v1beta2"),
        # Acronym / digit-boundary edges.
        ("OAuth2", "o-auth2"),
        ("iOS", "i-os"),
        ("Foo2Bar", "foo2-bar"),
        # A trailing separator becomes a trailing delimiter; a leading one is
        # dropped (no word precedes it).
        ("trailing_", "trailing-"),
        ("_leading", "leading"),
        # Non-ASCII letters are lower-cased and kept, not dropped.
        ("héllo", "héllo"),
        ("MixedÜmlautCase", "mixed-ümlaut-case"),
    ],
)
def test_to_kebab_matches_go(value: str, expected: str) -> None:
    assert to_kebab(value) == expected


def test_resolve_topic_name_from_descriptor() -> None:
    descriptor = ping_pb2.Message.DESCRIPTOR
    options = topic_options_from_message(descriptor)
    assert options is not None
    assert resolve_topic_name(descriptor, options) == "gram-ping-v1-message"


def test_resolve_subscription_name_from_descriptor() -> None:
    descriptor = processor_pb2.Processor.DESCRIPTOR
    options = subscription_options_from_message(descriptor)
    assert options is not None
    assert resolve_subscription_name(descriptor, options) == "gram-ping-v1-processor"


def test_dead_letter_name_defaults_to_suffix() -> None:
    options = subscription_options_from_message(processor_pb2.Processor.DESCRIPTOR)
    assert options is not None
    assert (
        resolve_dead_letter_topic_name("gram-ping-v1-processor", options.dead_letter)
        == "gram-ping-v1-processor-dlq"
    )


def test_dead_letter_name_honors_explicit_name() -> None:
    dead_letter = options_pb2.DeadLetterPolicy(name="My DLQ Topic")
    assert resolve_dead_letter_topic_name("ignored", dead_letter) == "my-dlq-topic"


def test_explicit_topic_name_overrides_full_name() -> None:
    descriptor = ping_pb2.Message.DESCRIPTOR
    options = options_pb2.TopicOptions(name="custom.Override")
    assert resolve_topic_name(descriptor, options) == "custom-override"
