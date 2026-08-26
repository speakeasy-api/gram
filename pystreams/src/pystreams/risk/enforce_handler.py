"""Inline Presidio enforcement scans with a correlated Redis reply.

Python counterpart of the gitleaks enforcement handler
(``server/internal/scanners/gitleaks/enforce_handler.go``), consuming the
dedicated ``gram.risk.v1.PresidioEnforcement`` topic. The handler scans one
request and writes an ``EnforcementReply`` to the replica inbox named by the
reply URN. Replies never carry raw matched text: findings hold masked previews
and tenant-peppered fingerprints only, and logs report entity types and counts.

Ack/nack policy mirrors the Go handler: stale requests, scan failures, and
reply-write failures are ACKed (redelivery cannot rescue an inline scan, and a
nack would eventually park raw request content in the DLQ), while undecodable
or structurally invalid requests raise so the restricted forensic DLQ retains
what could not be interpreted.
"""

from __future__ import annotations

import time
import uuid
from datetime import UTC, datetime
from typing import Final

import structlog
from gram.risk.v1 import enforcement_reply_pb2, presidio_enforcement_pb2
from gram_infra.pubsub.subscriber import MessageMetadata
from opentelemetry import metrics

from pystreams.risk import maskdisplay
from pystreams.risk.fingerprint import Fingerprinter, encode_fingerprint
from pystreams.risk.replywriter import ReplyWriter, parse_reply_urn
from pystreams.risk.scanner import (
    DEFAULT_SCORE_THRESHOLD,
    Detection,
    Scanner,
    ScanSlotTimeout,
)

# Maximum useful age of an inline scan request; the dispatcher's wait ceiling
# means anything older can no longer satisfy a caller.
DEFAULT_MAX_REQUEST_AGE_SECONDS: Final = 30.0

# Per-message content budget, matching replyinbox.MaxContentBytes on the Go
# dispatcher; a request that bypasses the dispatcher must not buy an unbounded
# scan.
MAX_CONTENT_BYTES: Final = 50 * 1024

# Transport-metadata key carrying the request's return address; must match
# requestreply.ReplyURNAttribute on the Go side.
REPLY_URN_ATTRIBUTE: Final = "gram-reply-urn"

_MAX_REPLY_REASON_CHARS: Final = 256

# Presidio pii.* rule ids mapped to their canonical category, mirroring the
# classification order in server/internal/risk/categories (first match wins;
# anything else with source "presidio" is pii).
_GOVERNMENT_ID_RULE_IDS: Final = frozenset(
    {
        "pii.us_ssn",
        "pii.us_passport",
        "pii.us_itin",
        "pii.uk_nhs",
        "pii.uk_nino",
        "pii.uk_passport",
        "pii.es_nif",
        "pii.it_fiscal_code",
        "pii.au_tfn",
        "pii.in_pan",
        "pii.in_aadhaar",
        "pii.sg_nric_fin",
    }
)
_HEALTHCARE_RULE_IDS: Final = frozenset(
    {
        "pii.medical_license",
        "pii.us_mbi",
        "pii.us_npi",
        "pii.medical_disease_disorder",
        "pii.medical_medication",
        "pii.medical_therapeutic_procedure",
        "pii.medical_clinical_event",
        "pii.medical_biological_attribute",
        "pii.medical_family_history",
    }
)
_OFF_POLICY_RULE_IDS: Final = frozenset(
    {
        "pii.harmful_content_request",
        "pii.policy_violation",
        "pii.unauthorized_action",
        "pii.topic_boundary_violation",
    }
)

_meter = metrics.get_meter("pystreams.risk.enforce")
_stale_dropped = _meter.create_counter(
    "risk.enforcement.presidio.stale_dropped",
    unit="{request}",
    description=(
        "Presidio enforcement requests acknowledged without scanning because "
        "they were stale"
    ),
)
_reply_write_errors = _meter.create_counter(
    "risk.enforcement.presidio.reply_write_errors",
    unit="{error}",
    description=("Presidio enforcement reply writes acknowledged after Redis failure"),
)


class MalformedEnforcementRequest(ValueError):
    """A structurally invalid request; raised so it lands in the forensic DLQ."""


class PresidioEnforceHandler:
    """Scans one inline request and writes a safe correlated reply."""

    def __init__(
        self,
        logger: structlog.stdlib.BoundLogger,
        writer: ReplyWriter,
        scanner: Scanner,
        fingerprinter: Fingerprinter,
        max_request_age_seconds: float = DEFAULT_MAX_REQUEST_AGE_SECONDS,
    ) -> None:
        self._logger = logger
        self._writer = writer
        self._scanner = scanner
        self._fingerprinter = fingerprinter
        if max_request_age_seconds <= 0:
            max_request_age_seconds = DEFAULT_MAX_REQUEST_AGE_SECONDS
        self._max_request_age = max_request_age_seconds
        self._consumer_id = str(uuid.uuid4())

    async def handle(
        self,
        message: presidio_enforcement_pb2.PresidioEnforcement,
        meta: MessageMetadata,
    ) -> None:
        try:
            created_at = datetime.fromisoformat(message.created_at)
            if created_at.tzinfo is None:
                raise ValueError("enforcement created_at must include a timezone")
        except ValueError as exc:
            raise MalformedEnforcementRequest("parse enforcement created_at") from exc
        age = (datetime.now(UTC) - created_at).total_seconds()
        # Symmetric window: a far-future stamp is as suspect as a stale one,
        # while ordinary clock skew stays within the allowance.
        if abs(age) > self._max_request_age:
            _stale_dropped.add(1)
            return
        if not message.organization_id:
            raise MalformedEnforcementRequest("enforcement organization id is required")
        if not message.project_id:
            raise MalformedEnforcementRequest("enforcement project id is required")
        # The reply address rides transport metadata, mirroring Go's
        # requestreply.ReplyURNAttribute; payloads carry no routing.
        reply_urn = meta.attributes.get(REPLY_URN_ATTRIBUTE, "")
        try:
            _, scan_id = parse_reply_urn(reply_urn)
        except ValueError as exc:
            raise MalformedEnforcementRequest("parse enforcement reply urn") from exc

        started = time.perf_counter()
        detections: list[Detection] = []
        status = enforcement_reply_pb2.ENFORCEMENT_STATUS_OK
        reason = ""
        if len(message.content.encode()) > MAX_CONTENT_BYTES:
            status = enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
            reason = "enforcement content exceeds the maximum byte budget"
        elif not 0.0 <= message.score_threshold <= 1.0:
            # A negative threshold admits noise; above 1.0 suppresses every
            # finding. Neither is a scan worth running.
            status = enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
            reason = "enforcement score threshold is outside 0.0-1.0"
        else:
            requested = list(message.entities) or None
            score_threshold = message.score_threshold or DEFAULT_SCORE_THRESHOLD
            try:
                detections = await self._scanner.scan(
                    message.content, requested, score_threshold
                )
            except ScanSlotTimeout:
                # Unlike the batch path, requeueing cannot rescue an inline
                # scan: reply ERROR now so the waiter applies its configured
                # failure mode instead of burning the deadline.
                status = enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
                reason = "presidio scan slot timeout"
            except Exception as exc:
                # Only the exception type: an error string can echo the
                # scanned content, which never leaves this handler.
                status = enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
                reason = "presidio scan failed: " + type(exc).__name__

        findings: list[enforcement_reply_pb2.EnforcementFinding] = []
        if status == enforcement_reply_pb2.ENFORCEMENT_STATUS_OK:
            try:
                findings = self._build_findings(message.organization_id, detections)
            except Exception as exc:
                status = enforcement_reply_pb2.ENFORCEMENT_STATUS_ERROR
                reason = "fingerprint enforcement finding"
                findings = []
                self._logger.error(
                    "fingerprint presidio enforcement finding",
                    request_id=message.request_id,
                    error_type=type(exc).__name__,
                )

        reply = enforcement_reply_pb2.EnforcementReply(
            correlation_id=scan_id,
            scanner=enforcement_reply_pb2.ENFORCEMENT_SCANNER_PRESIDIO,
            status=status,
            reason=reason[:_MAX_REPLY_REASON_CHARS],
            findings=findings,
            diagnostics=enforcement_reply_pb2.EnforcementDiagnostics(
                scan_duration_ms=int((time.perf_counter() - started) * 1000),
                consumer_id=self._consumer_id,
                delivery_attempt=meta.delivery_attempt or 0,
            ),
        )
        try:
            await self._writer.write(reply_urn, reply)
        except Exception as exc:
            _reply_write_errors.add(1)
            self._logger.error(
                "write presidio enforcement reply; acknowledging request",
                request_id=message.request_id,
                error_type=type(exc).__name__,
            )
            return

        await self._logger.adebug(
            "presidio enforcement scan complete",
            request_id=message.request_id,
            detections=len(detections),
            status=enforcement_reply_pb2.EnforcementStatus.Name(status),
        )

    def _build_findings(
        self, organization_id: str, detections: list[Detection]
    ) -> list[enforcement_reply_pb2.EnforcementFinding]:
        findings: list[enforcement_reply_pb2.EnforcementFinding] = []
        for d in detections:
            rule_id = "pii." + d.entity_type.lower()
            sum_, _ = self._fingerprinter.tenanted_hs256(
                organization_id, d.match.encode()
            )
            findings.append(
                enforcement_reply_pb2.EnforcementFinding(
                    rule_id=rule_id,
                    category=_classify(rule_id),
                    score=d.confidence,
                    start_pos=d.start_pos,
                    end_pos=d.end_pos,
                    surface="content",
                    masked_preview=maskdisplay.display(rule_id, d.match),
                    fingerprint=encode_fingerprint(sum_),
                )
            )
        return findings


def _classify(rule_id: str) -> str:
    if rule_id in maskdisplay.FINANCIAL_RULE_IDS:
        return "financial"
    if rule_id in _GOVERNMENT_ID_RULE_IDS:
        return "government_ids"
    if rule_id in _HEALTHCARE_RULE_IDS:
        return "healthcare"
    if rule_id in _OFF_POLICY_RULE_IDS:
        return "off_policy"
    return "pii"
