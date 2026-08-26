"""Tests for the Presidio enforcement lane: fingerprints, masks, writer, handler."""

import json
from datetime import UTC, datetime, timedelta
from typing import cast

import fakeredis.aioredis
import pytest
import structlog
from gram.risk.v1 import enforcement_reply_pb2, presidio_enforcement_pb2
from gram_infra.pubsub.subscriber import MessageMetadata

import pystreams.risk.enforce_handler as enforce_handler_mod
from pystreams.risk import maskdisplay
from pystreams.risk.enforce_handler import (
    MAX_CONTENT_BYTES,
    MalformedEnforcementRequest,
    PresidioEnforceHandler,
)
from pystreams.risk.fingerprint import (
    Fingerprinter,
    FingerprintKeyringError,
    encode_fingerprint,
    parse_pepper_keyring,
)
from pystreams.risk.replywriter import (
    InvalidReplyURNError,
    ReplyWriter,
    inbox_key,
    parse_reply_urn,
)
from pystreams.risk.scanner import Detection, ScanSlotTimeout

# Keyring shared with the Go golden-vector run (see the fingerprint parity test).
_KEYRING = json.dumps(
    {
        "current": "v1",
        "keys": {"v1": "c3ludGhldGljLWZpbmdlcnByaW50LWtleS1tYXRlcmlhbA=="},
    }
)

_REPLY_URN = "urn:gram:risk:enforce:replica-1:0198f1f4-0000-7000-8000-000000000000"
_SCAN_ID = "0198f1f4-0000-7000-8000-000000000000"


class FakeScanner:
    def __init__(
        self,
        detections: list[Detection] | None = None,
        error: Exception | None = None,
    ) -> None:
        self.detections = detections or []
        self.error = error
        self.calls: list[str] = []

    async def scan(
        self,
        content: str,
        entities: list[str] | None,
        score_threshold: float,
    ) -> list[Detection]:
        self.calls.append(content)
        if self.error is not None:
            raise self.error
        return self.detections

    async def aclose(self) -> None:
        return None

    async def __aenter__(self) -> FakeScanner:
        return self

    async def __aexit__(self, *exc_info: object) -> None:
        return None


def _message(
    *,
    content: str = "email jane.doe@example.com",
    created_at: str | None = None,
    organization_id: str = "org-123",
) -> presidio_enforcement_pb2.PresidioEnforcement:
    return presidio_enforcement_pb2.PresidioEnforcement(
        request_id="req-1",
        project_id="proj-1",
        organization_id=organization_id,
        created_at=created_at or datetime.now(UTC).isoformat(),
        content=content,
    )


def _meta(reply_urn: str = _REPLY_URN) -> MessageMetadata:
    return MessageMetadata(
        id="m1",
        attributes={enforce_handler_mod.REPLY_URN_ATTRIBUTE: reply_urn},
        delivery_attempt=1,
    )


def _handler(
    scanner: FakeScanner, client: fakeredis.aioredis.FakeRedis
) -> PresidioEnforceHandler:
    return PresidioEnforceHandler(
        structlog.get_logger(),
        ReplyWriter(client),
        scanner,
        parse_pepper_keyring(_KEYRING),
    )


async def _read_reply(
    client: fakeredis.aioredis.FakeRedis,
) -> enforcement_reply_pb2.EnforcementReply:
    raw = await client.lpop(inbox_key("replica-1"))
    assert isinstance(raw, bytes)
    reply = enforcement_reply_pb2.EnforcementReply()
    reply.ParseFromString(raw)
    return reply


def test_fingerprint_matches_go_golden_vectors():
    fp = parse_pepper_keyring(_KEYRING)
    sum1, version = fp.tenanted_hs256("org-123", b"jane.doe@example.com")
    assert version == "v1"
    # Golden values from server/internal/risk (TenantedHS256 + EncodeFingerprint).
    assert encode_fingerprint(sum1) == "OtttmK1tiaZmS8oK2PAT-n3vNa4iic0SQh6RpOY5_yo"
    sum2, _ = fp.tenanted_hs256("org-456", b"jane.doe@example.com")
    assert encode_fingerprint(sum2) == "sPaNo7W-wULGODbRJGEiypFFzKfzTAbMsQ77l_KBi54"


def test_parse_pepper_keyring_rejects_bad_input():
    with pytest.raises(FingerprintKeyringError):
        parse_pepper_keyring("not json")
    with pytest.raises(FingerprintKeyringError):
        parse_pepper_keyring(json.dumps({"current": "v2", "keys": {"v1": "AAAA"}}))


def test_maskdisplay_tiers():
    assert maskdisplay.display("pii.email_address", "jane.doe@example.com") == (
        "***@example.com"
    )
    assert maskdisplay.display("pii.credit_card", "4111111111111111") == "****1111"
    assert maskdisplay.display("pii.phone_number", "+14155550123") == ("+141******23")
    assert maskdisplay.display("pii.person", "Jo") == "**"


def test_parse_reply_urn():
    assert parse_reply_urn(_REPLY_URN) == ("replica-1", _SCAN_ID)
    for bad in [
        "urn:gram:risk:enforce:replica-1",
        "urn:gram:risk:enforce::scan",
        "urn:gram:risk:enforce:bad id:scan",
        "urn:other:replica:scan",
    ]:
        with pytest.raises(InvalidReplyURNError):
            parse_reply_urn(bad)


async def test_writer_rejects_correlation_id_mismatch():
    client = fakeredis.aioredis.FakeRedis()
    writer = ReplyWriter(client)
    reply = enforcement_reply_pb2.EnforcementReply(correlation_id="other")
    with pytest.raises(InvalidReplyURNError):
        await writer.write(_REPLY_URN, reply)


async def test_handler_writes_ok_reply_with_safe_findings():
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner(
        detections=[
            Detection(
                entity_type="EMAIL_ADDRESS",
                match="jane.doe@example.com",
                start_pos=6,
                end_pos=26,
                confidence=0.9,
            )
        ]
    )
    await _handler(scanner, client).handle(_message(), _meta())

    # TTL is set alongside the push; -1 (no expiry) must fail. Checked before
    # reading, since popping the only element deletes the key.
    assert 0 < await client.ttl(inbox_key("replica-1")) <= 60
    reply = await _read_reply(client)
    assert reply.correlation_id == _SCAN_ID
    assert reply.scanner == enforcement_reply_pb2.ENFORCEMENT_SCANNER_PRESIDIO
    assert reply.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_OK
    assert len(reply.findings) == 1
    finding = reply.findings[0]
    assert finding.rule_id == "pii.email_address"
    assert finding.category == "pii"
    assert finding.masked_preview == "***@example.com"
    assert "jane.doe" not in finding.masked_preview
    assert finding.fingerprint == "OtttmK1tiaZmS8oK2PAT-n3vNa4iic0SQh6RpOY5_yo"


class FailingWriter:
    async def write(self, reply_urn: str, reply: object) -> None:
        raise ConnectionError("redis unavailable")


async def test_handler_acks_request_when_reply_write_fails():
    # A nack would send the raw content to the DLQ; the handler must swallow
    # the write failure and return so the message is acked. The waiter's
    # deadline covers the lost reply.
    handler = PresidioEnforceHandler(
        structlog.get_logger(),
        cast(ReplyWriter, FailingWriter()),
        FakeScanner(),
        parse_pepper_keyring(_KEYRING),
    )
    await handler.handle(_message(), _meta())


async def test_handler_replies_error_when_fingerprinting_fails():
    class FailingFingerprinter:
        def tenanted_hs256(self, tenant_id: str, message: bytes) -> tuple[bytes, str]:
            raise RuntimeError("hkdf failure")

    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner(
        detections=[
            Detection(
                entity_type="EMAIL_ADDRESS",
                match="jane.doe@example.com",
                start_pos=6,
                end_pos=26,
                confidence=0.9,
            )
        ]
    )
    handler = PresidioEnforceHandler(
        structlog.get_logger(),
        ReplyWriter(client),
        scanner,
        cast(Fingerprinter, FailingFingerprinter()),
    )
    await handler.handle(_message(), _meta())
    reply = await _read_reply(client)
    assert reply.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
    assert reply.reason == "fingerprint enforcement finding"
    assert len(reply.findings) == 0


async def test_handler_drops_stale_request_without_reply():
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner()
    stale = (datetime.now(UTC) - timedelta(seconds=45)).isoformat()
    await _handler(scanner, client).handle(_message(created_at=stale), _meta())
    assert scanner.calls == []
    assert await client.lpop(inbox_key("replica-1")) is None


async def test_handler_rejects_naive_created_at_as_malformed():
    naive = datetime.now(UTC).replace(tzinfo=None).isoformat()
    with pytest.raises(MalformedEnforcementRequest):
        await _handler(FakeScanner(), fakeredis.aioredis.FakeRedis()).handle(
            _message(created_at=naive), _meta()
        )


async def test_handler_raises_on_missing_tenant_for_dlq():
    client = fakeredis.aioredis.FakeRedis()
    with pytest.raises(MalformedEnforcementRequest):
        await _handler(FakeScanner(), client).handle(
            _message(organization_id=""), _meta()
        )


async def test_handler_replies_error_on_scan_failure_without_content():
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner(error=RuntimeError("boom jane.doe@example.com"))
    await _handler(scanner, client).handle(_message(), _meta())
    reply = await _read_reply(client)
    assert reply.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
    assert "jane.doe" not in reply.reason
    assert "RuntimeError" in reply.reason


async def test_handler_replies_error_on_scan_slot_timeout():
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner(error=ScanSlotTimeout("pool saturated"))
    await _handler(scanner, client).handle(_message(), _meta())
    reply = await _read_reply(client)
    assert reply.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR


async def test_handler_drops_far_future_request_without_reply():
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner()
    future = (datetime.now(UTC) + timedelta(minutes=5)).isoformat()
    await _handler(scanner, client).handle(_message(created_at=future), _meta())
    assert scanner.calls == []
    assert await client.lpop(inbox_key("replica-1")) is None


@pytest.mark.parametrize(
    "threshold", [1.5, -0.5, float("nan"), float("inf"), float("-inf")]
)
async def test_handler_rejects_out_of_range_threshold_without_scanning(
    threshold: float,
):
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner()
    message = _message()
    message.score_threshold = threshold
    await _handler(scanner, client).handle(message, _meta())
    assert scanner.calls == []
    reply = await _read_reply(client)
    assert reply.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR


async def test_handler_rejects_oversized_content_without_scanning():
    client = fakeredis.aioredis.FakeRedis()
    scanner = FakeScanner()
    big = "a" * (MAX_CONTENT_BYTES + 1)
    await _handler(scanner, client).handle(_message(content=big), _meta())
    assert scanner.calls == []
    reply = await _read_reply(client)
    assert reply.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
