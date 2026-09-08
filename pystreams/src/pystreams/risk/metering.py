from datetime import UTC, datetime
from typing import Protocol

from gram.metering.v1 import meter_reading_pb2
from gram_infra.pubsub import PublishResult


class MeterReadingPublisher(Protocol):
    """Publisher surface shared by the Presidio handlers."""

    def publish(self, message: meter_reading_pb2.MeterReading) -> PublishResult: ...


async def publish_meter_reading(
    publisher: MeterReadingPublisher,
    serialized_template: bytes,
    scan_started_at: datetime,
) -> None:
    """Publish without changing the reading's stable identity or provenance."""
    reading = meter_reading_pb2.MeterReading()
    reading.ParseFromString(serialized_template)
    reading.occurred_at = _utc_timestamp(scan_started_at)
    reading.produced_at = _utc_timestamp(datetime.now(UTC))
    await publisher.publish(reading).get()


def _utc_timestamp(value: datetime) -> str:
    return (
        value.astimezone(UTC).isoformat(timespec="microseconds").replace("+00:00", "Z")
    )
