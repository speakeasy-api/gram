import re
import uuid

import structlog
from gram.risk.v1 import finding_pb2, presidio_analysis_pb2
from gram_infra.pubsub.subscriber import MessageMetadata
from structlog.testing import capture_logs

from pystreams.risk.handler import PresidioHandler
from pystreams.risk.scanner import Detection

# Matches the RFC3339 UTC form the handler stamps on a finding's created_at.
_RFC3339_UTC = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z")


def _detection(
    entity_type: str,
    match: str,
    *,
    start_pos: int = 0,
    end_pos: int = 0,
    confidence: float = 0.5,
) -> Detection:
    return Detection(
        entity_type=entity_type,
        match=match,
        start_pos=start_pos,
        end_pos=end_pos,
        confidence=confidence,
    )


class FakeScanner:
    """Returns canned detections (or raises), and records its scan calls.

    Stands in for a real :class:`~pystreams.risk.scanner.Scanner` so the handler
    tests exercise finding-building, logging, and publishing without loading
    Presidio's NLP model.
    """

    def __init__(
        self,
        detections: list[Detection] | None = None,
        *,
        error: Exception | None = None,
    ):
        self._detections = detections or []
        self._error = error
        self.calls: list[tuple[str, list[str] | None]] = []

    async def scan(self, content: str, entities: list[str] | None) -> list[Detection]:
        self.calls.append((content, entities))
        if self._error is not None:
            raise self._error
        return list(self._detections)

    async def aclose(self) -> None:
        return None


class _FakeResult:
    """A PublishResult whose ``get`` returns a canned message id."""

    def __init__(self, message_id: str):
        self._id = message_id

    async def get(self) -> str:
        return self._id


class FakePublisher:
    """Captures published findings instead of sending them to Pub/Sub."""

    def __init__(self):
        self.published: list[finding_pb2.Finding] = []

    def publish(self, message: finding_pb2.Finding) -> _FakeResult:
        self.published.append(message)
        return _FakeResult(f"id-{len(self.published)}")


def _meta(delivery_attempt: int = 1) -> MessageMetadata:
    return MessageMetadata(id="m-1", attributes={}, delivery_attempt=delivery_attempt)


def _handler(
    scanner: FakeScanner, publisher: FakePublisher | None = None
) -> PresidioHandler:
    return PresidioHandler(
        structlog.get_logger(),
        publisher or FakePublisher(),
        scanner,
    )


def _message(content: str, **kwargs) -> presidio_analysis_pb2.PresidioAnalysis:
    return presidio_analysis_pb2.PresidioAnalysis(content=content, **kwargs)


async def test_publishes_a_finding_per_detection():
    content = "email me at a@b.com"
    scanner = FakeScanner(
        [
            _detection(
                "EMAIL_ADDRESS", "a@b.com", start_pos=12, end_pos=19, confidence=0.85
            )
        ]
    )
    publisher = FakePublisher()
    handler = _handler(scanner, publisher)
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
    # Detection fields are mapped onto the finding; rule_id is derived here.
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
    scanner = FakeScanner(
        [
            _detection("PHONE_NUMBER", "555-0100", start_pos=5, end_pos=13),
            _detection("PHONE_NUMBER", "555-0199", start_pos=17, end_pos=25),
        ]
    )
    publisher = FakePublisher()
    handler = _handler(scanner, publisher)

    with capture_logs() as logs:
        await handler.handle(_message("...", request_id="req-1"), _meta())

    # One finding per detection...
    assert [f.match for f in publisher.published] == ["555-0100", "555-0199"]
    # ...each with its own distinct id.
    assert len({f.id for f in publisher.published}) == 2
    # ...but the log de-duplicates entity types and counts total hits.
    (entry,) = logs
    assert entry["detected_entity_types"] == ["PHONE_NUMBER"]
    assert entry["detected_count"] == 2
    assert entry["published_count"] == 2


async def test_no_log_or_publish_when_nothing_detected():
    scanner = FakeScanner([])  # scanner finds nothing
    publisher = FakePublisher()
    handler = _handler(scanner, publisher)

    with capture_logs() as logs:
        await handler.handle(_message("nothing sensitive here"), _meta())

    assert logs == []
    assert publisher.published == []


async def test_requested_entities_forwarded_to_scanner():
    scanner = FakeScanner([_detection("EMAIL_ADDRESS", "a@b.com")])
    handler = _handler(scanner)
    msg = _message(
        "a@b.com", request_id="req-1", entities=["EMAIL_ADDRESS", "PHONE_NUMBER"]
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    # The explicit request set is passed through to the scanner verbatim...
    assert scanner.calls == [("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"])]
    # ...and echoed back on the log line.
    (entry,) = logs
    assert entry["requested_entities"] == ["EMAIL_ADDRESS", "PHONE_NUMBER"]


async def test_empty_entities_means_scan_all():
    scanner = FakeScanner([_detection("EMAIL_ADDRESS", "a@b.com")])
    handler = _handler(scanner)

    with capture_logs() as logs:
        await handler.handle(_message("a@b.com", request_id="req-1"), _meta())

    # No entities requested -> None, which tells the scanner to scan every type.
    assert scanner.calls == [("a@b.com", None)]
    (entry,) = logs
    assert entry["requested_entities"] == []


async def test_scan_failure_is_swallowed_and_logged():
    secret = "my ssn is 123-45-6789"
    # The error carries the content to prove it isn't logged.
    scanner = FakeScanner(error=RuntimeError(secret))
    publisher = FakePublisher()
    handler = _handler(scanner, publisher)
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
    # Nothing is published when the scan failed.
    assert publisher.published == []
    # The error must not leak the scanned content even though the exception
    # carried it.
    assert secret not in repr(entry)
    assert "123-45-6789" not in repr(entry)


class _BoomResult:
    """A PublishResult whose ``get`` raises, simulating a Pub/Sub commit failure."""

    def __init__(self, match: str):
        self._match = match

    async def get(self) -> str:
        # The match carries the value, to prove it isn't logged on failure.
        raise RuntimeError(self._match)


class _BoomPublisher:
    """Publisher whose publishes all fail when their result is awaited."""

    def publish(self, message: finding_pb2.Finding) -> _BoomResult:
        return _BoomResult(message.match)


async def test_publish_failure_is_swallowed_and_logged():
    secret = "a@b.com"
    scanner = FakeScanner([_detection("EMAIL_ADDRESS", secret)])
    handler = PresidioHandler(structlog.get_logger(), _BoomPublisher(), scanner)

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


class _SyncBoomPublisher:
    """Publisher whose ``publish`` raises synchronously (a misconfigured client)."""

    def publish(self, message: finding_pb2.Finding) -> _FakeResult:
        # The match carries the value, to prove a sync failure doesn't leak it.
        raise RuntimeError(message.match)


async def test_sync_publish_failure_is_swallowed_and_logged():
    secret = "a@b.com"
    scanner = FakeScanner([_detection("EMAIL_ADDRESS", secret)])
    handler = PresidioHandler(structlog.get_logger(), _SyncBoomPublisher(), scanner)

    # A synchronous publish failure must be treated like an async one: logged as a
    # publish failure and skipped, never escaping to nack/redeliver the message
    # (which would duplicate findings) and never mislabeled as a scan failure.
    with capture_logs() as logs:
        await handler.handle(_message(secret, request_id="req-1"), _meta())

    publish_errors = [e for e in logs if e["event"] == "failed to publish risk finding"]
    (err,) = publish_errors
    assert err["rule_id"] == "pii.email_address"
    assert err["error_type"] == "RuntimeError"
    assert secret not in repr(err)
    # Not reported as a scan failure...
    assert not [e for e in logs if e["event"] == "presidio scan failed"]
    # ...and the summary still lands, reporting zero published.
    summary = next(e for e in logs if e["event"] == "presidio scan detected entities")
    assert summary["published_count"] == 0


async def test_does_not_leak_content_or_values_to_logs():
    secret = "my ssn is 123-45-6789"
    scanner = FakeScanner(
        [_detection("US_SSN", "123-45-6789", start_pos=10, end_pos=21, confidence=0.99)]
    )
    publisher = FakePublisher()
    handler = _handler(scanner, publisher)
    msg = _message(secret, request_id="req-1", reply_urn="urn:reply:1")

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    # The matched value is carried on the published finding...
    (finding,) = publisher.published
    assert finding.match == "123-45-6789"
    # ...but the scanned content and matched value must never appear in the logs.
    for entry in logs:
        assert secret not in repr(entry)
        assert "123-45-6789" not in repr(entry)
