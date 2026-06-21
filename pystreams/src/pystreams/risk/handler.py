import time
import uuid
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Protocol

import anyio
import structlog
from asyncer import asyncify
from gram.risk.v1 import finding_pb2, presidio_analysis_pb2
from gram_infra.pubsub import PublishResult
from gram_infra.pubsub.subscriber import MessageMetadata
from presidio_analyzer import AnalyzerEngine
from presidio_analyzer.nlp_engine import NlpEngineProvider

from pystreams.risk import presidiofp

# The spaCy model bundled into the image (pinned in pystreams/pyproject.toml).
# Presidio's default AnalyzerEngine() would also load this model, but selecting
# it explicitly ties the handler to the model we actually ship and stops a
# future Presidio default change from silently reaching for a model we don't
# package.
SPACY_MODEL = "en_core_web_lg"

# Source label stamped on every finding this handler emits, so all findings
# from the Presidio path are attributed identically downstream.
SOURCE_PRESIDIO = "presidio"


# Generic, source-agnostic description stamped on every published finding. The
# canonical rule_id carries the specific entity, so a consumer can resolve a
# richer description from it.
_FINDING_DESCRIPTION = "Identified potentially sensitive personal information."


class PresidioHandler:
    """Scans :class:`PresidioAnalysis` payloads for PII using Presidio and
    publishes each detection to the findings topic.

    Every recognized entity becomes a :class:`Finding` carrying the originating
    request/message context plus the match span, and is published to the
    ``gram.risk.v1.Finding`` topic for downstream persistence. The matched value
    is included on the published finding, but is still never written to the
    handler's own logs — log lines report entity *types* and
    counts only, so they stay safe to retain.
    """

    def __init__(
        self,
        logger: structlog.stdlib.BoundLogger,
        publisher: FindingPublisher,
        analyzer: Analyzer,
        *,
        max_scan_concurrency: int | None = 2,
    ):
        self.logger = logger
        self.publisher = publisher
        self.analyzer = analyzer
        # Ceiling on concurrent off-loop scans. Presidio's per-scan work is almost
        # entirely GIL-bound, so extra scan threads don't add parallelism — they
        # thrash the GIL and starve the event loop. A local burst sweep on a
        # 10-core box found throughput/latency peak at 2 (≈41 msg/s, scan p50
        # ~1.08s) and degrade monotonically with more: the default-40 thread pool
        # managed only ~28 msg/s at p50 ~1.55s. The optimum tracks the GIL, not the
        # core count, so 2 is a sane default everywhere; scale throughput with more
        # processes/replicas, not more threads. None (or <=0) disables the cap and
        # falls back to anyio's default thread-pool limiter (40).
        self._max_scan_concurrency = max_scan_concurrency
        self._scan_limiter: anyio.CapacityLimiter | None = None

    def _get_scan_limiter(self) -> anyio.CapacityLimiter | None:
        """Lazily build the shared scan limiter (needs a running event loop)."""
        if self._max_scan_concurrency is None or self._max_scan_concurrency <= 0:
            return None
        if self._scan_limiter is None:
            self._scan_limiter = anyio.CapacityLimiter(self._max_scan_concurrency)
        return self._scan_limiter

    async def handle(
        self,
        message: presidio_analysis_pb2.PresidioAnalysis,
        meta: MessageMetadata,
    ) -> None:
        # An empty list means "analyze every entity Presidio knows about".
        requested = list(message.entities) or None

        # One worker hop does the CPU/GIL-bound scan *and* the build + serialize +
        # client-enqueue of every finding, so neither the spaCy work nor protobuf
        # serialization runs on the event-loop thread competing with intake for the
        # GIL. Timed as wall clock — it includes any wait for a free scan slot,
        # which under load (not the scan itself) can dominate per-message ACK
        # latency.
        try:
            scan_started = time.perf_counter()
            pending = await asyncify(
                self._scan_and_publish, limiter=self._get_scan_limiter()
            )(
                message=message,
                content=message.content,
                entities=requested,
            )
            scan_ms = (time.perf_counter() - scan_started) * 1000
        except Exception as exc:
            # This is best-effort shadow processing, and the PresidioAnalyzer
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

        if not pending:
            return

        # The publishes were already fired on the worker thread (the client batches
        # them), so this wall time is just the wait for their commits — roughly one
        # round-trip regardless of count, the other half of the ACK cost.
        publish_started = time.perf_counter()
        published = await self._collect(message, pending)
        publish_ms = (time.perf_counter() - publish_started) * 1000

        # Log entity *types* and counts only — never the matched values or the
        # scanned content — so the line is safe to retain while still being
        # traceable back to the originating request.
        #
        # Emitted at debug, not info: this fires once per detection-bearing
        # message, and rendering it (JSON-encoding the event dict) runs on the
        # event-loop thread holding the GIL — the per-message loop work that
        # dominates ACK latency under burst. At the default info level the
        # filtering bound logger short-circuits ``adebug`` to a no-op before any
        # rendering, so the line costs nothing in production yet stays available
        # when debugging. The async ``adebug`` keeps that render off the event
        # loop on the occasions debug *is* enabled.
        entity_types: dict[str, int] = {}
        for p in pending:
            entity_types[p.entity_type] = entity_types.get(p.entity_type, 0) + 1
        await self.logger.adebug(
            "presidio scan detected entities",
            request_id=message.request_id,
            reply_urn=message.reply_urn,
            requested_entities=requested or [],
            detected_entity_types=sorted(entity_types),
            detected_count=len(pending),
            published_count=published,
            delivery_attempt=meta.delivery_attempt,
            # Per-message latency split, safe to retain (durations, never content):
            # the off-loop scan + finding-publish dispatch (incl. scan-slot wait)
            # vs. the wait for those publishes' commits.
            scan_ms=round(scan_ms, 1),
            publish_ms=round(publish_ms, 1),
        )

    def _scan_and_publish(
        self,
        *,
        message: presidio_analysis_pb2.PresidioAnalysis,
        content: str,
        entities: list[str] | None,
    ) -> list[_PendingPublish]:
        """Scan, then build and fire each finding's publish — all off the loop.

        Runs on a worker thread (via ``asyncify``): the analyzer call, the
        false-positive classification (which may consult the embedded ASN
        database), the byte-offset conversion, **and** each finding's proto build +
        ``SerializeToString`` + client enqueue all stay off the event-loop thread.
        Keeping the serialization here is the point — done on the loop it would be
        per-finding GIL work competing with message intake.

        A message's findings are independent, so every publish is fired up front
        (``publish`` returns a future without waiting and the client batches the
        sends); the returned futures are collected on the loop by ``_collect``. In
        series a message with N findings would pay N Pub/Sub round-trips back to
        back; firing them together collapses that toward a single round-trip.
        """
        detections = self._scan(content, entities)
        if not detections:
            return []
        # A fresh detection timestamp (UTC, RFC3339) stamped on every finding from
        # this delivery — when the scan ran, not when the request was created.
        created_at = datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
        return [
            _PendingPublish(
                entity_type=d.entity_type,
                rule_id=d.rule_id,
                result=self._publish(
                    finding_pb2.Finding(
                        # A UUIDv7 per finding: globally unique with a time-ordered
                        # prefix, so findings sort by creation in storage.
                        id=str(uuid.uuid7()),
                        request_id=message.request_id,
                        chat_message_id=message.chat_message_id,
                        project_id=message.project_id,
                        organization_id=message.organization_id,
                        risk_policy_id=message.risk_policy_id,
                        risk_policy_version=message.risk_policy_version,
                        created_at=created_at,
                        rule_id=d.rule_id,
                        description=_FINDING_DESCRIPTION,
                        match=d.match,
                        start_pos=d.start_pos,
                        end_pos=d.end_pos,
                        tags=["pii"],
                        source=SOURCE_PRESIDIO,
                        confidence=d.confidence,
                    )
                ),
            )
            for d in detections
        ]

    def _publish(self, finding: finding_pb2.Finding) -> PublishResult:
        """Fire one publish, deferring a synchronous failure to its result.

        ``publish`` normally returns a future, but a synchronous raise (e.g. a
        misconfigured client) would otherwise propagate out of ``_scan_and_publish``
        and be reported as a *scan* failure, aborting the message's remaining
        findings. Capturing it as a :class:`_FailedPublish` keeps per-finding
        failure handling uniform whether ``publish`` raises now or the commit fails
        later — both are logged and skipped by ``_collect``.
        """
        try:
            return self.publisher.publish(finding)
        except Exception as exc:
            return _FailedPublish(exc)

    async def _collect(
        self,
        message: presidio_analysis_pb2.PresidioAnalysis,
        pending: Sequence[_PendingPublish],
    ) -> int:
        """Await each already-fired publish's commit; return how many landed.

        A publish failure is logged and skipped rather than raised: nacking the
        message would redeliver it and re-publish the findings that already
        landed, duplicating them (there is no dedup downstream).
        """
        published = 0
        for p in pending:
            try:
                await p.result.get()
            except Exception as exc:
                # Never echo the finding (it carries the matched value); log the
                # request, rule id, and exception type only.
                self.logger.error(
                    "failed to publish risk finding",
                    request_id=message.request_id,
                    rule_id=p.rule_id,
                    error_type=type(exc).__name__,
                )
                continue
            published += 1

        return published

    def _scan(self, content: str, entities: list[str] | None) -> list[_Detection]:
        """Analyze the content and return the real (non-false-positive) matches.

        Runs on a worker thread (via ``asyncify``), so the analyzer call, the
        false-positive classification (which may consult the embedded ASN
        database), and the byte-offset conversion all stay off the event loop.
        """
        detections: list[_Detection] = []
        for r in self.analyzer.analyze(text=content, entities=entities, language="en"):
            start_byte, end_byte, match = _byte_span(content, r.start, r.end)
            # Drop catalog false positives (reserved/placeholder IPs and emails,
            # cloud/CDN ASN attribution) before they ever reach the topic.
            if presidiofp.reason(r.entity_type, match):
                continue
            detections.append(
                _Detection(
                    entity_type=r.entity_type,
                    rule_id=_canonical_rule_id(r.entity_type),
                    match=match,
                    start_pos=start_byte,
                    end_pos=end_byte,
                    confidence=r.score,
                )
            )
        return detections


async def build_default_analyzer() -> AnalyzerEngine:
    """Construct an AnalyzerEngine backed by the explicitly selected spaCy model."""
    provider = NlpEngineProvider(
        nlp_configuration={
            "nlp_engine_name": "spacy",
            "models": [{"lang_code": "en", "model_name": SPACY_MODEL}],
        }
    )
    nlp_engine = await asyncify(provider.create_engine)()
    return AnalyzerEngine(nlp_engine=nlp_engine)


def _canonical_rule_id(entity_type: str) -> str:
    """Map a Presidio UPPER_SNAKE entity type to the canonical rule id.

    Lowercases the entity name and prefixes it with ``pii.`` (e.g.
    ``EMAIL_ADDRESS`` -> ``pii.email_address``) so the same finding gets the same
    rule id regardless of which path produced it.
    """
    return "pii." + entity_type.lower()


def _byte_span(content: str, start: int, end: int) -> tuple[int, int, str]:
    """Clamp a Presidio character span and convert it to UTF-8 byte offsets.

    Presidio reports character (code point) offsets, but the Finding schema
    carries byte positions. Offsets are clamped to the content's bounds first to
    guard against an out-of-range span. Returns ``(start_byte, end_byte, match)``.
    """
    n = len(content)
    start = max(0, min(start, n))
    end = max(start, min(end, n))
    start_byte = len(content[:start].encode("utf-8"))
    end_byte = len(content[:end].encode("utf-8"))
    return start_byte, end_byte, content[start:end]


class Recognized(Protocol):
    """The slice of Presidio's ``RecognizerResult`` this handler consumes."""

    entity_type: str
    start: int  # Character offset (inclusive) of the match in the scanned text.
    end: int  # Character offset (exclusive) of the match in the scanned text.
    score: float  # Detection confidence, 0.0-1.0.


class Analyzer(Protocol):
    """The slice of ``AnalyzerEngine`` this handler depends on.

    Narrowing to a protocol keeps the engine injectable — tests can supply a
    lightweight fake instead of loading Presidio's NLP model.
    """

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> Sequence[Recognized]: ...


class FindingPublisher(Protocol):
    """The slice of ``gram_infra.pubsub.Publisher`` this handler depends on.

    Narrowing to a protocol keeps publishing injectable — tests supply a fake
    that captures findings instead of talking to Pub/Sub. The real
    ``Publisher[finding_pb2.Finding]`` satisfies it structurally: ``publish``
    returns immediately with a :class:`~gram_infra.pubsub.PublishResult` whose
    ``get`` is awaited to confirm the commit.
    """

    def publish(self, message: finding_pb2.Finding) -> PublishResult: ...


@dataclass(frozen=True)
class _Detection:
    """A real (non-false-positive) PII match, ready to become a Finding.

    Produced off the event loop by ``_scan`` so the analyzer call, false-positive
    classification (which may hit the embedded ASN database), and byte-offset
    conversion all stay on the worker thread.
    """

    entity_type: str
    rule_id: str
    match: str
    start_pos: int  # UTF-8 byte offset.
    end_pos: int  # UTF-8 byte offset.
    confidence: float


@dataclass(frozen=True)
class _PendingPublish:
    """A finding whose publish has already been fired; ``result.get`` confirms it.

    Built and fired off the event loop by ``_scan_and_publish`` so the proto
    build + serialize never run on the loop thread. The entity type and rule id
    ride along so the loop can assemble the summary log and per-finding error
    context without re-deriving them from the detection.
    """

    entity_type: str
    rule_id: str
    result: PublishResult


@dataclass(frozen=True)
class _FailedPublish:
    """A :class:`PublishResult` standing in for a publish that raised synchronously.

    ``Publisher.publish`` returns a future, but it can still raise *synchronously*
    (e.g. a misconfigured client). Capturing that as a result whose ``get`` re-raises
    routes it through ``_collect``'s normal publish-failure path — logged and
    skipped — exactly like an async commit failure. So one finding's failure
    neither aborts the message's other publishes nor lets the error escape the
    handler and nack the message (which would redeliver and duplicate the findings
    that already landed).
    """

    exc: Exception

    async def get(self) -> str:
        raise self.exc
