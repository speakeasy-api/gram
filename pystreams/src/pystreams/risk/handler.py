import time
import uuid
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Protocol

import structlog
from asyncer import asyncify
from gram.risk.v1 import finding_pb2, presidio_analysis_pb2
from gram_infra.pubsub import PublishResult
from gram_infra.pubsub.subscriber import MessageMetadata
from opentelemetry import trace

from pystreams import attr
from pystreams.risk import metrics
from pystreams.risk.scanner import (
    DEFAULT_SCORE_THRESHOLD,
    Detection,
    Scanner,
    ScanSlotTimeout,
)

# Source label stamped on every finding this handler emits, so all findings
# from the Presidio path are attributed identically downstream.
SOURCE_PRESIDIO = "presidio"


# Generic, source-agnostic description stamped on every published finding. The
# canonical rule_id carries the specific entity, so a consumer can resolve a
# richer description from it.
_FINDING_DESCRIPTION = "Identified potentially sensitive personal information."


class PresidioHandler:
    """Publishes a :class:`Finding` per PII detection to the findings topic.

    The PII scan itself is delegated to an injected :class:`Scanner` (see
    ``scanner.py``); this handler owns everything *around* it — logging, mapping
    each :class:`Detection` onto a :class:`Finding`, and publishing to the
    ``gram.risk.v1.Finding`` topic for downstream persistence. Every recognized
    entity becomes a finding carrying the originating request/message context plus
    the match span. The matched value is included on the published finding, but is
    never written to the handler's own logs — log lines report entity *types* and
    counts only, so they stay safe to retain.

    The scanner determines how the scan is parallelized: a ``ProcessPoolScanner``
    (the default) that breaks the single-process GIL ceiling, or an in-process
    ``ThreadScanner``. The handler is agnostic to which it holds.
    """

    def __init__(
        self,
        logger: structlog.stdlib.BoundLogger,
        publisher: FindingPublisher,
        scanner: Scanner,
    ):
        self.logger = logger
        self.publisher = publisher
        self._scanner = scanner

    async def handle(
        self,
        message: presidio_analysis_pb2.PresidioAnalysis,
        meta: MessageMetadata,
    ) -> None:
        # An empty list means "analyze every entity Presidio knows about".
        requested = list(message.entities) or None
        # Zero/unset on the request means "use the default floor".
        score_threshold = message.score_threshold or DEFAULT_SCORE_THRESHOLD

        # Stamp the exact input size on the delivery span (the traced-receiver
        # span is current here). A size — never content — is safe to record, and
        # span attributes aren't cardinality-bounded like metric tags, so trace
        # analytics can scatter per-message duration against exact size and click
        # through to the pathological payloads. The metrics below carry the same
        # dimension as a bounded ``size_bucket`` band for dashboards/monitors.
        content_chars = len(message.content)
        trace.get_current_span().set_attribute(attr.RISK_CONTENT_CHARS, content_chars)
        size_bucket = metrics.size_bucket_for(content_chars)

        # Time the whole handler end to end and record it as a distribution,
        # tagged with the terminal outcome. The timer starts here and is recorded
        # in the ``finally`` so every path — scan failure, nothing detected, or
        # findings published — contributes, and ``outcome`` defaults to error so a
        # swallowed scan failure (which returns early) is attributed correctly.
        process_started = time.perf_counter()
        outcome = metrics.OUTCOME_ERROR
        try:
            # The scan (CPU/GIL-bound spaCy + Presidio work plus the false-positive
            # filter and byte-offset conversion) runs off the event loop inside the
            # scanner — on a worker thread or in a separate process. Timed as wall
            # clock, so it includes any wait for a free scan slot / pool worker,
            # which under load (not the scan itself) can dominate per-message ACK
            # latency.
            try:
                scan_started = time.perf_counter()
                detections = await self._scanner.scan(
                    message.content, requested, score_threshold
                )
                scan_ms = (time.perf_counter() - scan_started) * 1000
            except ScanSlotTimeout:
                # The scan never started: the pool was backlogged for the whole
                # slot budget and the content was never touched. Unlike the
                # swallowed failures below this is not a property of the
                # message, so re-raising to nack is safe (nothing scanned,
                # nothing published — redelivery duplicates no work) and
                # correct: the subscription's retry policy backs the message
                # off (10s..600s) and it lands back here once capacity clears,
                # instead of being silently dropped unscanned. No log line:
                # backlog requeues fire in bursts, and the ``requeued`` outcome
                # recorded in the ``finally`` already carries the signal.
                outcome = metrics.OUTCOME_REQUEUED
                raise
            except Exception as exc:
                # This is best-effort shadow processing, and the PresidioAnalyzer
                # subscription declares no dead-letter policy. Letting a scan
                # failure escape would nack the message, and any input that
                # deterministically trips the analyzer would then redeliver and
                # fail again for the full retention window (30 days) — one bad
                # message poisoning the subscription. Swallow it (so the message is
                # acked) and log instead. Cancellation derives from BaseException,
                # not Exception, so graceful shutdown still propagates. Only the
                # exception *type* is logged: an error string or traceback could
                # echo the scanned content, which this handler never emits (see the
                # detection log below).
                self.logger.warning(
                    "presidio scan failed",
                    request_id=message.request_id,
                    reply_urn=message.reply_urn,
                    requested_entities=requested or [],
                    error_type=type(exc).__name__,
                    delivery_attempt=meta.delivery_attempt,
                )
                return

            if not detections:
                outcome = metrics.OUTCOME_CLEAN
                return

            # Build + serialize + enqueue every finding off the event loop too,
            # then await the commits. Keeping the proto build and
            # ``SerializeToString`` on a worker thread (not the loop) is
            # deliberate: done on the loop it would be per-finding GIL work
            # competing with message intake. ``asyncify`` copies the contextvar
            # context into the worker, so trace-context injection on publish is
            # preserved.
            publish_started = time.perf_counter()
            entity_types, pending = await asyncify(self._build_and_dispatch)(
                message, detections
            )
            published = await self._collect(message, pending)
            publish_ms = (time.perf_counter() - publish_started) * 1000
            # "detected" means at least one finding actually landed. If every
            # publish failed (all swallowed by ``_collect``), the work is an error
            # outcome, not a success — keeping those out of the detected bucket so
            # it stays a clean read of healthy detection latency.
            outcome = metrics.OUTCOME_DETECTED if published else metrics.OUTCOME_ERROR

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
            await self.logger.adebug(
                "presidio scan detected entities",
                request_id=message.request_id,
                reply_urn=message.reply_urn,
                requested_entities=requested or [],
                detected_entity_types=sorted(entity_types),
                detected_count=sum(entity_types.values()),
                published_count=published,
                delivery_attempt=meta.delivery_attempt,
                # Per-message latency split, safe to retain (durations, never
                # content): the off-loop scan (incl. scan-slot / pool-worker wait)
                # vs. the build + publish-dispatch and the wait for those
                # publishes' commits.
                scan_ms=round(scan_ms, 1),
                publish_ms=round(publish_ms, 1),
            )
        finally:
            metrics.record_process_duration(
                time.perf_counter() - process_started, outcome, size_bucket
            )

    def _build_and_dispatch(
        self,
        message: presidio_analysis_pb2.PresidioAnalysis,
        detections: list[Detection],
    ) -> tuple[dict[str, int], list[_PendingPublish]]:
        """Build a finding per detection and fire each publish — all off the loop.

        Runs on a worker thread (via ``asyncify``): the per-finding proto build +
        ``SerializeToString`` + client enqueue all stay off the event-loop thread.

        Returns the histogram of detected entity types (for the summary log) and
        the fired publishes for ``_collect`` to await. A message's findings are
        independent, so each publish is fired up front (``publish`` returns a
        future without waiting and the client batches the sends); in series N
        findings would pay N Pub/Sub round-trips back to back. A publish that raises
        *synchronously* (e.g. a stopped client) is logged and skipped here — the
        same disposition ``_collect`` gives an async commit failure — so one
        finding's failure neither aborts the rest nor escapes to nack (and
        duplicate) the message.
        """
        # A fresh detection timestamp (UTC, RFC3339) stamped on every finding from
        # this delivery — when the scan ran, not when the request was created.
        created_at = datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
        # An explicit loop, not a comprehension: each publish needs its own
        # try/except so a synchronous failure skips just that finding.
        entity_types: dict[str, int] = {}
        pending: list[_PendingPublish] = []
        # Occurrence counter per deterministic base id, mirroring the Go
        # publisher (scanners.StartPublishFindings): identical detections in one
        # message publish under stable suffixed ids instead of colliding.
        occurrences: dict[uuid.UUID, int] = {}
        for d in detections:
            entity_types[d.entity_type] = entity_types.get(d.entity_type, 0) + 1
            rule_id = _canonical_rule_id(d.entity_type)
            base_id = _deterministic_finding_id(message, rule_id, d)
            finding_id = base_id
            n = occurrences.get(base_id, 0)
            if n > 0:
                finding_id = _duplicate_finding_id(base_id, n)
            occurrences[base_id] = n + 1
            finding = finding_pb2.Finding(
                id=str(finding_id),
                request_id=message.request_id,
                chat_message_id=message.chat_message_id,
                content_part_id=message.content_part_id,
                project_id=message.project_id,
                organization_id=message.organization_id,
                risk_policy_id=message.risk_policy_id,
                risk_policy_version=message.risk_policy_version,
                created_at=created_at,
                rule_id=rule_id,
                description=_FINDING_DESCRIPTION,
                match=d.match,
                start_pos=d.start_pos,
                end_pos=d.end_pos,
                tags=["pii"],
                source=SOURCE_PRESIDIO,
                confidence=d.confidence,
                # Presidio scans the request's verbatim content, so the span
                # offsets index the anchored message/part text.
                surface="content",
            )
            try:
                result = self.publisher.publish(finding)
            except Exception as exc:
                self._log_publish_failure(message, rule_id, exc)
                continue
            pending.append(_PendingPublish(rule_id=rule_id, result=result))
        return entity_types, pending

    async def _collect(
        self,
        message: presidio_analysis_pb2.PresidioAnalysis,
        pending: Sequence[_PendingPublish],
    ) -> int:
        """Await each already-fired publish's commit; return how many landed.

        A commit failure is logged and skipped rather than raised: this is
        best-effort shadow processing, so a transient publish failure must not
        redrive the whole scan. Deterministic finding ids mean a redelivery
        would at least republish under the same ids (ClickHouse dedupes by id),
        but acking keeps one flaky commit from re-running the scanner.
        """
        published = 0
        for p in pending:
            try:
                await p.result.get()
            except Exception as exc:
                self._log_publish_failure(message, p.rule_id, exc)
                continue
            published += 1

        return published

    def _log_publish_failure(
        self,
        message: presidio_analysis_pb2.PresidioAnalysis,
        rule_id: str,
        exc: Exception,
    ) -> None:
        """Log one finding's publish failure (synchronous dispatch or async commit).

        Never echo the finding — it carries the matched value — so only the request,
        rule id, and exception type are logged.
        """
        self.logger.error(
            "failed to publish risk finding",
            request_id=message.request_id,
            rule_id=rule_id,
            error_type=type(exc).__name__,
        )


def _canonical_rule_id(entity_type: str) -> str:
    """Map a Presidio UPPER_SNAKE entity type to the canonical rule id.

    Lowercases the entity name and prefixes it with ``pii.`` (e.g.
    ``EMAIL_ADDRESS`` -> ``pii.email_address``) so the same finding gets the same
    rule id regardless of which path produced it.
    """
    return "pii." + entity_type.lower()


def _deterministic_finding_id(
    message: presidio_analysis_pb2.PresidioAnalysis,
    rule_id: str,
    detection: Detection,
) -> uuid.UUID:
    """Derive the finding id from its identity, not a fresh random uuid.

    Byte-for-byte the Go derivation in ``scanners.deterministicFindingID``
    (server/internal/scanners/publish.go): ``uuid.uuid5(NAMESPACE_URL, name)``
    is the same SHA-1 construction as Go's ``uuid.NewSHA1(NameSpaceURL, name)``.
    A redelivered Pub/Sub message then republishes each detection under the id
    it used the first time, so ClickHouse's id-level dedup collapses the
    duplicates instead of counting a new row per delivery attempt.
    """
    parts = [message.request_id, message.chat_message_id]
    if message.content_part_id:
        parts.append(message.content_part_id)
    parts.extend(
        [
            message.project_id,
            message.organization_id,
            message.risk_policy_id,
            str(message.risk_policy_version),
            SOURCE_PRESIDIO,
            rule_id,
            str(detection.start_pos),
            str(detection.end_pos),
            detection.match,
        ]
    )
    return uuid.uuid5(uuid.NAMESPACE_URL, "gram:risk:finding:" + "\x00".join(parts))


def _duplicate_finding_id(base: uuid.UUID, n: int) -> uuid.UUID:
    """Id for the n-th repeat (n >= 1) of a colliding detection in one message.

    Mirrors Go's ``duplicateFindingID``: derived from the base id rather than
    re-joining the parts so it can never collide with any base id, and stable
    across redeliveries because the repeat ordinal follows detection order.
    """
    return uuid.uuid5(uuid.NAMESPACE_URL, f"gram:risk:finding:dup:{base}:{n}")


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
class _PendingPublish:
    """A fired publish awaiting its commit; ``result.get`` confirms or raises.

    Built and fired off the event loop by ``_build_and_dispatch`` so the proto
    build + serialize never run on the loop thread. The rule id rides along so
    ``_collect`` can attribute a commit failure without re-deriving it.
    """

    rule_id: str
    result: PublishResult
