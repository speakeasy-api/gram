from datetime import UTC, datetime
from typing import Protocol

import structlog
from google.protobuf.message import DecodeError
from gram.metering.v1 import meter_reading_pb2
from gram_infra.pubsub import PublishResult

_logger = structlog.get_logger()


class MeterReadingPublisher(Protocol):
    """Publisher surface shared by the Presidio handlers."""

    def publish(self, message: meter_reading_pb2.MeterReading) -> PublishResult: ...


async def publish_meter_reading(
    publisher: MeterReadingPublisher,
    serialized_template: bytes,
    scan_started_at: datetime,
    *,
    request_id: str,
    reply_urn: str,
    delivery_attempt: int | None,
) -> None:
    """Publish without changing the reading's stable identity or provenance."""
    reading = meter_reading_pb2.MeterReading()
    try:
        reading.ParseFromString(serialized_template)
    except DecodeError as exc:
        _logger.warning(
            "discard malformed presidio meter reading",
            request_id=request_id,
            reply_urn=reply_urn,
            delivery_attempt=delivery_attempt,
            error_type=type(exc).__name__,
        )
        return
    reading.occurred_at = _utc_timestamp(scan_started_at)
    reading.produced_at = _utc_timestamp(datetime.now(UTC))
    await publisher.publish(reading).get()


def _utc_timestamp(value: datetime) -> str:
    return (
        value.astimezone(UTC).isoformat(timespec="microseconds").replace("+00:00", "Z")
    )
