import logging
import os
from typing import Any

from opentelemetry import trace
from .. import attr
import structlog
from structlog.typing import EventDict, Processor, WrappedLogger


def _add_open_telemetry_spans(_, __, event_dict):
    span = trace.get_current_span()
    ctx = span.get_span_context()
    if not ctx.is_valid:
        return event_dict

    span_id = format(ctx.span_id, "016x")
    trace_id = format(ctx.trace_id, "032x")

    event_dict[attr.SPAN_ID] = span_id
    event_dict[attr.TRACE_ID] = trace_id

    if os.environ.get("DD_SERVICE"):
        event_dict[attr.DATADOG_SPAN_ID] = span_id
        event_dict[attr.DATADOG_TRACE_ID] = trace_id

    return event_dict


def _rename_event_to_message(_, __, event_dict):
    """Rename the ``event`` key to ``message``, when present.

    Unlike ``structlog.processors.EventRenamer``, this guards on the key being
    present so log calls without an event (e.g. ``log.info(None, foo="bar")``)
    don't raise a ``KeyError``.
    """
    if "event" in event_dict:
        event_dict["message"] = event_dict.pop("event")
    return event_dict


def _add_base_attrs(base_attrs: dict[str, Any]):
    """Build a processor that merges fixed base attributes into every event."""

    def processor(
        _logger: WrappedLogger, _method_name: str, event_dict: EventDict
    ) -> EventDict:
        for key, value in base_attrs.items():
            if value is not None:
                event_dict.setdefault(key, value)
        return event_dict

    return processor


def configure_logging(
    *, pretty_log: bool, log_level: str, base_attrs: dict[str, Any] | None = None
):
    """Configure structlog for the application.

    Args:
      pretty_log: When True, emit human-friendly colored console output.
        Otherwise emit structured JSON suitable for log aggregation.
      log_level: Minimum level to emit (e.g. "info", "debug", "warning").
      base_attrs: Optional attributes bound to every log line (e.g. service
        name, version). Per-event keys of the same name take precedence.
    """
    level = logging.getLevelNamesMapping().get(log_level.upper(), logging.INFO)

    shared_processors: list[Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.StackInfoRenderer(),
        structlog.processors.TimeStamper(fmt="iso", utc=True),
    ]

    if base_attrs:
        shared_processors.insert(0, _add_base_attrs(base_attrs))

    shared_processors.insert(0, _add_open_telemetry_spans)

    if pretty_log:
        renderer = structlog.dev.ConsoleRenderer()
    else:
        shared_processors.append(structlog.processors.format_exc_info)
        shared_processors.append(_rename_event_to_message)
        renderer = structlog.processors.JSONRenderer()

    structlog.configure(
        processors=[*shared_processors, renderer],
        wrapper_class=structlog.make_filtering_bound_logger(level),
        logger_factory=structlog.PrintLoggerFactory(),
        cache_logger_on_first_use=True,
    )
