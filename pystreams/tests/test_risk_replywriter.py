"""Tests for enforcement reply delivery to Redis inboxes."""

import fakeredis.aioredis
import pytest
from gram.risk.v1 import enforcement_reply_pb2

from pystreams.risk.replywriter import (
    InvalidReplyURNError,
    ReplyWriter,
    inbox_key,
    parse_reply_urn,
)

_REPLY_URN = "urn:gram:risk:enforce:replica-1:0198f1f4-0000-7000-8000-000000000000"
_SCAN_ID = "0198f1f4-0000-7000-8000-000000000000"


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


async def test_writer_delivers_reply_to_replica_inbox_with_ttl():
    client = fakeredis.aioredis.FakeRedis()
    reply = enforcement_reply_pb2.EnforcementReply(
        correlation_id=_SCAN_ID,
        scanner=enforcement_reply_pb2.ENFORCEMENT_SCANNER_PRESIDIO,
        status=enforcement_reply_pb2.ENFORCEMENT_STATUS_OK,
    )
    await ReplyWriter(client).write(_REPLY_URN, reply)

    # TTL is set in the same pipeline as the push; -1 (no expiry) must fail.
    # Checked before reading, since popping the only element deletes the key.
    assert 0 < await client.ttl(inbox_key("replica-1")) <= 60
    raw = await client.lpop(inbox_key("replica-1"))
    assert isinstance(raw, bytes)
    stored = enforcement_reply_pb2.EnforcementReply()
    stored.ParseFromString(raw)
    assert stored.correlation_id == _SCAN_ID
    assert stored.scanner == enforcement_reply_pb2.ENFORCEMENT_SCANNER_PRESIDIO
    assert stored.status == enforcement_reply_pb2.ENFORCEMENT_STATUS_OK


async def test_writer_rejects_correlation_id_mismatch():
    client = fakeredis.aioredis.FakeRedis()
    writer = ReplyWriter(client)
    reply = enforcement_reply_pb2.EnforcementReply(correlation_id="other")
    with pytest.raises(InvalidReplyURNError):
        await writer.write(_REPLY_URN, reply)
