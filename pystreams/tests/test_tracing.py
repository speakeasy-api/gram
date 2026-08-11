import pytest
import structlog
from gram.ping.v2 import ping_pb2, processor_pb2
from gram.risk.v1 import finding_pb2, presidio_analysis_pb2
from gram_infra.pubsub import PublishResult
from gram_infra.pubsub.subscriber import MessageMetadata
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import (
    InMemorySpanExporter,
)
from opentelemetry.trace import StatusCode

from pystreams import attr
from pystreams.deps import tracing
from pystreams.risk.handler import PresidioHandler
from pystreams.risk.scanner import Detection, _AsyncCloseable

TOPIC = ping_pb2.Message.DESCRIPTOR.full_name
SUBSCRIPTION = processor_pb2.PyProcessor.DESCRIPTOR.full_name

# The global TracerProvider can only be set once per process, so install one
# in-memory exporter at import time and clear it between tests. The module-level
# tracer resolves the provider lazily per span, so this still captures spans the
# wrapper opens.
_EXPORTER = InMemorySpanExporter()
_PROVIDER = TracerProvider()
_PROVIDER.add_span_processor(SimpleSpanProcessor(_EXPORTER))
trace.set_tracer_provider(_PROVIDER)


@pytest.fixture
def exporter() -> InMemorySpanExporter:
    _EXPORTER.clear()
    return _EXPORTER


def _meta() -> MessageMetadata:
    return MessageMetadata(id="m-1", attributes={}, delivery_attempt=1)


async def test_traced_records_ok_span(exporter: InMemorySpanExporter):
    seen: list[ping_pb2.Message] = []

    async def handler(message: ping_pb2.Message, meta: MessageMetadata) -> None:
        seen.append(message)

    wrapped = tracing.traced(
        handler, topic_proto_name=TOPIC, subscription_proto_name=SUBSCRIPTION
    )

    msg = ping_pb2.Message(id="ping-1")
    await wrapped(msg, _meta())

    assert seen == [msg]

    (span,) = exporter.get_finished_spans()
    assert span.name == "stream.handleMessage"
    assert span.attributes is not None
    assert span.attributes[attr.TOPIC_PROTO_NAME] == TOPIC
    assert span.attributes[attr.SUBSCRIPTION_PROTO_NAME] == SUBSCRIPTION
    assert span.status.status_code == StatusCode.UNSET


async def test_traced_continues_trace_from_attributes(
    exporter: InMemorySpanExporter,
):
    # A W3C traceparent the publisher would have stamped onto the message
    # attributes: version-00, the trace id, the parent span id, sampled flag.
    trace_id = "0af7651916cd43dd8448eb211c80319c"
    parent_span_id = "b7ad6b7169203331"
    attributes = {
        "traceparent": f"00-{trace_id}-{parent_span_id}-01",
    }

    async def handler(message: ping_pb2.Message, meta: MessageMetadata) -> None:
        pass

    wrapped = tracing.traced(
        handler, topic_proto_name=TOPIC, subscription_proto_name=SUBSCRIPTION
    )

    await wrapped(
        ping_pb2.Message(id="ping-1"),
        MessageMetadata(id="m-1", attributes=attributes, delivery_attempt=1),
    )

    (span,) = exporter.get_finished_spans()
    ctx = span.get_span_context()
    assert ctx is not None
    # Same trace as the producer, and parented to the propagated span.
    assert format(ctx.trace_id, "032x") == trace_id
    assert span.parent is not None
    assert format(span.parent.span_id, "016x") == parent_span_id


async def test_traced_starts_new_trace_without_attributes(
    exporter: InMemorySpanExporter,
):
    async def handler(message: ping_pb2.Message, meta: MessageMetadata) -> None:
        pass

    wrapped = tracing.traced(
        handler, topic_proto_name=TOPIC, subscription_proto_name=SUBSCRIPTION
    )

    # No traceparent on the message: the span roots its own trace.
    await wrapped(ping_pb2.Message(id="ping-1"), _meta())

    (span,) = exporter.get_finished_spans()
    assert span.parent is None


class _CleanScanner(_AsyncCloseable):
    """Scanner stand-in that always finds nothing (the publisher stays unused)."""

    async def scan(
        self, content: str, entities: list[str] | None, score_threshold: float
    ) -> list[Detection]:
        return []

    async def aclose(self) -> None:
        return None


class _UnusedPublisher:
    """Fails the test if the handler publishes — the clean path never should."""

    def publish(self, message: finding_pb2.Finding) -> PublishResult:
        raise AssertionError("no findings expected on the clean path")


async def test_presidio_handler_stamps_content_size_on_delivery_span(
    exporter: InMemorySpanExporter,
):
    # The handler runs inside the traced-receiver span (start_as_current_span),
    # so its content-size attribute must land on that delivery span — the exact
    # per-message value trace analytics correlates against duration.
    handler = PresidioHandler(
        structlog.get_logger(), _UnusedPublisher(), _CleanScanner()
    )
    wrapped = tracing.traced(
        handler.handle,
        topic_proto_name=presidio_analysis_pb2.PresidioAnalysis.DESCRIPTOR.full_name,
        subscription_proto_name=SUBSCRIPTION,
    )

    content = "nothing sensitive in here"
    await wrapped(presidio_analysis_pb2.PresidioAnalysis(content=content), _meta())

    (span,) = exporter.get_finished_spans()
    assert span.attributes is not None
    assert span.attributes[attr.RISK_CONTENT_CHARS] == len(content)


async def test_traced_marks_error_and_reraises(exporter: InMemorySpanExporter):
    boom = ValueError("handler failed")

    async def handler(message: ping_pb2.Message, meta: MessageMetadata) -> None:
        raise boom

    wrapped = tracing.traced(
        handler, topic_proto_name=TOPIC, subscription_proto_name=SUBSCRIPTION
    )

    with pytest.raises(ValueError, match="handler failed"):
        await wrapped(ping_pb2.Message(id="ping-1"), _meta())

    (span,) = exporter.get_finished_spans()
    assert span.status.status_code == StatusCode.ERROR
    assert span.status.description == "handler failed"
    assert any(event.name == "exception" for event in span.events)
