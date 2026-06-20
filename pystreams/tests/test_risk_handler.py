import re
import uuid

import structlog
from gram.risk.v1 import finding_pb2, presidio_analysis_pb2
from gram_infra.pubsub.subscriber import MessageMetadata
from structlog.testing import capture_logs

from pystreams.risk.handler import PresidioHandler, Recognized

# Matches the RFC3339 UTC form the handler stamps on a finding's created_at.
_RFC3339_UTC = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z")


class _Result:
    """Minimal stand-in for a Presidio ``RecognizerResult``."""

    def __init__(
        self, entity_type: str, start: int = 0, end: int = 0, score: float = 0.5
    ):
        self.entity_type = entity_type
        self.start = start
        self.end = end
        self.score = score


class FakeAnalyzer:
    """Records calls and returns canned detections keyed by input text.

    Lets the handler tests run without loading Presidio's NLP model and keeps
    detection results deterministic.
    """

    def __init__(self, detections: dict[str, list[Recognized]] | None = None):
        self.detections = detections or {}
        self.calls: list[tuple[str, list[str] | None]] = []

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> list[Recognized]:
        self.calls.append((text, entities))
        assert language == "en"
        return self.detections.get(text, [])


class FakePublisher:
    """Captures published findings instead of sending them to Pub/Sub."""

    def __init__(self):
        self.published: list[finding_pb2.Finding] = []

    async def publish(self, message: finding_pb2.Finding) -> str:
        self.published.append(message)
        return f"id-{len(self.published)}"


def _meta(delivery_attempt: int = 1) -> MessageMetadata:
    return MessageMetadata(id="m-1", attributes={}, delivery_attempt=delivery_attempt)


def _handler(
    analyzer: FakeAnalyzer, publisher: FakePublisher | None = None
) -> PresidioHandler:
    return PresidioHandler(
        structlog.get_logger(),
        publisher or FakePublisher(),
        analyzer=analyzer,
    )


def _message(content: str, **kwargs) -> presidio_analysis_pb2.PresidioAnalysis:
    return presidio_analysis_pb2.PresidioAnalysis(content=content, **kwargs)


async def test_publishes_a_finding_per_detection():
    content = "email me at a@b.com"
    analyzer = FakeAnalyzer(
        {content: [_Result("EMAIL_ADDRESS", start=12, end=19, score=0.85)]}
    )
    publisher = FakePublisher()
    handler = _handler(analyzer, publisher)
    msg = _message(
        content,
        request_id="req-1",
        chat_message_id="chat-1",
        project_id="proj-1",
        organization_id="org-1",
        risk_policy_id="policy-1",
        risk_policy_version=7,
        created_at="2023-01-01T00:00:00Z",
        reply_urn="urn:reply:1",
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta(delivery_attempt=2))

    (finding,) = publisher.published
    # Each finding gets a fresh UUIDv7 identifier.
    assert uuid.UUID(finding.id).version == 7
    # Originating request/message context is carried through verbatim.
    assert finding.request_id == "req-1"
    assert finding.chat_message_id == "chat-1"
    assert finding.project_id == "proj-1"
    assert finding.organization_id == "org-1"
    assert finding.risk_policy_id == "policy-1"
    assert finding.risk_policy_version == 7
    # created_at is a fresh detection timestamp, not the request's created_at.
    assert _RFC3339_UTC.fullmatch(finding.created_at)
    assert finding.created_at != "2023-01-01T00:00:00Z"
    # Detection fields are mapped onto the finding.
    assert finding.source == "presidio"
    assert finding.rule_id == "pii.email_address"
    assert finding.match == "a@b.com"
    assert finding.start_pos == 12
    assert finding.end_pos == 19
    assert finding.confidence == 0.85
    assert list(finding.tags) == ["pii"]

    (entry,) = logs
    assert entry["event"] == "presidio scan detected entities"
    assert entry["request_id"] == "req-1"
    assert entry["reply_urn"] == "urn:reply:1"
    assert entry["detected_entity_types"] == ["EMAIL_ADDRESS"]
    assert entry["detected_count"] == 1
    assert entry["published_count"] == 1
    assert entry["delivery_attempt"] == 2


async def test_publishes_one_finding_per_hit_and_dedupes_log_types():
    content = "call 555-0100 or 555-0199"
    analyzer = FakeAnalyzer(
        {
            content: [
                _Result("PHONE_NUMBER", start=5, end=13),
                _Result("PHONE_NUMBER", start=17, end=25),
            ]
        }
    )
    publisher = FakePublisher()
    handler = _handler(analyzer, publisher)

    with capture_logs() as logs:
        await handler.handle(_message(content, request_id="req-1"), _meta())

    # One finding per recognized span...
    assert [f.match for f in publisher.published] == ["555-0100", "555-0199"]
    # ...each with its own distinct id.
    assert len({f.id for f in publisher.published}) == 2
    # ...but the log de-duplicates entity types and counts total hits.
    (entry,) = logs
    assert entry["detected_entity_types"] == ["PHONE_NUMBER"]
    assert entry["detected_count"] == 2
    assert entry["published_count"] == 2


async def test_byte_offsets_for_multibyte_content():
    # "café " is 5 characters but 6 UTF-8 bytes; a match after it must be
    # reported in byte positions, not character positions.
    content = "café a@b.com"
    analyzer = FakeAnalyzer(
        {content: [_Result("EMAIL_ADDRESS", start=5, end=12, score=0.9)]}
    )
    publisher = FakePublisher()
    handler = _handler(analyzer, publisher)

    await handler.handle(_message(content, request_id="req-1"), _meta())

    (finding,) = publisher.published
    assert finding.match == "a@b.com"
    assert finding.start_pos == 6  # one extra byte from the 'é'
    assert finding.end_pos == 13


async def test_false_positives_are_filtered_before_publishing():
    # "10.0.0.1" is RFC1918 space and is dropped; "a@b.com" is a real match.
    content = "10.0.0.1 and a@b.com"
    analyzer = FakeAnalyzer(
        {
            content: [
                _Result("IP_ADDRESS", start=0, end=8),
                _Result("EMAIL_ADDRESS", start=13, end=20, score=0.7),
            ]
        }
    )
    publisher = FakePublisher()
    handler = _handler(analyzer, publisher)

    with capture_logs() as logs:
        await handler.handle(_message(content, request_id="req-1"), _meta())

    # Only the real match is published; the reserved IP never reaches the topic.
    (finding,) = publisher.published
    assert finding.rule_id == "pii.email_address"
    assert finding.match == "a@b.com"
    # The log counts reflect the post-filter set.
    (entry,) = logs
    assert entry["detected_entity_types"] == ["EMAIL_ADDRESS"]
    assert entry["detected_count"] == 1
    assert entry["published_count"] == 1


async def test_all_false_positives_means_no_publish_and_no_log():
    content = "reach me at user@example.com"
    analyzer = FakeAnalyzer({content: [_Result("EMAIL_ADDRESS", start=12, end=28)]})
    publisher = FakePublisher()
    handler = _handler(analyzer, publisher)

    with capture_logs() as logs:
        await handler.handle(_message(content, request_id="req-1"), _meta())

    # example.com is a placeholder domain: nothing published, nothing logged.
    assert publisher.published == []
    assert logs == []


async def test_no_log_or_publish_when_nothing_detected():
    publisher = FakePublisher()
    handler = _handler(FakeAnalyzer(), publisher)  # detects nothing

    with capture_logs() as logs:
        await handler.handle(_message("nothing sensitive here"), _meta())

    assert logs == []
    assert publisher.published == []


async def test_requested_entities_forwarded_to_analyzer():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})
    handler = _handler(analyzer)
    msg = _message(
        "a@b.com", request_id="req-1", entities=["EMAIL_ADDRESS", "PHONE_NUMBER"]
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    # The explicit request set is passed through to the analyzer verbatim...
    assert analyzer.calls == [("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"])]
    # ...and echoed back on the log line.
    (entry,) = logs
    assert entry["requested_entities"] == ["EMAIL_ADDRESS", "PHONE_NUMBER"]


async def test_empty_entities_means_scan_all():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})
    handler = _handler(analyzer)

    with capture_logs() as logs:
        await handler.handle(_message("a@b.com", request_id="req-1"), _meta())

    # No entities requested -> None, which tells Presidio to scan every type.
    assert analyzer.calls == [("a@b.com", None)]
    (entry,) = logs
    assert entry["requested_entities"] == []


class _BoomAnalyzer:
    """Analyzer whose ``analyze`` always raises, simulating a Presidio failure."""

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> list[Recognized]:
        raise RuntimeError(text)  # message carries content to prove it isn't logged


async def test_scan_failure_is_swallowed_and_logged():
    publisher = FakePublisher()
    handler = PresidioHandler(
        structlog.get_logger(), publisher, analyzer=_BoomAnalyzer()
    )
    secret = "my ssn is 123-45-6789"
    msg = _message(secret, request_id="req-1", reply_urn="urn:reply:1")

    # A scan failure must not propagate: raising here would nack the message and,
    # with no dead-letter policy on the subscription, poison it via redelivery.
    with capture_logs() as logs:
        await handler.handle(msg, _meta(delivery_attempt=3))

    (entry,) = logs
    assert entry["event"] == "presidio scan failed"
    assert entry["request_id"] == "req-1"
    assert entry["error_type"] == "RuntimeError"
    assert entry["delivery_attempt"] == 3
    # Nothing is published when the scan never produced results.
    assert publisher.published == []
    # The error must not leak the scanned content even though the exception
    # carried it.
    assert secret not in repr(entry)
    assert "123-45-6789" not in repr(entry)


class _BoomPublisher:
    """Publisher whose ``publish`` always raises, simulating a Pub/Sub failure."""

    async def publish(self, message: finding_pb2.Finding) -> str:
        raise RuntimeError(message.match)  # carries the value to prove it isn't logged


async def test_publish_failure_is_swallowed_and_logged():
    secret = "a@b.com"
    analyzer = FakeAnalyzer({secret: [_Result("EMAIL_ADDRESS", start=0, end=7)]})
    handler = PresidioHandler(
        structlog.get_logger(), _BoomPublisher(), analyzer=analyzer
    )

    # A publish failure must not propagate either: nacking would redeliver and
    # re-publish any findings that already landed, duplicating them.
    with capture_logs() as logs:
        await handler.handle(_message(secret, request_id="req-1"), _meta())

    publish_errors = [e for e in logs if e["event"] == "failed to publish risk finding"]
    (err,) = publish_errors
    assert err["request_id"] == "req-1"
    assert err["rule_id"] == "pii.email_address"
    assert err["error_type"] == "RuntimeError"
    # The matched value must not leak into the log even though the error carried it.
    assert secret not in repr(err)
    # The summary line still reports zero published.
    summary = next(e for e in logs if e["event"] == "presidio scan detected entities")
    assert summary["published_count"] == 0


async def test_does_not_leak_content_or_values_to_logs():
    secret = "my ssn is 123-45-6789"
    analyzer = FakeAnalyzer({secret: [_Result("US_SSN", start=10, end=21, score=0.99)]})
    publisher = FakePublisher()
    handler = _handler(analyzer, publisher)
    msg = _message(secret, request_id="req-1", reply_urn="urn:reply:1")

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    # The matched value is carried on the published finding...
    (finding,) = publisher.published
    assert finding.match == "123-45-6789"
    # ...but the scanned content must never appear anywhere in the emitted logs.
    for entry in logs:
        assert secret not in repr(entry)
        assert "123-45-6789" not in repr(entry)
