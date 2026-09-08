"""Enforcement reply delivery to replica-scoped Redis inboxes.

Python counterpart of ``server/internal/redisinbox/writer.go``: the reply
URN names the replica inbox and the scan id, the reply proto must carry the
same scan id, and the write is one MULTI/EXEC pipeline of RPUSH plus the inbox
TTL so a reply can never land without refreshing the key's expiry. Keep the URN
grammar, key layout, and TTL in lockstep with the Go package.
"""

from __future__ import annotations

from typing import Final

from gram.risk.v1 import enforcement_reply_pb2
from redis import asyncio as redis_asyncio

REPLY_URN_PREFIX: Final = "urn:gram:risk:enforce:"
# Every write refreshes this expiry, so with a stalled drainer the loss
# boundary is 60s after the LAST write; alert on risk.enforcement.drainer_alive
# rather than key expiry.
REPLY_TTL_SECONDS: Final = 60


class InvalidReplyURNError(ValueError):
    """The reply URN does not match the enforcement return-address grammar."""


def parse_reply_urn(value: str) -> tuple[str, str]:
    """Extract (replica_id, correlation_id); raises when malformed."""
    if not value.startswith(REPLY_URN_PREFIX):
        raise InvalidReplyURNError("invalid enforcement reply urn")
    replica_id, sep, scan_id = value[len(REPLY_URN_PREFIX) :].partition(":")
    if not sep or not _valid_replica_id(replica_id) or not scan_id or ":" in scan_id:
        raise InvalidReplyURNError("invalid enforcement reply urn")
    return replica_id, scan_id


def _valid_replica_id(value: str) -> bool:
    # Mirrors validReplicaID in the Go inbox: [A-Za-z0-9._-]+ only.
    return bool(value) and all(
        c.isascii() and (c.isalnum() or c in "-_.") for c in value
    )


def inbox_key(replica_id: str) -> str:
    return "enforce:reply:" + replica_id


class ReplyWriter:
    """Appends enforcement replies to the replica inbox named by the reply URN."""

    def __init__(self, client: redis_asyncio.Redis) -> None:
        self._client = client

    async def write(
        self, reply_urn: str, reply: enforcement_reply_pb2.EnforcementReply
    ) -> None:
        replica_id, scan_id = parse_reply_urn(reply_urn)
        if reply.correlation_id != scan_id:
            raise InvalidReplyURNError(
                "reply scan id does not match return address scan id"
            )
        payload = reply.SerializeToString()
        key = inbox_key(replica_id)
        async with self._client.pipeline(transaction=True) as pipe:
            pipe.rpush(key, payload)
            pipe.expire(key, REPLY_TTL_SECONDS)
            await pipe.execute()
