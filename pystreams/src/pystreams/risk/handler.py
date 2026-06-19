from typing import Protocol

import structlog
from asyncer import asyncify
from gram.risk.v1 import presidio_request_pb2
from gram_infra.pubsub.subscriber import MessageMetadata
from presidio_analyzer import AnalyzerEngine
from presidio_analyzer.nlp_engine import NlpEngineProvider

# The spaCy model bundled into the image (pinned in pystreams/pyproject.toml).
# Presidio's default AnalyzerEngine() would also load this model, but selecting
# it explicitly ties the handler to the model we actually ship and stops a
# future Presidio default change from silently reaching for a model we don't
# package.
SPACY_MODEL = "en_core_web_lg"


def _build_analyzer() -> AnalyzerEngine:
    """Construct an AnalyzerEngine backed by the explicitly selected spaCy model."""
    nlp_engine = NlpEngineProvider(
        nlp_configuration={
            "nlp_engine_name": "spacy",
            "models": [{"lang_code": "en", "model_name": SPACY_MODEL}],
        }
    ).create_engine()
    return AnalyzerEngine(nlp_engine=nlp_engine)


class Recognized(Protocol):
    entity_type: str


class Analyzer(Protocol):
    """The slice of ``AnalyzerEngine`` this handler depends on.

    Narrowing to a protocol keeps the engine injectable — tests can supply a
    lightweight fake instead of loading Presidio's NLP model.
    """

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> list[Recognized]: ...


class PresidioHandler:
    """Scans :class:`PresidioRequest` payloads for PII using Presidio.

    This is intentionally a stub: detection results are not forwarded anywhere
    yet because no downstream consumer exists. The handler only emits an opaque
    log line when something is detected, so the scan is observable without ever
    leaking the matched text or surrounding content.
    """

    def __init__(
        self,
        logger: structlog.stdlib.BoundLogger,
        analyzer: Analyzer | None = None,
    ):
        self.logger = logger
        # Constructing the engine loads the NLP model, so build it once and
        # reuse it across messages rather than per delivery.
        self.analyzer = analyzer or _build_analyzer()

    async def handle(
        self,
        message: presidio_request_pb2.PresidioRequest,
        meta: MessageMetadata,
    ) -> None:
        # An empty list means "analyze every entity Presidio knows about".
        requested = list(message.entities) or None

        # Presidio's analyzer is synchronous and CPU-bound; run it off the event
        # loop so a large request can't stall other subscriptions.
        try:
            detected = await asyncify(self._scan)(
                contents=list(message.contents),
                entities=requested,
            )
        except Exception as exc:
            # This is best-effort shadow processing, and the PresidioScanner
            # subscription declares no dead-letter policy. Letting a scan failure
            # escape would nack the message, and any input that deterministically
            # trips the analyzer would then redeliver and fail again for the full
            # retention window (30 days) — one bad message poisoning the
            # subscription. Swallow it (so the message is acked) and log instead.
            # Cancellation derives from BaseException, not Exception, so graceful
            # shutdown still propagates. Only the exception *type* is logged: an
            # error string or traceback could echo the scanned content, which this
            # handler never emits (see the detection log below).
            self.logger.error(
                "presidio scan failed",
                request_id=message.request_id,
                reply_urn=message.reply_urn,
                requested_entities=requested or [],
                error_type=type(exc).__name__,
                delivery_attempt=meta.delivery_attempt,
            )
            return

        if not detected:
            return

        # Log entity *types* and counts only — never the matched values or the
        # scanned content — so the line is safe to retain while still being
        # traceable back to the originating request.
        self.logger.info(
            "presidio scan detected entities",
            request_id=message.request_id,
            reply_urn=message.reply_urn,
            requested_entities=requested or [],
            detected_entity_types=sorted(detected),
            detected_count=sum(detected.values()),
            delivery_attempt=meta.delivery_attempt,
        )

    def _scan(self, contents: list[str], entities: list[str] | None) -> dict[str, int]:
        """Analyze each content string, returning a count per entity type."""
        counts: dict[str, int] = {}
        for content in contents:
            for result in self.analyzer.analyze(
                text=content, entities=entities, language="en"
            ):
                counts[result.entity_type] = counts.get(result.entity_type, 0) + 1
        return counts
