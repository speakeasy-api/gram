"""Resolve Pub/Sub topology from protobuf message options.

This is the Python counterpart of the option-extraction and name-resolution
helpers in ``infra/internal/gcp/pubsub_discover.go``. It reads the
``(gcp.pubsub.v1.topic)`` and ``(gcp.pubsub.v1.subscription)`` message options
declared on marker messages and derives the topic / subscription / dead-letter
names exactly as the Go layer does, so a Python publisher and a Go subscriber
(or vice versa) agree on names.

Only the runtime subset is ported here: extracting options and resolving names.
The full topology validation / kcc.yaml generation stays in Go.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any, NamedTuple, Union, cast

from google.protobuf.descriptor import Descriptor
from google.protobuf.message import Message

# Importing the generated options module registers the ``topic`` and
# ``subscription`` extensions in the default descriptor pool, which is what makes
# them readable off a message's options below.
from gcp.pubsub.v1 import options_pb2

if TYPE_CHECKING:
    # types-protobuf types ``type[Message].DESCRIPTOR`` as a union of the upb (C)
    # and pure-Python descriptor classes, so accept both anywhere a message's
    # descriptor is passed in. Resolved only by the type checker.
    from google._upb._message import Descriptor as _CDescriptor

    MessageDescriptor = Union[Descriptor, _CDescriptor]
else:
    MessageDescriptor = Descriptor

__all__ = [
    "DLQ_SUFFIX",
    "topic_options_from_message",
    "subscription_options_from_message",
    "require_topic_options",
    "require_subscription_options",
    "require_subscription_for_message",
    "SubscriptionBinding",
    "resolve_topic_name",
    "resolve_subscription_name",
    "resolve_dead_letter_topic_name",
    "to_kebab",
]

# Suffix appended to a subscription name to derive its dead-letter topic when no
# explicit name is given. Mirrors ``dlqSuffix`` in pubsub_discover.go.
DLQ_SUFFIX = "-dlq"


def _message_options_extension(descriptor: MessageDescriptor, extension: Any):
    """Read a MessageOptions extension value off a descriptor, or None if unset.

    The generated extension objects (``options_pb2.topic`` / ``.subscription``)
    are typed as plain ``FieldDescriptor`` in protobuf's stubs, but
    ``HasExtension`` / ``Extensions[]`` are typed against
    ``_ExtensionFieldDescriptor``. The ``Any`` parameter bridges that stub gap;
    at runtime this is exactly how protobuf exposes message-option extensions.
    """
    options = descriptor.GetOptions()
    if options is None or not options.HasExtension(extension):
        return None
    return options.Extensions[extension]


def topic_options_from_message(
    descriptor: MessageDescriptor,
) -> options_pb2.TopicOptions | None:
    """Return the TopicOptions declared on a message, or None if it declares no topic."""
    return cast(
        "options_pb2.TopicOptions | None",
        _message_options_extension(descriptor, options_pb2.topic),
    )


def subscription_options_from_message(
    descriptor: MessageDescriptor,
) -> options_pb2.SubscriptionOptions | None:
    """Return the SubscriptionOptions declared on a message, or None if it declares no subscription."""
    return cast(
        "options_pb2.SubscriptionOptions | None",
        _message_options_extension(descriptor, options_pb2.subscription),
    )


def require_topic_options(
    message_type: type[Message],
) -> tuple[MessageDescriptor, options_pb2.TopicOptions]:
    """Return ``(descriptor, TopicOptions)`` for a message type, raising if it declares no topic."""
    descriptor = message_type.DESCRIPTOR
    options = topic_options_from_message(descriptor)
    if options is None:
        raise ValueError(
            f"proto message {descriptor.full_name} does not declare a pubsub topic"
        )
    return descriptor, options


def require_subscription_options(
    subscription_type: type[Message],
) -> tuple[MessageDescriptor, options_pb2.SubscriptionOptions]:
    """Return ``(descriptor, SubscriptionOptions)`` for a marker type, raising if it declares no subscription."""
    descriptor = subscription_type.DESCRIPTOR
    options = subscription_options_from_message(descriptor)
    if options is None:
        raise ValueError(
            f"proto message {descriptor.full_name} does not declare a pubsub subscription"
        )
    return descriptor, options


class SubscriptionBinding(NamedTuple):
    """A validated (topic message, subscription marker) pairing and its options."""

    message_descriptor: MessageDescriptor
    topic_options: options_pb2.TopicOptions
    subscription_descriptor: MessageDescriptor
    subscription_options: options_pb2.SubscriptionOptions


def require_subscription_for_message(
    message_type: type[Message], subscription_type: type[Message]
) -> SubscriptionBinding:
    """Validate a (message, subscription) pair and return their descriptors + options.

    Ensures the message declares a topic, the marker declares a subscription, and
    the subscription's declared ``topic`` (a proto full name) matches the message
    type being consumed — so a mismatched pair fails fast here instead of
    silently consuming from (or, for the emulator, reconciling against) the wrong
    topic. Every broker routes subscriber resolution through this single check.
    """
    msg_descriptor, topic_options = require_topic_options(message_type)
    sub_descriptor, sub_options = require_subscription_options(subscription_type)

    declared_topic = sub_options.topic.strip()
    message_full_name = msg_descriptor.full_name
    if declared_topic != message_full_name:
        raise ValueError(
            f"subscription {sub_descriptor.full_name} declares topic "
            f"{declared_topic!r} but was given message type {message_full_name!r}"
        )

    return SubscriptionBinding(
        msg_descriptor, topic_options, sub_descriptor, sub_options
    )


def _require_ascii(name: str, context: str) -> None:
    """Reject non-ASCII resource names before kebab-casing.

    ``to_kebab`` mirrors Go's ettle/strcase only over ASCII (fuzz-verified);
    outside ASCII the two diverge on numeral classes, exotic whitespace, and
    one-to-many lowercasings (e.g. ``İ``), so a non-ASCII explicit name would
    make a Python publisher and a Go subscriber silently resolve different
    resource IDs — and GCP resource IDs may not contain non-ASCII characters
    anyway. Derived names are proto full names, which are always ASCII, so this
    only ever fires on an explicit ``name`` declaration.
    """
    if not name.isascii():
        raise ValueError(
            f"pubsub resource name {name!r} ({context}) must be ASCII: non-ASCII "
            "names are not valid GCP resource IDs and break naming parity with "
            "the Go layer"
        )


def _resolve_name(descriptor: MessageDescriptor, explicit: str) -> str:
    """Resolve a resource ID: the explicit name when set, else the kebab-cased proto full name."""
    name = (explicit or "").strip()
    if not name:
        name = descriptor.full_name
    _require_ascii(name, f"declared on {descriptor.full_name}")
    return to_kebab(name)


def resolve_topic_name(
    descriptor: MessageDescriptor, options: options_pb2.TopicOptions
) -> str:
    """Resolve a topic ID: the explicit name when set, else the kebab-cased proto full name."""
    return _resolve_name(descriptor, options.name)


def resolve_subscription_name(
    descriptor: MessageDescriptor, options: options_pb2.SubscriptionOptions
) -> str:
    """Resolve a subscription ID: the explicit name when set, else the kebab-cased proto full name."""
    return _resolve_name(descriptor, options.name)


def resolve_dead_letter_topic_name(
    subscription_name: str, dead_letter: options_pb2.DeadLetterPolicy
) -> str:
    """Resolve a DLQ topic ID: the kebab-cased explicit name when set, else ``<sub>-dlq``."""
    explicit = (dead_letter.name or "").strip()
    if not explicit:
        return subscription_name + DLQ_SUFFIX
    _require_ascii(explicit, f"dead-letter topic for {subscription_name!r}")
    return to_kebab(explicit)


# Sentinel rune used in place of Go's zero rune for the "no previous / no next
# character" positions at the string boundaries. Empty string makes every
# is*()/lower() probe below return False / itself, matching how Go's split
# function treats the 0 rune (not upper, lower, number, space, '.', '_' or '-').
_NONE = ""


def _is_split_skip(prev: str, curr: str, nxt: str) -> tuple[bool, bool]:
    """Port of ettle/strcase's ``defaultSplitFn`` for the kebab/snake delimiters.

    Returns ``(split, skip)`` for the character ``curr`` given its neighbours:
    ``split`` means start a new word before ``curr`` (a delimiter is emitted but
    ``curr`` is kept), ``skip`` means ``curr`` is itself a delimiter and is
    dropped. Both false means continue the current word. Mirrors the Go function
    rule-for-rule, including the digit-``.``/``,``-digit number-internal
    exception (``v4.3`` stays ``v4.3``) and treating ``.`` as a delimiter only
    once the numeric exceptions have been ruled out.
    """
    # Lower-case letters are always part of the current word (the hot path).
    if curr.islower():
        return False, False
    # Default delimiters are '_', '-' and unicode spaces. '.' is handled lower
    # down so the numeric exceptions get a chance to claim it first.
    if curr == "_" or curr == "-" or curr.isspace():
        return False, True

    if curr.isupper():
        if prev.islower():
            # fooBar -> foo|Bar
            return True, False
        if prev.isupper() and nxt.islower():
            # FOOBar -> FOO|Bar
            return True, False

    # Numeric exceptions come last to avoid a perf penalty on the common path.
    if prev.isnumeric():
        # v4.3 / 2,000 are not split: a '.' or ',' between two digits is kept.
        if (curr == "." or curr == ",") and nxt.isnumeric():
            return False, False
        if not curr.isnumeric() and curr != ".":
            return True, False
    # '.' is a delimiter, but only once it failed to be a number separator.
    if curr == ".":
        return False, True

    return False, False


def to_kebab(value: str) -> str:
    """Lower-case kebab-case a string, mirroring ettle/strcase ToKebab.

    Used to derive Pub/Sub resource IDs from proto full names, e.g.
    ``gram.ping.v1.Message`` -> ``gram-ping-v1-message``. Must stay byte-for-byte
    compatible with the Go layer or the two languages will resolve different
    names; ``tests/test_naming.py`` guards this against the known proto names.

    This is a direct port of ``convertWithoutInitialisms(s, '-', LowerCase)``
    streaming over runes, rather than a regex word-splitter, because ettle's
    boundary rules are inherently contextual (they depend on the previous and
    next rune, e.g. acronyms and the digit-``.``-digit number exception) and a
    regex cannot reproduce them faithfully. Non-ASCII letters are lower-cased and
    kept, matching Go's unicode-aware behavior.

    A leading delimiter is dropped (no word precedes it) while a trailing one is
    emitted, since the trailing word's delimiter is written before the run ends
    (``trailing_`` -> ``trailing-``).
    """
    runes = value.strip()
    if not runes:
        return ""

    out: list[str] = []
    in_word = False
    n = len(runes)
    for i in range(n):
        curr = runes[i]
        prev = runes[i - 1] if i > 0 else _NONE
        nxt = runes[i + 1] if i + 1 < n else _NONE

        split, skip = _is_split_skip(prev, curr, nxt)
        if skip:
            if in_word:
                out.append("-")
            in_word = False
            continue
        if split:
            if in_word:
                out.append("-")
            in_word = False
        out.append(curr.lower())
        in_word = True

    return "".join(out)
