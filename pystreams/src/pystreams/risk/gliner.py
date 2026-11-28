"""Local GLiNER2 PII extraction behind the existing scanner contract."""

from __future__ import annotations

import importlib
import math
import os
import time
from dataclasses import dataclass
from typing import Protocol, Self, cast

import anyio
from anyio.to_thread import run_sync
from asyncer import asyncify

from pystreams.risk.scanner import Detection, ScanInputError, results_to_detections

MODEL_ID = "fastino/gliner2-privacy-filter-PII-multi"
MODEL_REVISION = "1cb4166094dc58fa8d836429f060d6c95f62b495"
DEFAULT_THRESHOLD = 0.9

ENTITIES = {
    "AU_TFN": "Australian Tax File Numbers",
    "CREDIT_CARD": "credit and debit card numbers",
    "CRYPTO": "cryptocurrency wallet addresses",
    "EMAIL_ADDRESS": "email addresses",
    "ES_NIF": "Spanish personal tax identifiers (NIF)",
    "IBAN_CODE": "International Bank Account Numbers (IBAN)",
    "IN_AADHAAR": "Indian Aadhaar identifiers",
    "IN_PAN": "Indian Permanent Account Numbers (PAN)",
    "IP_ADDRESS": "IPv4 and IPv6 addresses",
    "IT_FISCAL_CODE": "Italian personal fiscal codes",
    "MAC_ADDRESS": "network interface MAC addresses",
    "MEDICAL_BIOLOGICAL_ATTRIBUTE": "personal biological attributes in medical context",
    "MEDICAL_CLINICAL_EVENT": "clinical events in medical context",
    "MEDICAL_DISEASE_DISORDER": "diseases and disorders in medical context",
    "MEDICAL_FAMILY_HISTORY": "family medical history references",
    "MEDICAL_LICENSE": "medical license numbers",
    "MEDICAL_MEDICATION": "medications and drugs in medical context",
    "MEDICAL_THERAPEUTIC_PROCEDURE": "treatments and diagnostic procedures",
    "PHONE_NUMBER": "telephone numbers",
    "SG_NRIC_FIN": "Singapore NRIC and FIN identifiers",
    "UK_NHS": "UK National Health Service numbers",
    "UK_NINO": "UK National Insurance Numbers",
    "UK_PASSPORT": "UK passport numbers",
    "US_BANK_NUMBER": "US bank account numbers",
    "US_DRIVER_LICENSE": "US driver license numbers",
    "US_ITIN": "US Individual Taxpayer Identification Numbers",
    "US_MBI": "US Medicare Beneficiary Identifiers",
    "US_NPI": "US National Provider Identifiers",
    "US_PASSPORT": "US passport numbers",
    "US_SSN": "US Social Security Numbers",
    "PERSON": "person names",
    "LOCATION": "location references",
    "DATE_TIME": "date and time references",
    "NRP": "nationality, religious and political group references",
    "URL": "URLs",
}

# Keep benchmarked native labels; other catalogue types use descriptive zero-shot
# labels and require separate validation before a rollout.
LABELS = {entity: entity.lower() for entity in ENTITIES}
LABELS.update(
    {
        "PERSON": "person",
        "EMAIL_ADDRESS": "email",
        "PHONE_NUMBER": "phone_number",
        "CREDIT_CARD": "payment_card",
        "IBAN_CODE": "iban",
        "US_SSN": "national_id_number",
        "IP_ADDRESS": "ip_address",
        "UK_NHS": "nhs_number",
    }
)
DESCRIPTIONS = dict(ENTITIES)
DESCRIPTIONS.update(
    {
        "PERSON": "Full names of people",
        "EMAIL_ADDRESS": "Email addresses",
        "PHONE_NUMBER": "Telephone numbers, including country codes",
        "CREDIT_CARD": "Credit or debit card numbers",
        "US_SSN": "United States Social Security numbers (SSN)",
        "IP_ADDRESS": "IPv4 or IPv6 addresses",
        "UK_NHS": "United Kingdom NHS patient identification numbers",
    }
)


@dataclass
class EntitySpan:
    entity_type: str
    start: int
    end: int
    score: float


class GlinerResponseError(RuntimeError):
    """A failed model call or invalid result, without input or model output."""


class Extractor(Protocol):
    def extract(
        self, text: str, labels: dict[str, str], threshold: float
    ) -> object: ...


class LocalExtractor:
    def __init__(self) -> None:
        # Optional dependencies stay out of the Presidio startup path. Fetch the
        # pinned checkpoint separately; serving must not download mutable weights.
        torch = importlib.import_module("torch")
        hub = importlib.import_module("huggingface_hub")
        gliner = importlib.import_module("gliner2")
        torch.set_num_threads(4)
        path = hub.snapshot_download(
            MODEL_ID, revision=MODEL_REVISION, local_files_only=True
        )
        self._torch = torch
        self._model = gliner.AutoExtractor.from_pretrained(path, map_location="cpu")
        self._model.eval()

    def extract(self, text: str, labels: dict[str, str], threshold: float) -> object:
        with self._torch.inference_mode():
            return self._model.extract_entities_long(
                text,
                labels,
                threshold=threshold,
                chunk_size=256,
                chunk_overlap=48,
                batch_size=1,
                include_spans=True,
                include_confidence=True,
            )


class GlinerScanner:
    def __init__(
        self,
        extractor: Extractor,
        *,
        threshold: float = DEFAULT_THRESHOLD,
        timeout: float = 30.0,
        slot_timeout: float = 1.0,
    ) -> None:
        if not math.isfinite(threshold) or not 0 <= threshold <= 1:
            raise ValueError("invalid GLiNER threshold")
        if any(not math.isfinite(v) or v <= 0 for v in (timeout, slot_timeout)):
            raise ValueError("invalid GLiNER timeout")
        self._extractor = extractor
        self._threshold = threshold
        self._timeout = timeout
        self._slot_timeout = slot_timeout
        self._limiter = anyio.CapacityLimiter(1)

    @classmethod
    def from_env(cls, *, timeout: float = 30, slot_timeout: float = 1) -> Self:
        return cls(
            LocalExtractor(),
            threshold=float(
                os.environ.get("GRAM_GLINER_MIN_SCORE", str(DEFAULT_THRESHOLD))
            ),
            timeout=timeout,
            slot_timeout=slot_timeout,
        )

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(self, *exc_info: object) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        # No work is abandoned on cancellation, so callers drain scans before
        # releasing the scanner; there is no detached inference queue to close.
        return None

    async def scan(
        self,
        content: str,
        entities: list[str] | None,
        score_threshold: float,
    ) -> list[Detection]:
        spans = await self.recognize(content, entities, score_threshold)
        return await asyncify(results_to_detections)(content, spans)

    async def recognize(
        self,
        content: str,
        entities: list[str] | None,
        score_threshold: float,
    ) -> list[EntitySpan]:
        if not math.isfinite(score_threshold) or not 0 <= score_threshold <= 1:
            raise ScanInputError("invalid confidence threshold")
        selected = list(dict.fromkeys(entities or ENTITIES))
        if any(entity not in ENTITIES for entity in selected):
            raise ScanInputError("unsupported PII entity")
        if len(content.encode("utf-8")) > 256 * 1024:
            raise ScanInputError("GLiNER input exceeds 256 KiB")
        if not content:
            return []
        with anyio.fail_after(self._slot_timeout):
            await self._limiter.acquire()
        try:
            # Shield worker completion: cancellation must not release the model
            # slot while its inference still runs. CPU deadlines are soft.
            return await run_sync(
                self._recognize,
                content,
                selected,
                max(self._threshold, score_threshold),
                abandon_on_cancel=False,
            )
        finally:
            self._limiter.release()

    def _recognize(
        self, text: str, entities: list[str], threshold: float
    ) -> list[EntitySpan]:
        started = time.monotonic()
        try:
            result = self._extractor.extract(
                text, {LABELS[e]: DESCRIPTIONS[e] for e in entities}, threshold
            )
            spans = parse_result(text, entities, threshold, result)
        except Exception:
            raise GlinerResponseError("PII model scan failed") from None
        if time.monotonic() - started > self._timeout:
            raise TimeoutError("PII scan deadline exceeded")
        return spans


def parse_result(
    text: str,
    entities: list[str],
    threshold: float,
    result: object,
) -> list[EntitySpan]:
    if not isinstance(result, dict) or not isinstance(result.get("entities"), dict):
        raise GlinerResponseError("invalid entity result")
    found = cast(dict[str, object], result["entities"])
    output: dict[tuple[str, int, int], EntitySpan] = {}
    for entity in entities:
        values = found.get(LABELS[entity])
        if not isinstance(values, list):
            raise GlinerResponseError("missing entity result")
        for value in values:
            if not isinstance(value, dict):
                raise GlinerResponseError("invalid span")
            start, end, score = (
                value.get("start"),
                value.get("end"),
                value.get("confidence"),
            )
            if (
                type(start) is not int
                or type(end) is not int
                or not 0 <= start < end <= len(text)
                or text[start:end] != value.get("text")
                or not isinstance(score, (int, float))
                or isinstance(score, bool)
                or not math.isfinite(score)
                or not 0 <= score <= 1
            ):
                raise GlinerResponseError("invalid span")
            if score >= threshold:
                key = (entity, start, end)
                if key not in output or score > output[key].score:
                    output[key] = EntitySpan(entity, start, end, score)
    return sorted(
        output.values(), key=lambda span: (span.start, span.end, span.entity_type)
    )
