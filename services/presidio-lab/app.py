from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

BASE_SUPPORTED_ENTITIES = {
    "CREDIT_CARD",
    "CRYPTO",
    "DATE_TIME",
    "DOMAIN_NAME",
    "EMAIL_ADDRESS",
    "IBAN_CODE",
    "IP_ADDRESS",
    "LOCATION",
    "MAC_ADDRESS",
    "MEDICAL_LICENSE",
    "NRP",
    "PERSON",
    "PHONE_NUMBER",
    "SG_NRIC_FIN",
    "UK_NHS",
    "URL",
    "US_BANK_NUMBER",
    "US_DRIVER_LICENSE",
    "US_ITIN",
    "US_PASSPORT",
    "US_SSN",
}

HEALTHCARE_EXPERIMENTAL_ENTITIES = {
    "US_MBI",
    "US_NPI",
    "MEDICAL_DISEASE_DISORDER",
    "MEDICAL_MEDICATION",
    "MEDICAL_THERAPEUTIC_PROCEDURE",
    "MEDICAL_CLINICAL_EVENT",
    "MEDICAL_BIOLOGICAL_ATTRIBUTE",
    "MEDICAL_FAMILY_HISTORY",
}

SUPPORTED_ENTITIES = sorted(BASE_SUPPORTED_ENTITIES | HEALTHCARE_EXPERIMENTAL_ENTITIES)

PROCEDURE_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bcolonoscopy\b",
        r"\bbiopsy\b",
        r"\bmri\b",
        r"\bct scan\b",
        r"\bchemotherapy\b",
        r"\bradiation therapy\b",
        r"\bappendectomy\b",
        r"\bdialysis\b",
        r"\bphysical therapy\b",
        r"\bsurgery\b",
    ]
]

DISEASE_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bcrohn'?s disease\b",
        r"\bdiabetes\b",
        r"\bhypertension\b",
        r"\basthma\b",
        r"\bmigraine\b",
        r"\bcancer\b",
        r"\bpsoriasis\b",
        r"\bdepression\b",
    ]
]

MEDICATION_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bamoxicillin\b",
        r"\bmetformin\b",
        r"\binsulin\b",
        r"\blisinopril\b",
        r"\bibuprofen\b",
        r"\balbuterol\b",
        r"\bsertraline\b",
        r"\bprednisone\b",
    ]
]

CLINICAL_EVENT_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bhospitalized\b",
        r"\badmitted\b",
        r"\bdischarged\b",
        r"\bseizure(?: episode)?\b",
        r"\bstroke\b",
        r"\brelapse\b",
        r"\bflare(?:-up)?\b",
        r"\bpost-?op complication\b",
        r"\ber visit\b",
    ]
]

BIOLOGICAL_ATTRIBUTE_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bblood pressure\b(?:\s+was|\s+of|\s*:)?\s*\d{2,3}/\d{2,3}\b",
        r"\bheart rate\b(?:\s+was|\s+of|\s*:)?\s*\d{2,3}\b",
        r"\ba1c\b(?:\s+was|\s+of|\s*:)?\s*\d+(?:\.\d+)?%",
        r"\bbmi\b(?:\s+was|\s+of|\s*:)?\s*\d+(?:\.\d+)?\b",
        r"\boxygen saturation\b(?:\s+was|\s+of|\s*:)?\s*\d+(?:\.\d+)?%",
        r"\bhemoglobin\b(?:\s+was|\s+of|\s*:)?\s*\d+(?:\.\d+)?\b",
    ]
]

FAMILY_HISTORY_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bfamily history of [^.]+",
        r"\bmother had [^.]+",
        r"\bfather had [^.]+",
        r"\bsister had [^.]+",
        r"\bbrother had [^.]+",
        r"\bruns in the family\b",
    ]
]

MEDICAL_LICENSE_PATTERN = re.compile(r"\bMD\d{6}\b")
US_NPI_PATTERN = re.compile(r"\b\d{10}\b")
US_MBI_PATTERN = re.compile(r"\b[1-9][A-Z]{2}\d[A-Z]{2}\d[A-Z]{2}\d{2}\b")


@dataclass(frozen=True)
class Detection:
    entity_type: str
    start: int
    end: int
    score: float

    def to_dict(self) -> dict[str, int | float | str]:
        return {
            "entity_type": self.entity_type,
            "start": self.start,
            "end": self.end,
            "score": self.score,
        }


class AnalyzeRequest(BaseModel):
    text: list[str] = Field(default_factory=list)
    language: str = "en"
    score_threshold: float = 0.0
    entities: list[str] | None = None


app = FastAPI(title="Gram Presidio Lab")


@app.get("/health")
def health() -> str:
    return "Presidio Analyzer service is up"


@app.get("/supportedentities")
def supported_entities() -> list[str]:
    return SUPPORTED_ENTITIES


@app.post("/analyze")
def analyze(request: AnalyzeRequest) -> list[list[dict[str, int | float | str]]]:
    if not request.text:
        raise HTTPException(status_code=500, detail="No text provided")

    allowed = requested_entities(request.entities)
    response: list[list[dict[str, int | float | str]]] = []
    for text in request.text:
        detections = detect_text(text, allowed)
        detections = [d for d in detections if d.score >= request.score_threshold]
        detections.sort(key=lambda item: (item.start, item.end, item.entity_type))
        response.append([d.to_dict() for d in dedupe(detections)])
    return response


def requested_entities(entities: list[str] | None) -> set[str]:
    if not entities:
        return set(SUPPORTED_ENTITIES)
    return {entity for entity in entities if entity in SUPPORTED_ENTITIES}


def detect_text(text: str, allowed: set[str]) -> list[Detection]:
    detections: list[Detection] = []

    if "MEDICAL_LICENSE" in allowed:
        detections.extend(match_pattern(text, MEDICAL_LICENSE_PATTERN, "MEDICAL_LICENSE", 0.9))
    if "US_NPI" in allowed:
        detections.extend(detect_npi(text))
    if "US_MBI" in allowed:
        detections.extend(match_pattern(text, US_MBI_PATTERN, "US_MBI", 0.92))
    if "MEDICAL_DISEASE_DISORDER" in allowed:
        detections.extend(match_patterns(text, DISEASE_PATTERNS, "MEDICAL_DISEASE_DISORDER", 0.85))
    if "MEDICAL_MEDICATION" in allowed:
        detections.extend(match_patterns(text, MEDICATION_PATTERNS, "MEDICAL_MEDICATION", 0.84))
    if "MEDICAL_THERAPEUTIC_PROCEDURE" in allowed:
        detections.extend(match_patterns(text, PROCEDURE_PATTERNS, "MEDICAL_THERAPEUTIC_PROCEDURE", 0.87))
    if "MEDICAL_CLINICAL_EVENT" in allowed:
        detections.extend(match_patterns(text, CLINICAL_EVENT_PATTERNS, "MEDICAL_CLINICAL_EVENT", 0.83))
    if "MEDICAL_BIOLOGICAL_ATTRIBUTE" in allowed:
        detections.extend(match_patterns(text, BIOLOGICAL_ATTRIBUTE_PATTERNS, "MEDICAL_BIOLOGICAL_ATTRIBUTE", 0.9))
    if "MEDICAL_FAMILY_HISTORY" in allowed:
        detections.extend(match_patterns(text, FAMILY_HISTORY_PATTERNS, "MEDICAL_FAMILY_HISTORY", 0.88))

    return detections


def match_pattern(text: str, pattern: re.Pattern[str], entity_type: str, score: float) -> list[Detection]:
    return [
        Detection(entity_type=entity_type, start=match.start(), end=match.end(), score=score)
        for match in pattern.finditer(text)
    ]


def match_patterns(
    text: str,
    patterns: Iterable[re.Pattern[str]],
    entity_type: str,
    score: float,
) -> list[Detection]:
    detections: list[Detection] = []
    for pattern in patterns:
        detections.extend(match_pattern(text, pattern, entity_type, score))
    return detections


def detect_npi(text: str) -> list[Detection]:
    detections: list[Detection] = []
    for match in US_NPI_PATTERN.finditer(text):
        candidate = match.group(0)
        if not has_npi_context(text, match.start(), match.end()):
            continue
        if not is_valid_npi(candidate):
            continue
        detections.append(
            Detection(
                entity_type="US_NPI",
                start=match.start(),
                end=match.end(),
                score=0.94,
            )
        )
    return detections


def has_npi_context(text: str, start: int, end: int) -> bool:
    left = text[max(0, start - 24) : start].lower()
    right = text[end : min(len(text), end + 24)].lower()
    context = f"{left} {right}"
    return "npi" in context or "provider" in context


def is_valid_npi(value: str) -> bool:
    if len(value) != 10 or not value.isdigit():
        return False
    digits = [int(char) for char in ("80840" + value[:9])]
    total = 0
    for index, digit in enumerate(reversed(digits), start=1):
        if index % 2 == 1:
            digit *= 2
            if digit > 9:
                digit = digit // 10 + digit % 10
        total += digit
    check_digit = (10 - (total % 10)) % 10
    return check_digit == int(value[-1])


def dedupe(detections: Iterable[Detection]) -> list[Detection]:
    best: dict[tuple[str, int, int], Detection] = {}
    for detection in detections:
        key = (detection.entity_type, detection.start, detection.end)
        existing = best.get(key)
        if existing is None or detection.score > existing.score:
            best[key] = detection
    return list(best.values())
