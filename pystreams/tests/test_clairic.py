import json

import httpx
import pytest

from pystreams.cmd.clairic_server import build_app
from pystreams.risk.clairic import (
    ClairicResponseError,
    ClairicScanner,
    parse_result,
)
from pystreams.risk.scanner import ScanInputError


def answer(values=None, spans=None, confidence=0.9):
    return {
        "type": "list",
        "value": values or [],
        "spans": spans or [],
        "confidence": confidence,
    }


def result(answers):
    return {"answers": answers, "truncated": False, "usage": {"input_tokens": 100}}


def scanner_for(payload, status=200):
    def respond(request):
        assert request.url.path.endswith("/ai/run/@cf/cloudflare/clairic")
        assert request.headers["Authorization"] == "Bearer test-token"
        body = json.loads(request.content)
        assert body["model"] == "clairic"
        assert all(field["type"] == "list" for field in body["fields"].values())
        return httpx.Response(status, json=payload)

    return ClairicScanner(
        "test-account",
        "test-token",
        client=httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


async def test_unicode_repeated_spans_and_threshold():
    text = "😀 Zoë and Zoë"
    payload = result({"PERSON": answer(["Zoë", "Zoë"], [[2, 5], [10, 13]])})
    async with scanner_for({"success": True, "result": payload}) as scanner:
        detections = await scanner.scan(text, ["PERSON"], 0.8)
        assert [(d.start_pos, d.end_pos) for d in detections] == [(5, 9), (14, 18)]
        assert [
            text.encode()[d.start_pos : d.end_pos].decode() for d in detections
        ] == ["Zoë", "Zoë"]
        assert await scanner.scan(text, ["PERSON"], 0.95) == []


async def test_existing_false_positive_and_retirement_filters():
    text = "a@example.com D1234567"
    payload = result(
        {
            "EMAIL_ADDRESS": answer(["a@example.com"], [[0, 13]]),
            "US_DRIVER_LICENSE": answer(["D1234567"], [[14, 22]]),
        }
    )
    async with scanner_for({"success": True, "result": payload}) as scanner:
        assert await scanner.scan(text, list(payload["answers"]), 0.5) == []


@pytest.mark.parametrize(
    "change",
    [
        {"truncated": True},
        {"truncated": None},
        {"answers": {}},
        {"answers": {"PERSON": answer(["wrong"], [[0, 3]])}},
        {"answers": {"PERSON": answer(["Zoë"], [[-1, 3]])}},
        {"answers": {"PERSON": answer(["Zoë"], [[False, 3]])}},
        {"answers": {"PERSON": answer(["Zoë"], [[0, 3]], float("nan"))}},
        {"answers": {"PERSON": answer(["Zoë"], [[0, 3]], True)}},
        {"answers": {"PERSON": {"type": "list", "value": None}}},
    ],
)
def test_incomplete_or_malformed_output_is_not_clean(change):
    payload = result({"PERSON": answer(["Zoë"], [[0, 3]])}) | change
    with pytest.raises(ClairicResponseError):
        parse_result("Zoë", ["PERSON"], 0.5, payload)


async def test_empty_lists_and_entity_selection():
    def respond(request):
        fields = json.loads(request.content)["fields"]
        assert list(fields) == ["US_SSN"]
        return httpx.Response(
            200,
            json={
                "success": True,
                "result": result({"US_SSN": answer()}),
            },
        )

    async with ClairicScanner(
        "test", "test", client=httpx.AsyncClient(transport=httpx.MockTransport(respond))
    ) as scanner:
        assert await scanner.scan("no identifiers", ["US_SSN", "US_SSN"], 0.5) == []
        assert await scanner.scan("", None, 0.5) == []
        with pytest.raises(ScanInputError):
            await scanner.scan("text", ["UNKNOWN"], 0.5)
        with pytest.raises(ScanInputError):
            await scanner.scan("x" * (256 * 1024 + 1), None, 0.5)


@pytest.mark.parametrize(
    ("status", "exception"),
    [(401, ScanInputError), (429, ClairicResponseError), (500, ClairicResponseError)],
)
async def test_remote_errors_are_sanitized(status, exception):
    async with scanner_for({"error": "sensitive upstream content"}, status) as scanner:
        with pytest.raises(exception) as raised:
            await scanner.scan("private input", ["PERSON"], 0.5)
        assert "sensitive" not in str(raised.value)
        assert "private" not in str(raised.value)


async def test_http_contract_matches_go_batch_client():
    payload = result({"PERSON": answer(["Zoë"], [[2, 5]])})
    async with scanner_for({"success": True, "result": payload}) as scanner:
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=build_app(scanner)),
            base_url="http://test",
        ) as client:
            response = await client.post(
                "/analyze",
                json={
                    "text": ["😀 Zoë"],
                    "language": "en",
                    "entities": ["PERSON"],
                    "score_threshold": 0.5,
                },
            )
            assert response.status_code == 200
            assert response.json() == [
                [
                    {
                        "entity_type": "PERSON",
                        "start": 2,
                        "end": 5,
                        "score": 0.9,
                    }
                ]
            ]
            for bad in ({"text": "wrong"}, {"text": ["x"], "entities": [1]}):
                assert (await client.post("/analyze", json=bad)).status_code == 400


async def test_http_failure_never_returns_empty_success():
    async with scanner_for({"success": False}) as scanner:
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=build_app(scanner)),
            base_url="http://test",
        ) as client:
            response = await client.post("/analyze", json={"text": ["private"]})
            assert response.status_code == 502
            assert "private" not in response.text


async def test_deadline_and_cancellation():
    import anyio

    async def slow_response(request):
        await anyio.sleep(1)
        return httpx.Response(200, json={})

    async with ClairicScanner(
        "test",
        "test",
        timeout=0.01,
        client=httpx.AsyncClient(transport=httpx.MockTransport(slow_response)),
    ) as scanner:
        with pytest.raises(TimeoutError):
            await scanner.scan("text", ["PERSON"], 0.5)
        with anyio.move_on_after(0.001) as scope:
            await scanner.scan("text", ["PERSON"], 0.5)
        assert scope.cancelled_caught


async def test_factory_selects_clairic_without_loading_presidio(monkeypatch):
    import structlog

    from pystreams.deps.scanner import build_presidio_scanner

    monkeypatch.setenv("GRAM_PII_BACKEND", "clairic")
    monkeypatch.setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
    monkeypatch.setenv("CLOUDFLARE_AUTH_TOKEN", "test-token")

    async def build():
        return await build_presidio_scanner(
            scan_workers=2,
            scan_max_tasks_per_child=100,
            scan_timeout=10,
            scan_slot_timeout=1,
            max_scan_concurrency=None,
            logger=structlog.get_logger(),
        )

    async with await build() as scanner:
        assert isinstance(scanner, ClairicScanner)
    monkeypatch.setenv("GRAM_PII_BACKEND", "typo")
    with pytest.raises(ValueError, match="GRAM_PII_BACKEND"):
        await build()
    monkeypatch.setenv("GRAM_PII_BACKEND", "clairic")
    monkeypatch.delenv("CLOUDFLARE_AUTH_TOKEN")
    with pytest.raises(ValueError, match="requires"):
        await build()


def test_all_catalog_entities_are_supported():
    import re
    from pathlib import Path

    from pystreams.risk.clairic import ENTITIES

    # A new policy entity must not become a silent gap in the alternative engine.
    source = (
        Path(__file__).resolve().parents[2]
        / "server/internal/risk/policycatalog/presidio.go"
    ).read_text()
    catalog = source.split("var presidioEntities = []string{", 1)[1].split("}", 1)[0]
    assert set(re.findall(r'"([A-Z_]+)"', catalog)) <= ENTITIES.keys()
