"""Experiment: scan gram.risk.v1.PresidioRequest messages with Presidio.

Subscribes to the ``gram.risk.v1.PresidioScanner`` subscription (declared in
``infra/proto/gram/risk/v1/scanners.proto``), which delivers ``PresidioRequest``
messages published to the ``gram.risk.v1.PresidioRequest`` topic. For each
message, every entry in ``contents`` is run through a Presidio ``AnalyzerEngine``
and each detection is printed to stdout as a logfmt ``Finding`` line.

Run it against the local Pub/Sub emulator:

    cd presidio-scanner
    uv sync
    uv run python -m spacy download en_core_web_sm   # one-time
    PUBSUB_EMULATOR_HOST=localhost:8088 uv run presidio-scanner

The reused ``EmulatedPubSubBroker`` reconciles the topic + subscription on
demand, so no Config Connector / GCP resources are needed locally. Feed it with
the Go CLI: ``cd infra && go run . presidio-submit "<text>"``.
"""

from __future__ import annotations

import logging
import os
from dataclasses import dataclass, field

from google.cloud.pubsub_v1 import PublisherClient, SubscriberClient
from gram.risk.v1 import presidio_request_pb2, scanners_pb2
from gram_infra.pubsub import (
    EmulatedPubSubBroker,
    pubsub_subscriber_for_message,
)

logger = logging.getLogger("presidio-scanner")

# Detection source label, mirroring SourcePresidio in the Go risk_analysis package.
SOURCE_PRESIDIO = "presidio"

# Rule-id prefix and dead-letter sentinel, mirroring the Go risk_analysis rules.
PII_PREFIX = "pii."
DEAD_LETTER_RULE_ID = PII_PREFIX + "dead_letter"

# How many times to retry analyzing a single content string before emitting a
# dead-letter sentinel Finding (mirrors the Go scanner's retry-then-dead-letter
# behaviour, in miniature).
RETRY_MAX_ATTEMPTS = 3


@dataclass
class Finding:
    """Python mirror of the Go Finding struct (risk_analysis/gitleaks.go).

    Field names use snake_case; the logfmt output keeps the same shape so the
    two scanners are comparable.
    """

    rule_id: str = ""
    description: str = ""
    match: str = ""
    start_pos: int = 0  # byte position in the scanned string
    end_pos: int = 0  # byte position in the scanned string
    tags: list[str] = field(default_factory=list)
    source: str = ""  # detection source (e.g. "gitleaks", "presidio")
    confidence: float = 0.0  # 0.0-1.0 confidence score
    # Non-empty => dead-letter sentinel, not a real finding.
    dead_letter_reason: str = ""


def _logfmt(**fields: object) -> str:
    """Render key=value pairs in logfmt, quoting values that need it."""
    parts = []
    for key, value in fields.items():
        if isinstance(value, bool):
            rendered = "true" if value else "false"
        elif isinstance(value, float):
            rendered = f"{value:g}"
        elif isinstance(value, (list, tuple)):
            rendered = ",".join(str(v) for v in value)
        else:
            rendered = str(value)

        if rendered == "" or any(c in rendered for c in ' "='):
            rendered = '"' + rendered.replace("\\", "\\\\").replace('"', '\\"') + '"'
        parts.append(f"{key}={rendered}")
    return " ".join(parts)


def _print_finding(finding: Finding) -> None:
    print(
        _logfmt(
            rule_id=finding.rule_id,
            description=finding.description,
            match=finding.match,
            start_pos=finding.start_pos,
            end_pos=finding.end_pos,
            tags=finding.tags,
            source=finding.source,
            confidence=finding.confidence,
            dead_letter_reason=finding.dead_letter_reason,
        ),
        flush=True,
    )


def describe_presidio_entity(entity_type: str) -> tuple[str, str]:
    """Map a Presidio entity type to a (rule_id, description) pair.

    Mirrors DescribePresidioEntity in the Go risk_analysis package for the
    common entities; falls back to a derived rule id/description otherwise.
    """
    known: dict[str, tuple[str, str]] = {
        "EMAIL_ADDRESS": ("pii.email_address", "Email address"),
        "PHONE_NUMBER": ("pii.phone_number", "Phone number"),
        "CREDIT_CARD": ("pii.credit_card", "Credit card number"),
        "US_SSN": ("pii.us_ssn", "US Social Security number"),
        "IBAN_CODE": ("pii.iban_code", "IBAN code"),
        "IP_ADDRESS": ("pii.ip_address", "IP address"),
        "CRYPTO": ("pii.crypto", "Crypto wallet address"),
        "US_BANK_NUMBER": ("pii.us_bank_number", "US bank account number"),
        "URL": ("pii.url", "URL"),
        "PERSON": ("pii.person", "Person name"),
        "LOCATION": ("pii.location", "Location"),
    }
    if entity_type in known:
        return known[entity_type]
    return (
        PII_PREFIX + entity_type.lower(),
        entity_type.replace("_", " ").capitalize(),
    )


def _build_analyzer():
    """Construct a Presidio AnalyzerEngine backed by the small spaCy model."""
    from presidio_analyzer import AnalyzerEngine
    from presidio_analyzer.nlp_engine import NlpEngineProvider

    configuration = {
        "nlp_engine_name": "spacy",
        "models": [{"lang_code": "en", "model_name": "en_core_web_sm"}],
    }
    nlp_engine = NlpEngineProvider(nlp_configuration=configuration).create_engine()
    return AnalyzerEngine(nlp_engine=nlp_engine, supported_languages=["en"])


def _scan_content(analyzer, content: str, entities: list[str] | None) -> list[Finding]:
    """Run Presidio over a single content string and convert results to Findings.

    Presidio reports rune (character) offsets; we convert them to byte offsets to
    match the Go Finding (see convertPresidioFindings in presidio.go).
    """
    results = analyzer.analyze(text=content, language="en", entities=entities)

    findings: list[Finding] = []
    for r in results:
        start = max(0, min(r.start, len(content)))
        end = max(start, min(r.end, len(content)))
        match = content[start:end]
        rule_id, description = describe_presidio_entity(r.entity_type)
        findings.append(
            Finding(
                rule_id=rule_id,
                description=description,
                match=match,
                start_pos=len(content[:start].encode("utf-8")),
                end_pos=len(content[:end].encode("utf-8")),
                tags=["pii"],
                source=SOURCE_PRESIDIO,
                confidence=r.score,
                dead_letter_reason="",
            )
        )
    return findings


def _handle(analyzer, msg: presidio_request_pb2.PresidioRequest, meta) -> None:
    entities = list(msg.entities) or None
    logger.info(
        "scanning request id=%s contents=%d entities=%s (delivery_attempt=%s)",
        msg.id,
        len(msg.contents),
        entities or "<defaults>",
        meta.delivery_attempt,
    )

    for index, content in enumerate(msg.contents):
        last_err: Exception | None = None
        for _ in range(RETRY_MAX_ATTEMPTS):
            try:
                for finding in _scan_content(analyzer, content, entities):
                    _print_finding(finding)
                last_err = None
                break
            except Exception as err:  # noqa: BLE001 - retry, then dead-letter
                last_err = err

        if last_err is not None:
            # Exhausted the retry budget for this content: emit a dead-letter
            # sentinel Finding instead of a real detection.
            _print_finding(
                Finding(
                    rule_id=DEAD_LETTER_RULE_ID,
                    description="Presidio could not analyze this content after exhausting its retry budget.",
                    source=SOURCE_PRESIDIO,
                    dead_letter_reason=f"content[{index}]: {last_err}",
                )
            )


def main() -> None:
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s"
    )

    if not os.environ.get("PUBSUB_EMULATOR_HOST"):
        logger.warning(
            "PUBSUB_EMULATOR_HOST is not set; start the local emulator and "
            "re-run with e.g. PUBSUB_EMULATOR_HOST=localhost:8088 uv run presidio-scanner"
        )

    project_id = os.environ.get("GOOGLE_CLOUD_PROJECT", "my-project-id")
    broker = EmulatedPubSubBroker(
        project_id, PublisherClient(), SubscriberClient(), logger=logger
    )

    # A handle on the PresidioScanner subscription delivering PresidioRequest messages.
    subscriber = pubsub_subscriber_for_message(
        broker,
        presidio_request_pb2.PresidioRequest,
        scanners_pb2.PresidioScanner,
        logger=logger,
    )

    logger.info("building Presidio analyzer (loading spaCy model)...")
    analyzer = _build_analyzer()

    logger.info(
        "subscriber started; waiting for PresidioRequest messages (Ctrl-C to stop)"
    )
    subscriber.receive(lambda msg, meta: _handle(analyzer, msg, meta))


if __name__ == "__main__":
    main()
