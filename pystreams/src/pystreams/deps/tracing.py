"""Per-message tracing for stream handlers.

Mirrors the Go streams command (``server/cmd/gram/streams.go``), which opens a
``stream.handleMessage`` span around every delivered message before invoking the
handler. The span carries the topic and subscription proto names; a handler that
raises (i.e. nacks the message) has its error recorded and the span marked
``ERROR`` before it ends, so a failed delivery is visible as a failed span.

This lives in the command layer rather than in ``gram_infra.pubsub`` for the
same reason it does in Go: the Pub/Sub library stays tracing-agnostic and the
consumer that wires up handlers owns the span.
"""

from __future__ import annotations

from google.protobuf.message import Message
from gram_infra.pubsub.subscriber import MessageCallback, MessageMetadata
from opentelemetry import propagate, trace
from opentelemetry.trace import StatusCode

from pystreams import attr

_tracer = trace.get_tracer("github.com/speakeasy-api/gram/pystreams")


def traced[M: Message](
    callback: MessageCallback[M],
    *,
    topic_proto_name: str,
    subscription_proto_name: str,
) -> MessageCallback[M]:
    """Wrap a message callback so each invocation runs inside its own span.

    Returning from ``callback`` acks the message and the span ends ``OK``;
    raising nacks it and the span records the exception and is marked ``ERROR``,
    then the exception propagates unchanged so the subscriber's nack path runs.
    """

    async def wrapper(message: M, meta: MessageMetadata) -> None:
        # Continue the producer's trace: extract any W3C trace context the
        # publisher propagated through the message attributes, so this span is a
        # child of the publishing span instead of the root of a fresh trace. The
        # message attributes act as the textmap carrier; ``extract`` uses the
        # globally configured propagator (W3C tracecontext + baggage by default)
        # and quietly yields an empty context when no trace headers are present,
        # so unpropagated messages still start a new trace.
        parent = propagate.extract(meta.attributes)
        # Disable the context manager's automatic exception handling and do it
        # explicitly so the span's status description carries the error message,
        # matching the Go receiver's ``span.SetStatus(codes.Error, err.Error())``.
        with _tracer.start_as_current_span(
            "stream.handleMessage",
            context=parent,
            attributes={
                attr.TOPIC_PROTO_NAME: topic_proto_name,
                attr.SUBSCRIPTION_PROTO_NAME: subscription_proto_name,
            },
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            try:
                await callback(message, meta)
            except BaseException as exc:
                span.record_exception(exc)
                span.set_status(StatusCode.ERROR, str(exc))
                raise

    return wrapper
