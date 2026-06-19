import structlog
from structlog.testing import capture_logs

from gram.risk.v1 import presidio_request_pb2
from gram_infra.pubsub.subscriber import MessageMetadata
from pystreams.risk.handler import PresidioHandler, Recognized


class _Result:
    """Minimal stand-in for a Presidio ``RecognizerResult``."""

    def __init__(self, entity_type: str):
        self.entity_type = entity_type


class FakeAnalyzer:
    """Records calls and returns canned detections keyed by input text.

    Lets the handler tests run without loading Presidio's NLP model and keeps
    detection results deterministic.
    """

    def __init__(self, detections: dict[str, list[str]] | None = None):
        self.detections = detections or {}
        self.calls: list[tuple[str, list[str] | None]] = []

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> list[Recognized]:
        self.calls.append((text, entities))
        assert language == "en"
        return [_Result(t) for t in self.detections.get(text, [])]


def _meta(delivery_attempt: int = 1) -> MessageMetadata:
    return MessageMetadata(id="m-1", attributes={}, delivery_attempt=delivery_attempt)


def _handler(analyzer: FakeAnalyzer) -> PresidioHandler:
    return PresidioHandler(structlog.get_logger(), analyzer=analyzer)


async def test_logs_when_entities_detected():
    analyzer = FakeAnalyzer(
        {
            "email me at a@b.com": ["EMAIL_ADDRESS"],
            "call 555-0100": ["PHONE_NUMBER", "PHONE_NUMBER"],
        }
    )
    handler = _handler(analyzer)
    msg = presidio_request_pb2.PresidioRequest(
        request_id="req-1",
        reply_urn="urn:reply:1",
        contents=["email me at a@b.com", "call 555-0100"],
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta(delivery_attempt=2))

    (entry,) = logs
    assert entry["event"] == "presidio scan detected entities"
    assert entry["request_id"] == "req-1"
    assert entry["reply_urn"] == "urn:reply:1"
    # Types are reported sorted and de-duplicated; the count is total hits.
    assert entry["detected_entity_types"] == ["EMAIL_ADDRESS", "PHONE_NUMBER"]
    assert entry["detected_count"] == 3
    assert entry["delivery_attempt"] == 2


async def test_no_log_when_nothing_detected():
    handler = _handler(FakeAnalyzer())  # detects nothing
    msg = presidio_request_pb2.PresidioRequest(
        request_id="req-1",
        reply_urn="urn:reply:1",
        contents=["nothing sensitive here"],
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    assert logs == []


async def test_requested_entities_forwarded_to_analyzer():
    analyzer = FakeAnalyzer({"a@b.com": ["EMAIL_ADDRESS"]})
    handler = _handler(analyzer)
    msg = presidio_request_pb2.PresidioRequest(
        request_id="req-1",
        contents=["a@b.com"],
        entities=["EMAIL_ADDRESS", "PHONE_NUMBER"],
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    # The explicit request set is passed through to the analyzer verbatim...
    assert analyzer.calls == [("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"])]
    # ...and echoed back on the log line.
    (entry,) = logs
    assert entry["requested_entities"] == ["EMAIL_ADDRESS", "PHONE_NUMBER"]


async def test_empty_entities_means_scan_all():
    analyzer = FakeAnalyzer({"a@b.com": ["EMAIL_ADDRESS"]})
    handler = _handler(analyzer)
    msg = presidio_request_pb2.PresidioRequest(request_id="req-1", contents=["a@b.com"])

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

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
    handler = PresidioHandler(structlog.get_logger(), analyzer=_BoomAnalyzer())
    secret = "my ssn is 123-45-6789"
    msg = presidio_request_pb2.PresidioRequest(
        request_id="req-1", reply_urn="urn:reply:1", contents=[secret]
    )

    # A scan failure must not propagate: raising here would nack the message and,
    # with no dead-letter policy on the subscription, poison it via redelivery.
    with capture_logs() as logs:
        await handler.handle(msg, _meta(delivery_attempt=3))

    (entry,) = logs
    assert entry["event"] == "presidio scan failed"
    assert entry["request_id"] == "req-1"
    assert entry["error_type"] == "RuntimeError"
    assert entry["delivery_attempt"] == 3
    # The error must not leak the scanned content even though the exception
    # carried it.
    assert secret not in repr(entry)
    assert "123-45-6789" not in repr(entry)


async def test_does_not_leak_content_or_values():
    secret = "my ssn is 123-45-6789"
    analyzer = FakeAnalyzer({secret: ["US_SSN"]})
    handler = _handler(analyzer)
    msg = presidio_request_pb2.PresidioRequest(
        request_id="req-1", reply_urn="urn:reply:1", contents=[secret]
    )

    with capture_logs() as logs:
        await handler.handle(msg, _meta())

    # The scanned content must never appear anywhere in the emitted log.
    (entry,) = logs
    assert secret not in repr(entry)
    assert "123-45-6789" not in repr(entry)
