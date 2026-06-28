"""Publisher attribute-building tests (no live broker).

Covers the outgoing attribute set built by ``_message_attributes``: the
content-type/schema markers and the trace-context injection that lets a
subscriber continue the producer's trace. The injection mirrors
``messageAttributes`` in ``infra/pkg/gcp/publisher.go``; the round-trip uses the
same global propagator the receiver extracts with (``deps/tracing.py``).
"""

from __future__ import annotations

from opentelemetry import context, propagate, trace

from gram_infra.pubsub.publisher import CONTENT_TYPE, _message_attributes

SCHEMA = "gram.ping.v2.Message"


def test_message_attributes_always_carries_content_type_and_schema() -> None:
    attrs = _message_attributes(SCHEMA)

    assert attrs["content-type"] == CONTENT_TYPE
    assert attrs["schema"] == SCHEMA


def test_message_attributes_omits_trace_context_without_active_span() -> None:
    attrs = _message_attributes(SCHEMA)

    # No span in context: the propagator injects nothing, so unpropagated
    # messages still let the subscriber start a fresh trace.
    assert "traceparent" not in attrs


def test_message_attributes_round_trips_trace_context_to_subscriber() -> None:
    # A known span context, as if some upstream request started the trace.
    parent_ctx = trace.set_span_in_context(
        trace.NonRecordingSpan(
            trace.SpanContext(
                trace_id=0x0AF7651916CD43DD8448EB211C80319C,
                span_id=0xB7AD6B7169203331,
                is_remote=True,
                trace_flags=trace.TraceFlags(trace.TraceFlags.SAMPLED),
            )
        )
    )

    # Publisher side: inject the active trace context into the attributes. The
    # global propagate.inject reads the current context, so attach the parent
    # for the duration of the call.
    token = context.attach(parent_ctx)
    try:
        attrs = _message_attributes(SCHEMA)
    finally:
        context.detach(token)

    assert "traceparent" in attrs, "an active span should be propagated"

    # Subscriber side (mirrors deps/tracing.py): extract from the attributes.
    extracted = propagate.extract(attrs)
    got = trace.get_current_span(extracted).get_span_context()

    assert got.trace_id == 0x0AF7651916CD43DD8448EB211C80319C
    assert got.span_id == 0xB7AD6B7169203331
    assert got.trace_flags.sampled
