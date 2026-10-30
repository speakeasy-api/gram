import math
import time

import anyio
import httpx
import pytest

from pystreams.cmd.gliner_server import build_app
from pystreams.risk.gliner import (
    ENTITIES,
    GlinerResponseError,
    GlinerScanner,
    parse_result,
)
from pystreams.risk.scanner import ScanInputError


def span(text="Zoë", start=2, end=5, confidence=0.95):
    return dict(text=text, start=start, end=end, confidence=confidence)


class FakeExtractor:
    def __init__(self, result=None, delay=0, error=False):
        self.result = result
        self.delay = delay
        self.error = error
        self.calls = []
        self.active = self.peak = 0

    def extract(self, text, labels, threshold):
        self.calls.append((text, labels, threshold))
        self.active += 1
        self.peak = max(self.peak, self.active)
        try:
            time.sleep(self.delay)
            if self.error:
                raise RuntimeError("private model input")
            return self.result or {"entities": {label: [] for label in labels}}
        finally:
            self.active -= 1


async def test_unicode_repeats_floor_and_shared_filters():
    model = FakeExtractor(
        {"entities": {"person": [span(), span(start=10, end=13), span(confidence=0.7)]}}
    )
    async with GlinerScanner(model) as scanner:
        found = await scanner.scan("😀 Zoë and Zoë", ["PERSON"], 0.5)
        assert [(x.start_pos, x.end_pos) for x in found] == [(5, 9), (14, 18)]
        assert model.calls[0][2] == 0.9
        assert await scanner.scan("😀 Zoë and Zoë", ["PERSON"], 0.99) == []
    model = FakeExtractor({"entities": {"email": [span("a@example.com", 0, 13)]}})
    async with GlinerScanner(model) as scanner:
        assert await scanner.scan("a@example.com", ["EMAIL_ADDRESS"], 0.5) == []


@pytest.mark.parametrize(
    "value",
    [
        span("wrong"),
        span(start=-1),
        span(end=500),
        span(start=True),
        span(confidence=math.nan),
        span(confidence=True),
        span(confidence=1.1),
    ],
)
def test_malformed_spans_fail_even_below_threshold(value):
    with pytest.raises(GlinerResponseError):
        parse_result("😀 Zoë", ["PERSON"], 0.99, {"entities": {"person": [value]}})


@pytest.mark.parametrize(
    "result", [None, {}, {"entities": {}}, {"entities": {"person": None}}]
)
def test_missing_output_is_not_clean(result):
    with pytest.raises(GlinerResponseError):
        parse_result("text", ["PERSON"], 0.9, result)


async def test_input_limits_selection_and_errors():
    model = FakeExtractor(error=True)
    scanner = GlinerScanner(model)
    assert await scanner.scan("", None, 0.5) == []
    for text, entities, threshold in [
        ("text", ["UNKNOWN"], 0.5),
        ("x" * (256 * 1024 + 1), None, 0.5),
        ("text", None, math.nan),
    ]:
        with pytest.raises(ScanInputError):
            await scanner.scan(text, entities, threshold)
    assert not model.calls
    with pytest.raises(GlinerResponseError, match="PII model scan failed") as exc:
        await scanner.scan("private", ["PERSON", "PERSON"], 0.5)
    assert "private" not in str(exc.value)
    assert len(model.calls[0][1]) == 1


async def test_soft_deadline_and_slot_timeout():
    model = FakeExtractor(delay=0.05)
    scanner = GlinerScanner(model, timeout=0.01, slot_timeout=0.005)
    with pytest.raises(TimeoutError):
        await scanner.scan("text", ["PERSON"], 0.5)
    assert model.active == 0
    scanner = GlinerScanner(model, timeout=1, slot_timeout=0.005)
    async with anyio.create_task_group() as tasks:
        tasks.start_soon(scanner.scan, "text", ["PERSON"], 0.5)
        await anyio.sleep(0.01)
        with pytest.raises(TimeoutError):
            await scanner.scan("text", ["PERSON"], 0.5)
    assert model.peak == 1


async def test_cancellation_keeps_model_slot_until_worker_finishes():
    model = FakeExtractor(delay=0.03)
    scanner = GlinerScanner(model)
    with anyio.move_on_after(0.005):
        await scanner.scan("text", ["PERSON"], 0.5)
    assert model.active == 0


async def test_http_contract_and_sanitized_failure():
    model = FakeExtractor({"entities": {"person": [span()]}})
    scanner = GlinerScanner(model)
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=build_app(scanner)), base_url="http://test"
    ) as client:
        r = await client.post(
            "/analyze",
            json={"text": ["😀 Zoë"], "entities": ["PERSON"], "score_threshold": 0.5},
        )
        assert r.status_code == 200
        assert r.json() == [[dict(entity_type="PERSON", start=2, end=5, score=0.95)]]
        for bad in [
            {"text": "wrong"},
            {"text": ["x"], "entities": [1]},
            {"text": ["x"], "language": "fr"},
        ]:
            assert (await client.post("/analyze", json=bad)).status_code == 400
        model.error = True
        r = await client.post(
            "/analyze", json={"text": ["private"], "entities": ["PERSON"]}
        )
        assert r.status_code == 502
        assert "private" not in r.text


async def test_factory_selects_local_model(monkeypatch):
    import structlog

    from pystreams.deps.scanner import build_presidio_scanner
    from pystreams.risk import gliner

    monkeypatch.setenv("GRAM_PII_BACKEND", "gliner")
    monkeypatch.setattr(gliner, "LocalExtractor", FakeExtractor)
    scanner = await build_presidio_scanner(
        scan_workers=2,
        scan_max_tasks_per_child=100,
        scan_timeout=10,
        scan_slot_timeout=1,
        max_scan_concurrency=None,
        logger=structlog.get_logger(),
    )
    assert isinstance(scanner, GlinerScanner)
    assert await scanner.scan("text", ["PERSON"], 0.5) == []
    monkeypatch.setenv("GRAM_PII_BACKEND", "typo")
    with pytest.raises(ValueError, match="GRAM_PII_BACKEND"):
        await build_presidio_scanner(
            scan_workers=2,
            scan_max_tasks_per_child=100,
            scan_timeout=10,
            scan_slot_timeout=1,
            max_scan_concurrency=None,
            logger=structlog.get_logger(),
        )


def test_catalog_types_have_explicit_labels():
    import re
    from pathlib import Path

    from pystreams.risk.gliner import LABELS

    source = (
        Path(__file__).resolve().parents[2]
        / "server/internal/risk/policycatalog/presidio.go"
    ).read_text()
    catalog = source.split("var presidioEntities = []string{", 1)[1].split("}", 1)[0]
    assert set(re.findall(r'"([A-Z_]+)"', catalog)) <= ENTITIES.keys()
    assert set(LABELS) == set(ENTITIES)
    assert len(set(LABELS.values())) == len(LABELS)
